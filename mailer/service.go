package mailer

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/netgarden/maf/datatables"
	"github.com/netgarden/maf/security/encryption"
)

var (
	ErrNotFound        = errors.New("mailer: email not found")
	ErrNotRetryable    = errors.New("mailer: email is not in a retryable state")
	ErrNotCancellable  = errors.New("mailer: email is not in a cancellable state")
	ErrTemplateInvalid = errors.New("mailer: template failed to parse")
)

func NewService(db *gorm.DB, sender Sender, retryCfg RetryConfig, batchSize int, claimTimeout time.Duration, encryptionManager *encryption.Manager, directSendTimeout time.Duration) *Service {
	return &Service{
		db:                db,
		sender:            sender,
		retryCfg:          retryCfg,
		batchSize:         batchSize,
		claimTimeout:      claimTimeout,
		templates:         newTemplateRegistry(),
		encryption:        encryptionManager,
		directSendTimeout: directSendTimeout,
	}
}

// Service's public methods are all plaintext in, plaintext out — encrypting
// Email.BodyText/BodyHTML at rest (see encryptBody/decryptBody) is entirely
// an internal storage-layer detail. Subject/To/Cc/Bcc and Template's own
// body fields are deliberately not encrypted: Subject stays searchable via
// ListEmails' subject ILIKE filter, To/Cc/Bcc stay visible for admin
// triage, and templates are closer to code/config than data — the actual
// sensitive content only exists once rendered into a specific queued Email.
type Service struct {
	db                *gorm.DB
	sender            Sender
	retryCfg          RetryConfig
	batchSize         int
	claimTimeout      time.Duration
	templates         *templateRegistry
	encryption        *encryption.Manager
	directSendTimeout time.Duration
}

// encryptBody encrypts email's BodyText (always) and BodyHTML (if set) in
// place, before it's written to the database. encryption.Manager works in
// raw []byte; BodyText/BodyHTML are string columns (TEXT, not BYTEA), so
// the encrypted bytes are base64-encoded before being stored.
func (s *Service) encryptBody(email *Email) error {
	encryptedText, err := s.encryption.Encrypt([]byte(email.BodyText))
	if err != nil {
		return fmt.Errorf("mailer: encrypt body text: %w", err)
	}
	email.BodyText = base64.StdEncoding.EncodeToString(encryptedText)

	if email.BodyHTML != nil {
		encryptedHTML, err := s.encryption.Encrypt([]byte(*email.BodyHTML))
		if err != nil {
			return fmt.Errorf("mailer: encrypt body html: %w", err)
		}
		encoded := base64.StdEncoding.EncodeToString(encryptedHTML)
		email.BodyHTML = &encoded
	}

	return nil
}

// decryptBody reverses encryptBody, for every email loaded back out of the
// database — GetEmail, ListEmails, and claimBatch all call this so nothing
// downstream (including the actual SMTP send in sendAndFinalize) ever sees
// ciphertext.
func (s *Service) decryptBody(email *Email) error {
	rawText, err := base64.StdEncoding.DecodeString(email.BodyText)
	if err != nil {
		return fmt.Errorf("mailer: decode body text: %w", err)
	}
	decryptedText, err := s.encryption.Decrypt(rawText)
	if err != nil {
		return fmt.Errorf("mailer: decrypt body text: %w", err)
	}
	email.BodyText = string(decryptedText)

	if email.BodyHTML != nil {
		rawHTML, err := base64.StdEncoding.DecodeString(*email.BodyHTML)
		if err != nil {
			return fmt.Errorf("mailer: decode body html: %w", err)
		}
		decryptedHTML, err := s.encryption.Decrypt(rawHTML)
		if err != nil {
			return fmt.Errorf("mailer: decrypt body html: %w", err)
		}
		decoded := string(decryptedHTML)
		email.BodyHTML = &decoded
	}

	return nil
}

// SendRequest is the content of a not-yet-rendered email to queue.
type SendRequest struct {
	To       []string
	Cc       []string
	Bcc      []string
	Subject  string
	BodyText string
	BodyHTML *string
}

// Send writes req's row via EnqueueTx (autocommitted, since s.db is not an
// open caller transaction) and then attempts to deliver it right away in
// the background — see tryDeliverDirect. Use EnqueueTx directly if you need
// the enqueue to participate in your own transaction; that variant never
// attempts direct delivery, since its row isn't guaranteed to be committed
// when it returns.
func (s *Service) Send(req *SendRequest) (*Email, error) {
	email, err := s.EnqueueTx(s.db, req)
	if err != nil {
		return nil, err
	}
	s.tryDeliverDirect(email)
	return email, nil
}

// EnqueueTx enqueues within an existing transaction, so a caller can enqueue
// an email inside the same transaction as the business action that
// triggers it — a rollback of that transaction also rolls back the
// enqueue.
func (s *Service) EnqueueTx(tx *gorm.DB, req *SendRequest) (*Email, error) {
	email := &Email{
		To:            req.To,
		Cc:            req.Cc,
		Bcc:           req.Bcc,
		Subject:       req.Subject,
		BodyText:      req.BodyText,
		BodyHTML:      req.BodyHTML,
		Status:        EmailStatusQueued,
		NextAttemptAt: time.Now(),
	}

	// Encrypt a copy for storage — email itself, returned to the caller,
	// must stay plaintext (see Service's doc comment).
	toStore := *email
	if err := s.encryptBody(&toStore); err != nil {
		return nil, err
	}

	if err := tx.Create(&toStore).Error; err != nil {
		return nil, err
	}

	// Create populates ID/CreatedAt/UpdatedAt on toStore; copy those back
	// onto the plaintext value being returned.
	email.EntityBase = toStore.EntityBase

	return email, nil
}

// SendTemplate resolves templateID (an admin override if one exists, else
// the module-registered default), renders it with data, and enqueues the
// result via Send — from this point on there's no difference between a
// templated and a directly-enqueued email, including the direct-delivery
// attempt Send makes.
func (s *Service) SendTemplate(templateID string, to, cc, bcc []string, data any) (*Email, error) {
	tmpl, err := s.resolveTemplate(s.db, templateID)
	if err != nil {
		return nil, err
	}

	subject, bodyText, bodyHTML, err := renderTemplate(tmpl, data)
	if err != nil {
		return nil, err
	}

	return s.Send(&SendRequest{
		To: to, Cc: cc, Bcc: bcc,
		Subject: subject, BodyText: bodyText, BodyHTML: bodyHTML,
	})
}

// DeliveryEnabled reports whether this Service was constructed with a real
// Sender (see Module.Initialize) — false when mailer.smtp.host is unset, in
// which case ProcessBatch/tryDeliverDirect never attempt delivery and rows
// simply stay queued.
func (s *Service) DeliveryEnabled() bool {
	return s.sender != nil
}

// ProcessBatch claims and attempts to send up to batchSize due emails. It's
// called from a jobs.Handler (see handler.go) on a recurring tick,
// coordinated across replicas by jobs' own per-tick lease — but the
// per-row claimBatch below is what actually prevents any single email from
// being sent twice, since an admin-triggered RetryEmail/CancelEmail call
// can land on a different replica than whichever one is mid-tick. A nil
// sender (see DeliveryEnabled) short-circuits before any row is even
// claimed, so unconfigured deployments never flip queued rows to sending.
func (s *Service) ProcessBatch(ctx context.Context) error {
	if s.sender == nil {
		return nil
	}

	claimed, err := s.claimBatch()
	if err != nil {
		return fmt.Errorf("mailer: claim batch: %w", err)
	}

	for _, email := range claimed {
		if ctx.Err() != nil {
			// Any remaining claimed-but-unattempted rows are simply left in
			// EmailStatusSending — they're recovered automatically once
			// ClaimExpiresAt passes, no special cleanup needed here.
			break
		}
		s.sendAndFinalize(ctx, email)
	}

	return nil
}

// claimBatch atomically claims up to s.batchSize due (or abandoned) rows
// using SELECT ... FOR UPDATE SKIP LOCKED, then returns the full claimed
// rows via the UPDATE's own RETURNING clause — GORM's usual field/
// serializer handling (needed for the JSON-serialized To/Cc/Bcc columns)
// still applies when populating claimed, since it's scanned as a slice of
// Email rather than raw column values.
func (s *Service) claimBatch() ([]Email, error) {

	claimToken := uuid.NewV4().String()
	now := time.Now()
	claimExpiresAt := now.Add(s.claimTimeout)

	var claimed []Email

	err := s.db.Transaction(func(tx *gorm.DB) error {

		claimable := tx.Model(&Email{}).
			Select("id").
			Where("(status = ? AND next_attempt_at <= ?) OR (status = ? AND claim_expires_at < ?)",
				EmailStatusQueued, now, EmailStatusSending, now).
			Order("next_attempt_at").
			Limit(s.batchSize).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})

		return tx.Clauses(clause.Returning{}).
			Model(&claimed).
			Where("id IN (?)", claimable).
			Updates(map[string]interface{}{
				"status":           EmailStatusSending,
				"claim_token":      claimToken,
				"claimed_at":       now,
				"claim_expires_at": claimExpiresAt,
				"attempts":         gorm.Expr("attempts + 1"),
				"last_attempt_at":  now,
			}).Error
	})
	if err != nil {
		return nil, err
	}

	// Decrypt before returning: sendAndFinalize passes BodyText/BodyHTML
	// straight to the SMTP sender, so this is the one decryptBody call site
	// that matters functionally — get it wrong and mailer emails ciphertext.
	for i := range claimed {
		if err := s.decryptBody(&claimed[i]); err != nil {
			return nil, err
		}
	}

	return claimed, nil
}

// claimByID atomically claims a single queued, due email for immediate
// sending — the same "conditional UPDATE, check RowsAffected" idiom
// RetryEmail/CancelEmail use for their own single-row state transitions,
// just transitioning to "sending" instead of back to "queued"/"cancelled".
// Unlike claimBatch, no SELECT ... FOR UPDATE SKIP LOCKED subquery is
// needed here — a single conditional UPDATE by primary key is already
// atomic. Returns (nil, nil) if the row is no longer claimable (most
// commonly: the periodic tick's claimBatch already claimed it first) — this
// is what makes a direct-send attempt racing the tick harmless rather than
// a double send.
func (s *Service) claimByID(id string) (*Email, error) {
	now := time.Now()
	result := s.db.Model(&Email{}).
		Where("id = ? AND status = ? AND next_attempt_at <= ?", id, EmailStatusQueued, now).
		Updates(map[string]interface{}{
			"status":           EmailStatusSending,
			"claim_token":      uuid.NewV4().String(),
			"claimed_at":       now,
			"claim_expires_at": now.Add(s.claimTimeout),
			"attempts":         gorm.Expr("attempts + 1"),
			"last_attempt_at":  now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return s.GetEmail(id)
}

// tryDeliverDirect attempts to send email right away instead of waiting up
// to tickInterval for the next scheduled ProcessBatch — bounded by
// directSendTimeout so a slow/unreachable SMTP server can't hang around
// forever; if it doesn't finish in time, the row is simply left claimable
// and the next tick sends it normally. Runs in its own goroutine so Send
// never blocks its caller on an SMTP round-trip. A nil sender (see
// DeliveryEnabled) is a no-op — the row is left queued for whenever SMTP
// is eventually configured.
func (s *Service) tryDeliverDirect(email *Email) {
	if s.sender == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.directSendTimeout)
		defer cancel()

		claimed, err := s.claimByID(email.ID.String())
		if err != nil {
			slog.Error("mailer: direct-send claim failed", slog.Any("error", err), slog.String("id", email.ID.String()))
			return
		}
		if claimed == nil {
			return // already claimed elsewhere (e.g. the periodic tick) — not our job anymore
		}
		s.sendAndFinalize(ctx, *claimed)
	}()
}

func (s *Service) sendAndFinalize(ctx context.Context, email Email) {

	err := s.sender.Send(ctx, &Message{
		To:       email.To,
		Cc:       email.Cc,
		Bcc:      email.Bcc,
		Subject:  email.Subject,
		BodyText: email.BodyText,
		BodyHTML: email.BodyHTML,
	})

	if err != nil {
		s.finalizeFailure(email, err)
		return
	}

	s.finalizeSuccess(email)
}

func (s *Service) finalizeSuccess(email Email) {
	now := time.Now()
	err := s.db.Model(&Email{}).Where("id = ?", email.ID).Updates(map[string]interface{}{
		"status":           EmailStatusSent,
		"sent_at":          &now,
		"claim_token":      nil,
		"claimed_at":       nil,
		"claim_expires_at": nil,
		"last_error":       nil,
	}).Error
	if err != nil {
		slog.Error("mailer: failed to finalize successful send", slog.Any("error", err), slog.String("id", email.ID.String()))
	}
}

func (s *Service) finalizeFailure(email Email, sendErr error) {

	errMsg := sendErr.Error()
	updates := map[string]interface{}{
		"claim_token":      nil,
		"claimed_at":       nil,
		"claim_expires_at": nil,
		"last_error":       &errMsg,
	}

	if HasExceededMaxAge(email.CreatedAt, s.retryCfg) {
		now := time.Now()
		updates["status"] = EmailStatusFailed
		updates["gave_up_at"] = &now
	} else {
		updates["status"] = EmailStatusQueued
		updates["next_attempt_at"] = time.Now().Add(WithJitter(NextAttemptDelay(email.Attempts, s.retryCfg)))
	}

	if err := s.db.Model(&Email{}).Where("id = ?", email.ID).Updates(updates).Error; err != nil {
		slog.Error("mailer: failed to finalize failed send", slog.Any("error", err), slog.String("id", email.ID.String()))
	}
}

// RetentionConfig controls how long terminal-status emails are kept before
// PurgeOldEmails deletes them. Sent/cancelled rows are routine noise and
// can be pruned aggressively; failed (gave-up) rows are usually worth
// investigating, so they default to a much longer window.
type RetentionConfig struct {
	MaxAge       time.Duration // sent, cancelled
	FailedMaxAge time.Duration // failed (gave up)
}

// PurgeOldEmails deletes terminal-status rows (sent/cancelled/failed) past
// their retention window, using each status's own dedicated timestamp
// (SentAt/CancelledAt/GaveUpAt) rather than UpdatedAt. queued/sending rows
// are never touched no matter their age — only claimBatch/claimByID ever
// transition those. Returns the total number of rows deleted.
//
// A retention delete can in principle race an admin's RetryEmail/CancelEmail
// call on the same row (e.g. retrying a failed email right as it ages past
// FailedMaxAge): whichever commits first wins, same as every other
// conditional state transition in this file — the loser either finds
// nothing to retry (ErrNotFound) or the row survives as freshly "queued"
// and the delete's WHERE simply no longer matches it. No special handling
// needed.
func (s *Service) PurgeOldEmails(cfg RetentionConfig) (int64, error) {
	var total int64

	result := s.db.Where("status = ? AND sent_at < ?", EmailStatusSent, time.Now().Add(-cfg.MaxAge)).Delete(&Email{})
	if result.Error != nil {
		return total, fmt.Errorf("mailer: purge sent emails: %w", result.Error)
	}
	total += result.RowsAffected

	result = s.db.Where("status = ? AND cancelled_at < ?", EmailStatusCancelled, time.Now().Add(-cfg.MaxAge)).Delete(&Email{})
	if result.Error != nil {
		return total, fmt.Errorf("mailer: purge cancelled emails: %w", result.Error)
	}
	total += result.RowsAffected

	result = s.db.Where("status = ? AND gave_up_at < ?", EmailStatusFailed, time.Now().Add(-cfg.FailedMaxAge)).Delete(&Email{})
	if result.Error != nil {
		return total, fmt.Errorf("mailer: purge failed emails: %w", result.Error)
	}
	total += result.RowsAffected

	return total, nil
}

// emailColumns declares Email's queryable fields for ListEmails: which
// are sortable, and — if filterable — the single operator applied
// whenever a request's Filters has a value for it (see datatables.Column).
var emailColumns = []datatables.Column{
	{Name: "subject", DBColumn: "subject", Sortable: true, FilterOperator: datatables.Contains},
	{Name: "status", DBColumn: "status", Sortable: true, FilterOperator: datatables.Eq},
	{Name: "createdAt", DBColumn: "created_at", Sortable: true},
}

// ListEmails returns a filtered, sorted, paginated page of emails per q.
// When q.SortBy is unset, emails are ordered newest-first by default —
// datatables.Apply itself only adds an ORDER BY when a sort is actually
// requested, so the default (matching this endpoint's original hard-coded
// behavior) lives here on the base query instead.
func (s *Service) ListEmails(q datatables.Query) (*datatables.Result[Email], error) {
	db := s.db.Model(&Email{})
	if q.SortBy == "" {
		db = db.Order("created_at DESC")
	}

	result, err := datatables.Apply[Email](db, emailColumns, q)
	if err != nil {
		return nil, err
	}

	for i := range result.Items {
		if err := s.decryptBody(&result.Items[i]); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// GetEmail returns (nil, nil) when id doesn't exist, matching this
// codebase's not-found convention (see e.g. maf/auth's UsersService).
func (s *Service) GetEmail(id string) (*Email, error) {
	var email Email
	err := s.db.Where("id = ?", id).First(&email).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if err := s.decryptBody(&email); err != nil {
		return nil, err
	}
	return &email, nil
}

// RetryEmail re-arms email id for immediate retry (the next tick's
// claimBatch picks it up via NextAttemptAt = now) — it does not itself
// send. The single conditional UPDATE below is what makes this safe to
// call concurrently with a tick claiming the same row: whichever commits
// first wins, the other affects 0 rows.
func (s *Service) RetryEmail(id string) (*Email, error) {

	now := time.Now()
	result := s.db.Model(&Email{}).
		Where("id = ? AND (status = ? OR status = ? OR (status = ? AND claim_expires_at < ?))",
			id, EmailStatusQueued, EmailStatusFailed, EmailStatusSending, now).
		Updates(map[string]interface{}{
			"status":           EmailStatusQueued,
			"next_attempt_at":  now,
			"claim_token":      nil,
			"claimed_at":       nil,
			"claim_expires_at": nil,
			"gave_up_at":       nil,
		})
	if result.Error != nil {
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		return nil, s.notRetryableOrCancellableError(id, ErrNotRetryable)
	}

	return s.GetEmail(id)
}

// CancelEmail is the mirror image of RetryEmail.
func (s *Service) CancelEmail(id string) (*Email, error) {

	now := time.Now()
	result := s.db.Model(&Email{}).
		Where("id = ? AND (status = ? OR status = ? OR (status = ? AND claim_expires_at < ?))",
			id, EmailStatusQueued, EmailStatusFailed, EmailStatusSending, now).
		Updates(map[string]interface{}{
			"status":           EmailStatusCancelled,
			"cancelled_at":     &now,
			"claim_token":      nil,
			"claimed_at":       nil,
			"claim_expires_at": nil,
		})
	if result.Error != nil {
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		return nil, s.notRetryableOrCancellableError(id, ErrNotCancellable)
	}

	return s.GetEmail(id)
}

// notRetryableOrCancellableError distinguishes "id doesn't exist at all"
// from "id exists but isn't in a state RetryEmail/CancelEmail can act on"
// after their conditional UPDATE affected 0 rows.
func (s *Service) notRetryableOrCancellableError(id string, wrongStateErr error) error {
	email, err := s.GetEmail(id)
	if err != nil {
		return err
	}
	if email == nil {
		return ErrNotFound
	}
	return wrongStateErr
}

// TemplateInfo is the admin API's view of a template's effective content.
type TemplateInfo struct {
	ID           string
	Subject      string
	BodyText     string
	BodyHTML     *string
	IsCustomized bool
	Description  string
}

func (s *Service) ListTemplates() ([]TemplateInfo, error) {

	s.templates.mu.RLock()
	ids := make([]string, 0, len(s.templates.items))
	for id := range s.templates.items {
		ids = append(ids, id)
	}
	s.templates.mu.RUnlock()

	sort.Strings(ids)

	items := make([]TemplateInfo, 0, len(ids))
	for _, id := range ids {
		info, err := s.GetTemplateInfo(id)
		if err != nil {
			return nil, err
		}
		items = append(items, *info)
	}

	return items, nil
}

// GetTemplateInfo returns (nil, nil) when id was never registered by any
// module, matching this codebase's not-found convention.
func (s *Service) GetTemplateInfo(id string) (*TemplateInfo, error) {
	tmpl, err := s.resolveTemplate(s.db, id)
	if err != nil {
		if errors.Is(err, ErrTemplateNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &TemplateInfo{
		ID:           id,
		Subject:      tmpl.subject,
		BodyText:     tmpl.bodyText,
		BodyHTML:     tmpl.bodyHTML,
		IsCustomized: tmpl.isCustomized,
		Description:  tmpl.description,
	}, nil
}

// UpdateTemplate validates the submitted content (the same check
// RegisterTemplate applies to a module's own defaults) before writing an
// override row — an admin can never persist a template that would only
// fail to parse later, silently, in the background tick.
func (s *Service) UpdateTemplate(id, subject, bodyText string, bodyHTML *string) (*TemplateInfo, error) {

	if err := s.templateMustExist(id); err != nil {
		return nil, err
	}

	if err := validateTemplateContent(subject, bodyText, bodyHTML); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTemplateInvalid, err)
	}

	override := Template{TemplateID: id, Subject: subject, BodyText: bodyText, BodyHTML: bodyHTML}
	err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "template_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"subject", "body_text", "body_html"}),
	}).Create(&override).Error
	if err != nil {
		return nil, err
	}

	return s.GetTemplateInfo(id)
}

// ResetTemplate deletes id's override row, if any, reverting to its
// module-registered default.
func (s *Service) ResetTemplate(id string) error {

	if err := s.templateMustExist(id); err != nil {
		return err
	}

	return s.db.Where("template_id = ?", id).Delete(&Template{}).Error
}

func (s *Service) templateMustExist(id string) error {
	s.templates.mu.RLock()
	_, registered := s.templates.items[id]
	s.templates.mu.RUnlock()
	if registered {
		return nil
	}

	var existing Template
	err := s.db.Where("template_id = ?", id).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrTemplateNotFound
	}
	return err
}
