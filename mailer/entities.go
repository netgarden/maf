package mailer

import (
	"time"

	"github.com/netgarden/maf/database"
)

// EmailStatus is the lifecycle state of a queued Email. See service.go's
// claim/finalize logic for the transitions between these.
type EmailStatus string

const (
	EmailStatusQueued    EmailStatus = "queued"
	EmailStatusSending   EmailStatus = "sending"
	EmailStatusSent      EmailStatus = "sent"
	EmailStatusFailed    EmailStatus = "failed"
	EmailStatusCancelled EmailStatus = "cancelled"
)

// Email is a single queued message. To/Cc/Bcc are stored as JSON rather
// than a native Postgres array (e.g. pq.StringArray) since nothing else in
// this codebase uses lib/pq — the Postgres driver here is pgx-based — and
// GORM's serializer:json needs no new dependency.
type Email struct {
	database.EntityBase

	To  []string `gorm:"serializer:json;type:text;not null"`
	Cc  []string `gorm:"serializer:json;type:text"`
	Bcc []string `gorm:"serializer:json;type:text"`

	Subject  string
	BodyText string  `gorm:"type:text"`
	BodyHTML *string `gorm:"type:text"` // nil = text-only email

	Status        EmailStatus `gorm:"type:varchar(16);index:email_claimable;not null;default:queued"`
	Attempts      int
	NextAttemptAt time.Time `gorm:"index:email_claimable"`
	LastAttemptAt *time.Time
	LastError     *string `gorm:"type:text"`

	// Claim fields make the queue safe under multiple replicas — see
	// service.go's claimBatch/RetryEmail/CancelEmail. A row is claimable
	// again once ClaimExpiresAt is in the past, whether that's because
	// nothing ever claimed it or because whatever claimed it crashed
	// before finishing.
	ClaimToken     *string
	ClaimedAt      *time.Time
	ClaimExpiresAt *time.Time `gorm:"index:email_claimable"`

	SentAt      *time.Time
	CancelledAt *time.Time
	GaveUpAt    *time.Time
}

func (Email) TableName() string { return "mailer_queue" }

// EffectiveStatus reports what an observer should be told: a row stuck at
// EmailStatusSending past its ClaimExpiresAt belongs to a replica that
// crashed mid-send, not one actually in flight — reporting the raw
// "sending" value would be misleading (it will simply be retried
// automatically by the next tick), so this surfaces it as "queued"
// instead. Everywhere except the admin API should have no reason to care
// about the distinction.
func (e Email) EffectiveStatus() EmailStatus {
	if e.Status == EmailStatusSending && e.ClaimExpiresAt != nil && e.ClaimExpiresAt.Before(time.Now()) {
		return EmailStatusQueued
	}
	return e.Status
}

// Template stores an admin-supplied override for a template registered in
// code by some module (see templates.go). A row exists only if an admin
// has actually customized that template ID — this is what makes "reset to
// default" a plain row delete, and what lets a module's registered default
// evolve across releases without a stale copy sitting in the DB for apps
// that never touched it.
type Template struct {
	database.EntityBase

	TemplateID string `gorm:"uniqueIndex;not null"`
	Subject    string
	BodyText   string  `gorm:"type:text"`
	BodyHTML   *string `gorm:"type:text"`
}

func (Template) TableName() string { return "mailer_templates" }
