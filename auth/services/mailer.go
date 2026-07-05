package services

// TemplateMailer is the minimal mailer capability this package needs to
// send templated notification emails — narrowed from
// mailer.Service.EnqueueTemplate so this package doesn't need to depend on
// github.com/netgarden/maf/mailer directly. rrpc-auth's Module wires a
// real implementation in via each service's own SetMailer
// (UsersService.SetMailer, AuthService.SetMailer) only when a "mailer"
// module is also registered by the consuming application; left unset, the
// corresponding feature (dto.UserCreateDTO.SendCredentialsEmail,
// AuthService.RequestPasswordReset) is simply a no-op.
type TemplateMailer interface {
	EnqueueTemplate(templateID string, to, cc, bcc []string, data any) error
}

// NewUserCredentialsTemplateID is the mailer template ID rrpc-auth's
// Module registers a default for whenever a "mailer" module is present —
// override its wording via mailer's own admin API (see
// NewUserCredentialsData for the variables available to it).
const NewUserCredentialsTemplateID = "auth.new-user-credentials"

// NewUserCredentialsData is the template data rendered into
// NewUserCredentialsTemplateID.
type NewUserCredentialsData struct {
	Username          string
	TemporaryPassword string
	LoginURL          string
}

// PasswordResetTemplateID is the mailer template ID rrpc-auth's Module
// registers a default for whenever a "mailer" module is present — override
// its wording via mailer's own admin API (see PasswordResetData for the
// variables available to it).
const PasswordResetTemplateID = "auth.password-reset"

// PasswordResetData is the template data rendered into
// PasswordResetTemplateID.
type PasswordResetData struct {
	Username string
	ResetURL string
}
