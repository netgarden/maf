package rrpc_auth

import (
	"time"

	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/locks"
	"github.com/netgarden/maf/mailer"
	"github.com/netgarden/maf/security/passwords"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/security"
	"gorm.io/gorm"
)

func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager *maf.Manager
	config  *maf.Config
	db      *gorm.DB

	passwordsManager *passwords.Manager

	servicesManager *services.Manager
}

func (m *Module) GetID() string {
	return "auth"
}

func (m *Module) GetName() string {
	return "Auth"
}

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

// GetDependencies always requires "security" and "locks" (EnsureAdminExists
// needs locks.Service to serialize admin bootstrap across replicas); "mailer"
// is added dynamically only when a "mailer" module is also registered by the
// consuming application (checked here since SetManager has already run for
// every module by the time the framework calls this) — so wiring the
// optional notification emails in Initialize doesn't force every auth
// consumer to also register mailer.
func (m *Module) GetDependencies() []string {
	deps := []string{"security", "locks"}
	if m.manager.GetModule("mailer") != nil {
		deps = append(deps, "mailer")
	}
	return deps
}

func (m *Module) GetConfigSchema() []maf.ConfigItem {
	return []maf.ConfigItem{
		{Name: "auth.token.ttl", Type: maf.Duration, DefaultValue: 15 * time.Minute},
		{Name: "auth.session.ttl", Type: maf.Duration, DefaultValue: 7 * 24 * time.Hour},
		{Name: "auth.session.cookie.name", Type: maf.String, DefaultValue: "session"},
		{Name: "auth.session.cookie.path", Type: maf.String, DefaultValue: "/"},
		{Name: "auth.session.cookie.force_secure", Type: maf.Bool, DefaultValue: false},
		{Name: "auth.admin.defaultPassword", Type: maf.String, DefaultValue: "admin"},
		{Name: "auth.passwordReset.tokenTTL", Type: maf.Duration, DefaultValue: time.Hour},
		{Name: "auth.passwordReset.enabled", Type: maf.Bool, DefaultValue: true},
		// baseUrl lives here (rather than only in rrpc-auth, which has no
		// business logic of its own) even though it's really an HTTP-layer
		// concern — see Initialize, which is the one place that reads it.
		{Name: "auth.passwordReset.baseUrl", Type: maf.String, DefaultValue: ""},
	}
}

func (m *Module) SetConfig(config *maf.Config) {
	m.config = config
}

func (m *Module) GetConfig() *maf.Config {
	return m.config
}

func (m *Module) SetDB(db *gorm.DB) {
	m.db = db
}

func (m *Module) GetDBEntities() []interface{} {
	return []interface{}{
		&entities.User{},
		&entities.Session{},
		&entities.PasswordResetToken{},
		// locks.Lock is not listed here — it's locks.Module's own entity
		// (registered via its GetDBEntities()), and "locks" is now a hard
		// GetDependencies() entry above, so it's guaranteed to be present.
	}
}

func (m *Module) Initialize() error {

	var err error

	// Cross-module config: read security.secret without importing the security config type.
	secret := m.config.GetString("security.secret")

	// Security module is still needed for the passwords service (not config).
	securityModule := m.manager.GetModule("security").(*security.Module)
	m.passwordsManager = securityModule.GetPasswordsManager()

	locksModule := m.manager.GetModule("locks").(*locks.Module)
	locksService := locksModule.GetService()

	m.servicesManager = services.NewManager(
		m.config,
		m.db,
		secret,
		m.passwordsManager,
		locksService,
	)
	err = m.servicesManager.Init()
	if err != nil {
		return err
	}

	defaultAdminPassword := m.config.GetString("auth.admin.defaultPassword")
	if err = m.servicesManager.GetUsersService().EnsureAdminExists(defaultAdminPassword); err != nil {
		return err
	}

	// Optional: only wired when the consuming application also registers
	// maf/mailer. Registers sensible defaults for each notification
	// email's content — an admin can customize their wording afterward via
	// mailer's own admin API (UpdateTemplate), same as any other template.
	if mailerMod, ok := m.manager.GetModule("mailer").(*mailer.Module); ok {
		mailerSvc := mailerMod.GetService()

		err = mailerSvc.RegisterTemplate(mailer.TemplateDefault{
			ID:          NewUserCredentialsTemplateID,
			Subject:     "Your account has been created",
			BodyText:    "Hello {{.Username}},\n\nYour temporary password is: {{.TemporaryPassword}}\n\nLog in at {{.LoginURL}}.",
			Description: "Variables: Username, TemporaryPassword, LoginURL",
		})
		if err != nil {
			return err
		}
		m.servicesManager.GetUsersService().SetMailer(mailerTemplateAdapter{mailerSvc}, "")

		err = mailerSvc.RegisterTemplate(mailer.TemplateDefault{
			ID:          PasswordResetTemplateID,
			Subject:     "Reset your password",
			BodyText:    "Hello {{.Username}},\n\nA password reset was requested for your account. If this was you, reset it here: {{.ResetURL}}\n\nIf you didn't request this, you can safely ignore this email.",
			Description: "Variables: Username, ResetURL",
		})
		if err != nil {
			return err
		}
		m.servicesManager.GetAuthService().SetMailer(mailerTemplateAdapter{mailerSvc}, m.config.GetString("auth.passwordReset.baseUrl"))
	}

	return nil
}

// mailerTemplateAdapter narrows *mailer.Service down to the
// TemplateMailer interface UsersService/AuthService depend on, so
// auth/services doesn't need to import maf/mailer directly — only this
// module-wiring file does.
type mailerTemplateAdapter struct {
	svc *mailer.Service
}

func (a mailerTemplateAdapter) EnqueueTemplate(templateID string, to, cc, bcc []string, data any) error {
	_, err := a.svc.EnqueueTemplate(templateID, to, cc, bcc, data)
	return err
}

func (m *Module) PreStart() error {
	return nil
}

func (m *Module) GetServicesManager() *services.Manager {
	return m.servicesManager
}
