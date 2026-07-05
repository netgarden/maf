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

	"github.com/netgarden/maf/security/encryption"
)

var (
	ErrNotFound        = errors.New("mailer: email not found")
	ErrNotRetryable    = errors.New("mailer: email is not in a retryable state")
	ErrNotCancellable  = errors.New("mailer: email is not in a cancellable state")
	ErrTemplateInvalid = errors.New("mailer: template failed to parse")
)

func NewService(db *gorm.DB, sender Sender, retryCfg RetryConfig, batchSize int, claimTimeout time.Duration, encryptionManager *encryption.Manager) *Service {
	return &Service{
		db:           db,
		sender:       sender,
		retryCfg:     retryCfg,
		batchSize:    batchSize,
		claimTimeout: claimTimeout,
		templates:    newTemplateRegistry(),
		encryption:   encryptionManager,
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
	db           *gorm.DB
	sender       Sender
	retryCfg     RetryConfig
	batchSize    int
	claimTimeout time.Duration
	templates    *templateRegistry
	encryption   *encryption.Manager
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

// EnqueueRequest is the content of a not-yet-rendered email to queue.
type EnqueueRequest struct {
	To       []string
	Cc       []string
	Bcc      []string
	Subject  string
	BodyText string
	BodyHTML *string
}

func (s *Service) Enqueue(req *EnqueueRequest) (*Email, error) {
	return s.EnqueueTx(s.db, req)
}

// EnqueueTx enqueues within an existing transaction, so a caller can enqueue
// an email inside the same transaction as the business action that
// triggers it — a rollback of that transaction also rolls back the
// enqueue.
func (s *Service) EnqueueTx(tx *gorm.DB, req *EnqueueRequest) (*Email, error) {
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

// EnqueueTemplate resolves templateID (an admin override if one exists,
// else the module-registered default), renders it with data, and enqueues
// the result exactly like Enqueue — from this point on there's no
// difference between a templated and a directly-enqueued email.
func (s *Service) EnqueueTemplate(templateID string, to, cc, bcc []string, data any) (*Email, error) {
	return s.EnqueueTemplateTx(s.db, templateID, to, cc, bcc, data)
}

func (s *Service) EnqueueTemplateTx(tx *gorm.DB, templateID string, to, cc, bcc []string, data any) (*Email, error) {
	tmpl, err := s.resolveTemplate(tx, templateID)
	if err != nil {
		return nil, err
	}

	subject, bodyText, bodyHTML, err := renderTemplate(tmpl, data)
	if err != nil {
		return nil, err
	}

	return s.EnqueueTx(tx, &EnqueueRequest{
		To: to, Cc: cc, Bcc: bcc,
		Subject: subject, BodyText: bodyText, BodyHTML: bodyHTML,
	})
}

// ProcessBatch claims and attempts to send up to batchSize due emails. It's
// called from a jobs.Handler (see handler.go) on a recurring tick,
// coordinated across replicas by jobs' own per-tick lease — but the
// per-row claimBatch below is what actually prevents any single email from
// being sent twice, since an admin-triggered RetryEmail/CancelEmail call
// can land on a different replica than whichever one is mid-tick.
func (s *Service) ProcessBatch(ctx context.Context) error {

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

// ListEmailsFilter is the admin API's list/filter/pagination request.
type ListEmailsFilter struct {
	Status   string
	Search   string
	Page     int
	PageSize int
}

type ListEmailsResult struct {
	Items    []Email
	Total    int64
	Page     int
	PageSize int
}

func (s *Service) ListEmails(filter ListEmailsFilter) (*ListEmailsResult, error) {

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	}

	query := s.db.Model(&Email{})
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Search != "" {
		query = query.Where("subject ILIKE ?", "%"+filter.Search+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var items []Email
	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	if err != nil {
		return nil, err
	}

	for i := range items {
		if err := s.decryptBody(&items[i]); err != nil {
			return nil, err
		}
	}

	return &ListEmailsResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
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
