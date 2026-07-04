package mailer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNotFound        = errors.New("mailer: email not found")
	ErrNotRetryable    = errors.New("mailer: email is not in a retryable state")
	ErrNotCancellable  = errors.New("mailer: email is not in a cancellable state")
	ErrTemplateInvalid = errors.New("mailer: template failed to parse")
)

func NewService(db *gorm.DB, sender Sender, retryCfg RetryConfig, batchSize int, claimTimeout time.Duration) *Service {
	return &Service{
		db:           db,
		sender:       sender,
		retryCfg:     retryCfg,
		batchSize:    batchSize,
		claimTimeout: claimTimeout,
		templates:    newTemplateRegistry(),
	}
}

type Service struct {
	db           *gorm.DB
	sender       Sender
	retryCfg     RetryConfig
	batchSize    int
	claimTimeout time.Duration
	templates    *templateRegistry
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
	if err := tx.Create(email).Error; err != nil {
		return nil, err
	}
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
// using SELECT ... FOR UPDATE SKIP LOCKED, then loads the full rows via a
// normal GORM Find — done as two statements in one transaction, rather
// than a single UPDATE ... RETURNING * scanned directly, so GORM's usual
// field/serializer handling (needed for the JSON-serialized To/Cc/Bcc
// columns) is guaranteed to apply when populating the returned structs.
func (s *Service) claimBatch() ([]Email, error) {

	claimToken := uuid.NewV4().String()
	now := time.Now()
	claimExpiresAt := now.Add(s.claimTimeout)

	var claimed []Email

	err := s.db.Transaction(func(tx *gorm.DB) error {

		var ids []uuid.UUID
		err := tx.Raw(`
			UPDATE mailer_emails SET status = ?, claim_token = ?, claimed_at = ?,
				claim_expires_at = ?, attempts = attempts + 1, last_attempt_at = ?
			WHERE id IN (
				SELECT id FROM mailer_emails
				WHERE (status = ? AND next_attempt_at <= ?)
				   OR (status = ? AND claim_expires_at < ?)
				ORDER BY next_attempt_at
				LIMIT ?
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id`,
			EmailStatusSending, claimToken, now, claimExpiresAt, now,
			EmailStatusQueued, now,
			EmailStatusSending, now,
			s.batchSize,
		).Scan(&ids).Error
		if err != nil {
			return err
		}

		if len(ids) == 0 {
			return nil
		}

		return tx.Where("id IN ?", ids).Find(&claimed).Error
	})

	return claimed, err
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
