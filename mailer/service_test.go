package mailer

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/netgarden/maf/security/encryption"
)

// testEncryption is shared by every test in this file — encryption is an
// internal storage-layer detail, so unless a test specifically exercises
// it (see TestEnqueue_EncryptsBodyAtRest / TestGetEmail_WrongKeyFailsToDecrypt),
// this being a fixed, known manager is not itself load-bearing.
func testEncryption() *encryption.Manager {
	return encryption.NewManager("test-encryption-secret")
}

// testDB connects to a real Postgres database and migrates mailer's
// tables. See README.md for what MAF_MAILER_TEST_DSN should point at.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("MAF_MAILER_TEST_DSN")
	if dsn == "" {
		t.Skip("MAF_MAILER_TEST_DSN not set; skipping mailer integration tests (see README.md)")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(&Email{}, &Template{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	reset := func() {
		db.Exec("DELETE FROM mailer_emails")
		db.Exec("DELETE FROM mailer_templates")
	}
	reset()
	t.Cleanup(reset)

	return db
}

// fakeSender is a test double for Sender. Send returns err (nil for
// success) and records every message it was called with.
type fakeSender struct {
	mu      sync.Mutex
	err     error
	sendLog []*Message
}

func (f *fakeSender) Send(ctx context.Context, msg *Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendLog = append(f.sendLog, msg)
	return f.err
}

func (f *fakeSender) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sendLog)
}

func testRetryConfigFast() RetryConfig {
	return RetryConfig{
		// Long enough that WithJitter's -20% floor still comfortably
		// exceeds the DB round-trip time between finalizing a failed send
		// and a test asserting NextAttemptAt is still in the future.
		InitialInterval: 5 * time.Second,
		Multiplier:      2,
		MaxInterval:     time.Minute,
		MaxAge:          time.Hour,
	}
}

func newTestServiceWithSender(t *testing.T, db *gorm.DB, sender Sender) *Service {
	t.Helper()
	return NewService(db, sender, testRetryConfigFast(), 20, time.Minute, testEncryption())
}

func TestEnqueue_PersistsCorrectly(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	html := "<p>hi</p>"
	email, err := svc.Enqueue(&EnqueueRequest{
		To:       []string{"a@example.com"},
		Cc:       []string{"b@example.com"},
		Bcc:      []string{"c@example.com"},
		Subject:  "Hello",
		BodyText: "hi",
		BodyHTML: &html,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if email.Status != EmailStatusQueued {
		t.Errorf("expected status queued, got %v", email.Status)
	}

	got, err := svc.GetEmail(email.ID.String())
	if err != nil {
		t.Fatalf("GetEmail: %v", err)
	}
	if got == nil {
		t.Fatal("expected email to be found")
	}
	if len(got.To) != 1 || got.To[0] != "a@example.com" {
		t.Errorf("To not persisted correctly: %v", got.To)
	}
	if len(got.Cc) != 1 || len(got.Bcc) != 1 {
		t.Errorf("Cc/Bcc not persisted correctly: %v / %v", got.Cc, got.Bcc)
	}
	if got.BodyHTML == nil || *got.BodyHTML != html {
		t.Errorf("BodyHTML not persisted correctly: %v", got.BodyHTML)
	}
}

// TestEnqueue_EncryptsBodyAtRest is the actual proof that body encryption
// isn't a no-op: it reads the raw DB row directly, bypassing
// Service.GetEmail's decryption entirely, and checks the stored value.
func TestEnqueue_EncryptsBodyAtRest(t *testing.T) {
	db := testDB(t)
	enc := testEncryption()
	svc := NewService(db, &fakeSender{}, testRetryConfigFast(), 20, time.Minute, enc)

	plainText := "your temporary password is: correct-horse-battery-staple"
	html := "<p>your temporary password is: correct-horse-battery-staple</p>"
	email, err := svc.Enqueue(&EnqueueRequest{
		To:       []string{"a@example.com"},
		Subject:  "Hello",
		BodyText: plainText,
		BodyHTML: &html,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// The value returned directly from Enqueue must already be plaintext —
	// Service's public API is plaintext in, plaintext out.
	if email.BodyText != plainText {
		t.Errorf("expected Enqueue's own return value to be plaintext, got: %q", email.BodyText)
	}

	var rawBodyText, rawBodyHTML string
	err = db.Raw("SELECT body_text, body_html FROM mailer_emails WHERE id = ?", email.ID).
		Row().Scan(&rawBodyText, &rawBodyHTML)
	if err != nil {
		t.Fatalf("raw row query: %v", err)
	}

	if rawBodyText == plainText {
		t.Fatal("expected the stored body_text column to be ciphertext, not plaintext")
	}
	if rawBodyHTML == html {
		t.Fatal("expected the stored body_html column to be ciphertext, not plaintext")
	}

	rawBodyTextBytes, err := base64.StdEncoding.DecodeString(rawBodyText)
	if err != nil {
		t.Fatalf("base64-decode rawBodyText: %v", err)
	}
	decryptedText, err := enc.Decrypt(rawBodyTextBytes)
	if err != nil {
		t.Fatalf("Decrypt(rawBodyText): %v", err)
	}
	if string(decryptedText) != plainText {
		t.Errorf("decrypting the raw stored value did not recover the original plaintext: got %q, want %q", decryptedText, plainText)
	}
}

// TestGetEmail_WrongKeyFailsToDecrypt is the key-rotation-shaped failure
// mode: a Service configured with a different encryption secret than the
// one that wrote a row must surface a decrypt error, not silently corrupt
// or crash.
func TestGetEmail_WrongKeyFailsToDecrypt(t *testing.T) {
	db := testDB(t)
	writer := NewService(db, &fakeSender{}, testRetryConfigFast(), 20, time.Minute, encryption.NewManager("secret-a"))

	email, err := writer.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "sensitive content"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	reader := NewService(db, &fakeSender{}, testRetryConfigFast(), 20, time.Minute, encryption.NewManager("secret-b"))

	if _, err := reader.GetEmail(email.ID.String()); err == nil {
		t.Fatal("expected GetEmail to fail to decrypt a row written under a different encryption secret")
	}
}

func TestClaimBatch_ConcurrentClaimersGetDisjointSets(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	const numEmails = 20
	want := make(map[string]bool, numEmails)
	for i := 0; i < numEmails; i++ {
		email, err := svc.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		want[email.ID.String()] = true
	}

	// Two Service instances with their own DB connections, standing in for
	// two application replicas racing to claim the same batch.
	db2, err := gorm.Open(postgres.Open(os.Getenv("MAF_MAILER_TEST_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to open second connection: %v", err)
	}
	svc1 := NewService(db, &fakeSender{}, testRetryConfigFast(), numEmails, time.Minute, testEncryption())
	svc2 := NewService(db2, &fakeSender{}, testRetryConfigFast(), numEmails, time.Minute, testEncryption())

	var wg sync.WaitGroup
	var mu sync.Mutex
	claimedBy := make(map[string]int) // id -> which claimer got it (1 or 2)

	claim := func(svc *Service, who int) {
		defer wg.Done()
		claimed, err := svc.claimBatch()
		if err != nil {
			t.Errorf("claimBatch (%d): %v", who, err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, e := range claimed {
			claimedBy[e.ID.String()] = who
		}
	}

	wg.Add(2)
	go claim(svc1, 1)
	go claim(svc2, 2)
	wg.Wait()

	if len(claimedBy) != numEmails {
		t.Fatalf("expected all %d emails claimed exactly once total, got %d claimed", numEmails, len(claimedBy))
	}
	for id := range want {
		if _, ok := claimedBy[id]; !ok {
			t.Errorf("email %s was never claimed by either claimer", id)
		}
	}
}

func TestClaimBatch_AbandonedClaimIsReclaimable(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	email, err := svc.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Simulate a stale claim from a replica that crashed mid-send: status
	// "sending" with a claim_expires_at already in the past.
	past := time.Now().Add(-time.Hour)
	err = db.Model(&Email{}).Where("id = ?", email.ID).Updates(map[string]interface{}{
		"status":           EmailStatusSending,
		"claim_expires_at": &past,
	}).Error
	if err != nil {
		t.Fatalf("failed to simulate stale claim: %v", err)
	}

	claimed, err := svc.claimBatch()
	if err != nil {
		t.Fatalf("claimBatch: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != email.ID {
		t.Fatalf("expected the abandoned email to be reclaimed, got %d claimed", len(claimed))
	}
}

func TestRetryEmail_StaleSendingSucceeds_LiveSendingFails(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	stale, err := svc.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if err := db.Model(&Email{}).Where("id = ?", stale.ID).Updates(map[string]interface{}{
		"status": EmailStatusSending, "claim_expires_at": &past,
	}).Error; err != nil {
		t.Fatalf("failed to simulate stale claim: %v", err)
	}

	if _, err := svc.RetryEmail(stale.ID.String()); err != nil {
		t.Errorf("expected retrying a stale sending email to succeed, got: %v", err)
	}

	live, err := svc.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	future := time.Now().Add(time.Hour)
	if err := db.Model(&Email{}).Where("id = ?", live.ID).Updates(map[string]interface{}{
		"status": EmailStatusSending, "claim_expires_at": &future,
	}).Error; err != nil {
		t.Fatalf("failed to simulate live claim: %v", err)
	}

	// This is the literal proof that a tick mid-send and an admin retry
	// can't race the same email: a currently (non-expired) claimed row
	// must not be retryable out from under whatever's sending it.
	_, err = svc.RetryEmail(live.ID.String())
	if !errors.Is(err, ErrNotRetryable) {
		t.Errorf("expected ErrNotRetryable for a live sending email, got: %v", err)
	}
}

func TestRetryEmail_UnknownID_ReturnsNotFound(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	_, err := svc.RetryEmail("00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestCancelEmail_QueuedSucceeds_SentFails(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	email, err := svc.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if _, err := svc.CancelEmail(email.ID.String()); err != nil {
		t.Errorf("expected cancelling a queued email to succeed, got: %v", err)
	}

	sent, err := svc.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := db.Model(&Email{}).Where("id = ?", sent.ID).Update("status", EmailStatusSent).Error; err != nil {
		t.Fatalf("failed to mark email sent: %v", err)
	}

	_, err = svc.CancelEmail(sent.ID.String())
	if !errors.Is(err, ErrNotCancellable) {
		t.Errorf("expected ErrNotCancellable for an already-sent email, got: %v", err)
	}
}

func TestProcessBatch_SuccessPath(t *testing.T) {
	db := testDB(t)
	sender := &fakeSender{}
	svc := newTestServiceWithSender(t, db, sender)

	email, err := svc.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := svc.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	got, err := svc.GetEmail(email.ID.String())
	if err != nil {
		t.Fatalf("GetEmail: %v", err)
	}
	if got.Status != EmailStatusSent {
		t.Errorf("expected status sent, got %v", got.Status)
	}
	if got.SentAt == nil {
		t.Error("expected SentAt to be set")
	}
	if sender.callCount() != 1 {
		t.Errorf("expected exactly 1 send attempt, got %d", sender.callCount())
	}
}

func TestProcessBatch_FailureReschedules(t *testing.T) {
	db := testDB(t)
	sender := &fakeSender{err: errors.New("smtp: connection refused")}
	svc := newTestServiceWithSender(t, db, sender)

	email, err := svc.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := svc.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	got, err := svc.GetEmail(email.ID.String())
	if err != nil {
		t.Fatalf("GetEmail: %v", err)
	}
	if got.Status != EmailStatusQueued {
		t.Errorf("expected status queued (rescheduled) after a failed send, got %v", got.Status)
	}
	if got.Attempts != 1 {
		t.Errorf("expected 1 attempt recorded, got %d", got.Attempts)
	}
	if got.LastError == nil || *got.LastError != sender.err.Error() {
		t.Errorf("expected LastError to be recorded, got %v", got.LastError)
	}
	if !got.NextAttemptAt.After(time.Now()) {
		t.Error("expected NextAttemptAt to be rescheduled into the future")
	}
}

func TestProcessBatch_GivesUpPastMaxAge(t *testing.T) {
	db := testDB(t)
	sender := &fakeSender{err: errors.New("smtp: connection refused")}
	svc := newTestServiceWithSender(t, db, sender)

	email, err := svc.Enqueue(&EnqueueRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	longAgo := time.Now().Add(-2 * time.Hour) // testRetryConfigFast's MaxAge is 1 hour
	if err := db.Model(&Email{}).Where("id = ?", email.ID).Update("created_at", longAgo).Error; err != nil {
		t.Fatalf("failed to backdate email: %v", err)
	}

	if err := svc.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	got, err := svc.GetEmail(email.ID.String())
	if err != nil {
		t.Fatalf("GetEmail: %v", err)
	}
	if got.Status != EmailStatusFailed {
		t.Errorf("expected status failed (gave up), got %v", got.Status)
	}
	if got.GaveUpAt == nil {
		t.Error("expected GaveUpAt to be set")
	}
}

func TestEnqueueTemplate_EndToEnd(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	err := svc.RegisterTemplate(TemplateDefault{
		ID:       "test.welcome",
		Subject:  "Welcome {{.Name}}",
		BodyText: "Hi {{.Name}}, your code is {{.Code}}.",
	})
	if err != nil {
		t.Fatalf("RegisterTemplate: %v", err)
	}

	email, err := svc.EnqueueTemplate("test.welcome", []string{"a@example.com"}, nil, nil, map[string]any{
		"Name": "Alice", "Code": "123456",
	})
	if err != nil {
		t.Fatalf("EnqueueTemplate: %v", err)
	}
	if email.Subject != "Welcome Alice" {
		t.Errorf("subject not rendered: %q", email.Subject)
	}
	if email.BodyText != "Hi Alice, your code is 123456." {
		t.Errorf("bodyText not rendered: %q", email.BodyText)
	}

	// Overriding the template changes what a subsequent EnqueueTemplate call
	// renders, without affecting the email already enqueued above.
	if _, err := svc.UpdateTemplate("test.welcome", "New subject {{.Name}}", "New body {{.Name}}", nil); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}

	email2, err := svc.EnqueueTemplate("test.welcome", []string{"a@example.com"}, nil, nil, map[string]any{"Name": "Bob"})
	if err != nil {
		t.Fatalf("EnqueueTemplate (after override): %v", err)
	}
	if email2.Subject != "New subject Bob" {
		t.Errorf("expected the override to be used, got subject: %q", email2.Subject)
	}

	unchanged, err := svc.GetEmail(email.ID.String())
	if err != nil {
		t.Fatalf("GetEmail: %v", err)
	}
	if unchanged.Subject != "Welcome Alice" {
		t.Errorf("expected the already-enqueued email to be unaffected by the later override, got: %q", unchanged.Subject)
	}

	// Resetting reverts to the registered default for further enqueues.
	if err := svc.ResetTemplate("test.welcome"); err != nil {
		t.Fatalf("ResetTemplate: %v", err)
	}
	info, err := svc.GetTemplateInfo("test.welcome")
	if err != nil {
		t.Fatalf("GetTemplateInfo: %v", err)
	}
	if info.IsCustomized {
		t.Error("expected IsCustomized to be false after reset")
	}
	if info.Subject != "Welcome {{.Name}}" {
		t.Errorf("expected the default subject template to be restored, got: %q", info.Subject)
	}
}

func TestUpdateTemplate_RejectsInvalidSyntax(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	if err := svc.RegisterTemplate(TemplateDefault{ID: "test.x", Subject: "ok", BodyText: "ok"}); err != nil {
		t.Fatalf("RegisterTemplate: %v", err)
	}

	_, err := svc.UpdateTemplate("test.x", "ok", "Hello {{.Name", nil)
	if !errors.Is(err, ErrTemplateInvalid) {
		t.Errorf("expected ErrTemplateInvalid, got: %v", err)
	}

	// The invalid override must not have been persisted.
	info, err := svc.GetTemplateInfo("test.x")
	if err != nil {
		t.Fatalf("GetTemplateInfo: %v", err)
	}
	if info.IsCustomized {
		t.Error("expected the rejected update to not have been persisted as a customization")
	}
}

func TestUpdateTemplate_UnknownID_ReturnsNotFound(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	_, err := svc.UpdateTemplate("nonexistent.template", "s", "b", nil)
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("expected ErrTemplateNotFound, got: %v", err)
	}
}
