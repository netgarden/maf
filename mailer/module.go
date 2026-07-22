// Package mailer queues emails in the database and delivers them
// asynchronously, retrying transient failures with a bounded (default 1
// week) retry window — coordinated across multiple application replicas
// via github.com/netgarden/maf/jobs for scheduling and a SELECT ... FOR
// UPDATE SKIP LOCKED claim (see service.go) so no email is ever sent
// twice. See README.md for configuration, usage, and testing.
package mailer

import (
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/jobs"
	"github.com/netgarden/maf/mailer/rpc"
	"github.com/netgarden/maf/security"
	"github.com/netgarden/rrpc"
)

func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager *maf.Manager
	config  *maf.Config
	db      *gorm.DB

	service   *Service
	rpcModule *rpc.Module
}

func (m *Module) GetID() string   { return "mailer" }
func (m *Module) GetName() string { return "Mailer" }

func (m *Module) SetManager(manager *maf.Manager) { m.manager = manager }

// GetDependencies declares "jobs" and "security". mailer never talks to
// github.com/netgarden/maf/locks directly (jobs already brings it in
// transitively for its own per-tick lease), and "database" isn't declared
// either, matching the locks/jobs precedent: SetDB/GetDBEntities don't need
// a formal dependency since the database module delivers SetDB during its
// own PreInitialize-phase consumer fan-out, before any module's
// Initialize() runs. "security" is required for its encryption manager —
// see service.go's encryptBody/decryptBody.
func (m *Module) GetDependencies() []string { return []string{"jobs", "security"} }

func (m *Module) GetConfigSchema() []maf.ConfigItem {
	return []maf.ConfigItem{
		// smtp.host is deliberately not Required — see Initialize: when
		// unset, emails are still queued but never delivered (see
		// README.md's "Running without SMTP configured").
		{Name: "mailer.smtp.host", Type: maf.String},
		{Name: "mailer.smtp.port", Type: maf.Int, DefaultValue: 587},
		{Name: "mailer.smtp.username", Type: maf.String},
		{Name: "mailer.smtp.password", Type: maf.String},
		{Name: "mailer.smtp.encryption", Type: maf.String, DefaultValue: "starttls"}, // none|starttls|tls
		// smtp.from is not Required either — like smtp.host, there's no safe
		// static default for a real outgoing address (a placeholder would
		// just get real mail rejected/flagged as spam), so it's validated
		// in Initialize() instead: required only once smtp.host is actually
		// set, i.e. once delivery is actually going to be attempted.
		{Name: "mailer.smtp.from", Type: maf.String},
		{Name: "mailer.smtp.timeout", Type: maf.Duration, DefaultValue: 30 * time.Second},

		{Name: "mailer.retry.initialInterval", Type: maf.Duration, DefaultValue: time.Minute},
		{Name: "mailer.retry.multiplier", Type: maf.Float64, DefaultValue: 3.0},
		{Name: "mailer.retry.maxInterval", Type: maf.Duration, DefaultValue: 6 * time.Hour},
		{Name: "mailer.retry.maxAge", Type: maf.Duration, DefaultValue: 7 * 24 * time.Hour}, // the "1 week" default

		{Name: "mailer.queue.batchSize", Type: maf.Int, DefaultValue: 20},
		{Name: "mailer.queue.claimTimeout", Type: maf.Duration, DefaultValue: 2 * time.Minute},
		{Name: "mailer.queue.tickInterval", Type: maf.Duration, DefaultValue: 15 * time.Second},

		// How long Send's direct-delivery attempt (see service.go's
		// tryDeliverDirect) is allowed to run before being abandoned —
		// shorter than smtp.timeout since this is a "fast path, don't hold
		// it open too long" attempt, not the definitive send attempt (the
		// queue tick remains that; an abandoned attempt just leaves the row
		// queued for the next tick).
		{Name: "mailer.queue.directSendTimeout", Type: maf.Duration, DefaultValue: 10 * time.Second},

		// How long terminal-status rows are kept before the retention job
		// (see handler.go's NewRetentionHandler) deletes them. sent/cancelled
		// are routine noise and get a short window; failed (gave-up) rows are
		// usually worth investigating, so they get a much longer one.
		{Name: "mailer.retention.maxAge", Type: maf.Duration, DefaultValue: 30 * 24 * time.Hour},
		{Name: "mailer.retention.failedMaxAge", Type: maf.Duration, DefaultValue: 180 * 24 * time.Hour},
		{Name: "mailer.retention.tickInterval", Type: maf.Duration, DefaultValue: 24 * time.Hour},
		{Name: "mailer.retention.jobTimeout", Type: maf.Duration, DefaultValue: 10 * time.Minute},
	}
}

func (m *Module) SetConfig(config *maf.Config) { m.config = config }
func (m *Module) SetDB(db *gorm.DB)            { m.db = db }

func (m *Module) GetDBEntities() []interface{} {
	return []interface{}{&Email{}, &Template{}}
}

// validateSMTPFrom enforces that smtp.from is set whenever smtp.host is —
// neither has a safe static default (see GetConfigSchema), so this is done
// here in code rather than via ConfigItem.Required: unset host means
// "queue only, no delivery ever attempted" and from is irrelevant, but a
// set host with no from would mean actually attempting delivery with no
// usable From address.
func validateSMTPFrom(host, from string) error {
	if host != "" && from == "" {
		return errors.New("mailer: smtp.from is required when smtp.host is set")
	}
	return nil
}

func (m *Module) Initialize() error {
	jobsModule, ok := m.manager.GetModule("jobs").(*jobs.Module)
	if !ok {
		return errors.New("mailer: jobs module not found or wrong type — register jobs before mailer")
	}

	securityModule, ok := m.manager.GetModule("security").(*security.Module)
	if !ok {
		return errors.New("mailer: security module not found or wrong type — register security before mailer")
	}

	cfg := m.config.Sub("mailer")

	host := cfg.GetString("smtp.host")
	from := cfg.GetString("smtp.from")
	if err := validateSMTPFrom(host, from); err != nil {
		return err
	}

	// A nil sender means "queue only" — see Service.ProcessBatch/
	// tryDeliverDirect, which no-op rather than ever calling a nil Sender.
	var sender Sender
	if host != "" {
		var err error
		sender, err = NewSMTPSender(SMTPConfig{
			Host:       host,
			Port:       cfg.GetInt("smtp.port"),
			Username:   cfg.GetString("smtp.username"),
			Password:   cfg.GetString("smtp.password"),
			Encryption: cfg.GetString("smtp.encryption"),
			From:       from,
			Timeout:    cfg.GetDuration("smtp.timeout"),
		})
		if err != nil {
			return err
		}
	} else {
		slog.Info("mailer: smtp.host not configured — emails will be queued but not delivered")
	}

	retryCfg := RetryConfig{
		InitialInterval: cfg.GetDuration("retry.initialInterval"),
		Multiplier:      cfg.GetFloat64("retry.multiplier"),
		MaxInterval:     cfg.GetDuration("retry.maxInterval"),
		MaxAge:          cfg.GetDuration("retry.maxAge"),
	}

	claimTimeout := cfg.GetDuration("queue.claimTimeout")
	directSendTimeout := cfg.GetDuration("queue.directSendTimeout")
	m.service = NewService(m.db, sender, retryCfg, cfg.GetInt("queue.batchSize"), claimTimeout, securityModule.GetEncryptionManager(), directSendTimeout)

	jobsModule.GetService().RegisterHandler("mailer-tick", NewTickHandler(m.service))

	// jobs' Timeout is deliberately the same value as queue.claimTimeout —
	// "how long before a crashed replica's claim is considered abandoned"
	// is one config knob, not two.
	tickSeconds := int64(cfg.GetDuration("queue.tickInterval").Seconds())
	claimSeconds := int64(claimTimeout.Seconds())
	if _, err := jobsModule.GetService().AddJob("mailer-tick", "", "Send queued emails", tickSeconds, claimSeconds); err != nil {
		return err
	}

	retentionCfg := RetentionConfig{
		MaxAge:       cfg.GetDuration("retention.maxAge"),
		FailedMaxAge: cfg.GetDuration("retention.failedMaxAge"),
	}
	jobsModule.GetService().RegisterHandler("mailer-retention", NewRetentionHandler(m.service, retentionCfg))

	retentionTickSeconds := int64(cfg.GetDuration("retention.tickInterval").Seconds())
	retentionJobTimeoutSeconds := int64(cfg.GetDuration("retention.jobTimeout").Seconds())
	if _, err := jobsModule.GetService().AddJob("mailer-retention", "", "Purge old sent/cancelled/failed emails", retentionTickSeconds, retentionJobTimeoutSeconds); err != nil {
		return err
	}

	m.rpcModule = rpc.NewModule()
	m.rpcModule.SetMailerService(newMailerRPCService(m.service))

	return nil
}

// GetRRPCModules is duck-typed picked up by rrpc-server's PreStart (see
// maf/rrpc-server/interfaces.go) — no explicit "rrpc-server" dependency or
// registration call needed, same as citadel's own module. The service is
// registered under "api/admin/mailer", relying on the host application
// already gating "/api/admin/*" behind an admin-only middleware (as
// citadel does, and as maf/rrpc-auth's own Users service already relies on
// for "api/admin/users") — mailer implements no auth of its own.
func (m *Module) GetRRPCModules() []rrpc.ServerModule {
	return []rrpc.ServerModule{m.rpcModule}
}

// GetService returns the mailer service, available after Initialize — this
// is the module's public API: Send/EnqueueTx for plain emails,
// RegisterTemplate/SendTemplate for templated ones.
func (m *Module) GetService() *Service {
	return m.service
}
