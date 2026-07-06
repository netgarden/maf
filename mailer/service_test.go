package mailer

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/netgarden/maf/datatables"
	"github.com/netgarden/maf/security/encryption"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testEncryption is shared by every test in this file — encryption is an
// internal storage-layer detail, so unless a test specifically exercises
// it (see TestSend_EncryptsBodyAtRest / TestGetEmail_WrongKeyFailsToDecrypt),
// this being a fixed, known manager is not itself load-bearing.
func testEncryption() *encryption.Manager {
	return encryption.NewManager("test-encryption-secret")
}

// Postgres (testDSN/testDSNErr) and Mailpit (testSMTPHost/testSMTPPort/
// testMailpitAPIBase/testMailpitErr) are both started once by TestMain,
// before any test in this package runs — see TestMain.
var (
	testDSN    string
	testDSNErr error

	testSMTPHost       string
	testSMTPPort       int
	testMailpitAPIBase string
	testMailpitErr     error
)

// TestMain starts one disposable Postgres container (for Service's own
// tests, in this file) and one disposable Mailpit container (for
// SMTPSender's tests, in smtp_test.go) for this package's entire test run,
// instead of requiring a human to have already started either beforehand.
// Both are reused by every test in this package and torn down once, after
// all tests finish — cheaper than a container-per-test and matching this
// suite's previous "one long-lived dev Postgres/SMTP server shared by the
// whole run" performance characteristics. testDB resets its tables between
// individual tests, and smtp_test.go's tests clear Mailpit's mailbox
// between tests — that's what actually gives tests isolation from each
// other, same as before.
func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, pgErr := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
	)
	if pgErr != nil {
		testDSNErr = pgErr
	} else {
		testDSN, testDSNErr = pg.ConnectionString(ctx, "sslmode=disable")
		if testDSNErr == nil {
			testDSNErr = waitForPostgresReady(testDSN)
		}
	}

	mailpit, mpErr := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "axllent/mailpit:v1.30.3",
			ExposedPorts: []string{"1025/tcp", "8025/tcp"},
			WaitingFor:   wait.ForListeningPort("8025/tcp"),
		},
		Started: true,
	})
	if mpErr != nil {
		testMailpitErr = mpErr
	} else {
		host, hostErr := mailpit.Host(ctx)
		smtpPort, smtpErr := mailpit.MappedPort(ctx, "1025/tcp")
		httpPort, httpErr := mailpit.MappedPort(ctx, "8025/tcp")
		switch {
		case hostErr != nil:
			testMailpitErr = hostErr
		case smtpErr != nil:
			testMailpitErr = smtpErr
		case httpErr != nil:
			testMailpitErr = httpErr
		default:
			testSMTPHost = host
			testSMTPPort = int(smtpPort.Num())
			testMailpitAPIBase = "http://" + host + ":" + httpPort.Port()
		}
	}

	code := m.Run()

	if pg != nil {
		_ = pg.Terminate(ctx)
	}
	if mailpit != nil {
		_ = mailpit.Terminate(ctx)
	}
	os.Exit(code)
}

// waitForPostgresReady retries a plain ping for a few seconds — the
// container's own readiness signal (log line + port check) fires slightly
// before the mapped port reliably accepts connections in this environment,
// so the first real connection attempt can otherwise land in that gap.
func waitForPostgresReady(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	deadline := time.Now().Add(20 * time.Second)
	var pingErr error
	for time.Now().Before(deadline) {
		if pingErr = db.Ping(); pingErr == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return pingErr
}

// testDB connects to the Postgres container TestMain started and migrates
// mailer's tables.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	if testDSNErr != nil {
		t.Skipf("postgres testcontainer unavailable (Docker required): %v", testDSNErr)
	}

	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(&Email{}, &Template{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	reset := func() {
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Email{})
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Template{})
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

// testDirectSendTimeoutFast bounds Send's background direct-delivery
// attempt (see service.go's tryDeliverDirect) so a test with a hanging fake
// sender fails fast rather than hanging the suite.
const testDirectSendTimeoutFast = 2 * time.Second

func newTestServiceWithSender(t *testing.T, db *gorm.DB, sender Sender) *Service {
	t.Helper()
	return NewService(db, sender, testRetryConfigFast(), 20, time.Minute, testEncryption(), testDirectSendTimeoutFast)
}

func TestSend_PersistsCorrectly(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	html := "<p>hi</p>"
	email, err := svc.Send(&SendRequest{
		To:       []string{"a@example.com"},
		Cc:       []string{"b@example.com"},
		Bcc:      []string{"c@example.com"},
		Subject:  "Hello",
		BodyText: "hi",
		BodyHTML: &html,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
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

// TestSend_EncryptsBodyAtRest is the actual proof that body encryption
// isn't a no-op: it reads the raw DB row directly, bypassing
// Service.GetEmail's decryption entirely, and checks the stored value.
func TestSend_EncryptsBodyAtRest(t *testing.T) {
	db := testDB(t)
	enc := testEncryption()
	svc := NewService(db, &fakeSender{}, testRetryConfigFast(), 20, time.Minute, enc, testDirectSendTimeoutFast)

	plainText := "your temporary password is: correct-horse-battery-staple"
	html := "<p>your temporary password is: correct-horse-battery-staple</p>"
	email, err := svc.Send(&SendRequest{
		To:       []string{"a@example.com"},
		Subject:  "Hello",
		BodyText: plainText,
		BodyHTML: &html,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The value returned directly from Send must already be plaintext —
	// Service's public API is plaintext in, plaintext out.
	if email.BodyText != plainText {
		t.Errorf("expected Send's own return value to be plaintext, got: %q", email.BodyText)
	}

	var raw Email
	err = db.Select("body_text", "body_html").First(&raw, "id = ?", email.ID).Error
	if err != nil {
		t.Fatalf("raw row query: %v", err)
	}
	rawBodyText := raw.BodyText

	if rawBodyText == plainText {
		t.Fatal("expected the stored body_text column to be ciphertext, not plaintext")
	}
	if raw.BodyHTML == nil || *raw.BodyHTML == html {
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
	writer := NewService(db, &fakeSender{}, testRetryConfigFast(), 20, time.Minute, encryption.NewManager("secret-a"), testDirectSendTimeoutFast)

	email, err := writer.Send(&SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "sensitive content"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	reader := NewService(db, &fakeSender{}, testRetryConfigFast(), 20, time.Minute, encryption.NewManager("secret-b"), testDirectSendTimeoutFast)

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
		// EnqueueTx (not Send) so this fixture setup doesn't race the new
		// direct-send goroutine — this test is specifically about claimBatch,
		// not enqueue-time delivery.
		email, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
		if err != nil {
			t.Fatalf("EnqueueTx: %v", err)
		}
		want[email.ID.String()] = true
	}

	// Two Service instances with their own DB connections, standing in for
	// two application replicas racing to claim the same batch.
	db2, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to open second connection: %v", err)
	}
	svc1 := NewService(db, &fakeSender{}, testRetryConfigFast(), numEmails, time.Minute, testEncryption(), testDirectSendTimeoutFast)
	svc2 := NewService(db2, &fakeSender{}, testRetryConfigFast(), numEmails, time.Minute, testEncryption(), testDirectSendTimeoutFast)

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

	// EnqueueTx (not Send) so this fixture setup doesn't race the new
	// direct-send goroutine before the test simulates a stale claim below.
	email, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
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

	// EnqueueTx (not Send) so these fixtures don't race the new
	// direct-send goroutine before the test manually claims/checks them.
	stale, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
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

	live, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
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

	// EnqueueTx (not Send) so this fixture setup doesn't race the new
	// direct-send goroutine before CancelEmail runs below.
	email, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}

	if _, err := svc.CancelEmail(email.ID.String()); err != nil {
		t.Errorf("expected cancelling a queued email to succeed, got: %v", err)
	}

	sent, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
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

	// EnqueueTx (not Send) so ProcessBatch below is the only thing that
	// ever claims/sends this row — this test is about ProcessBatch, not the
	// new direct-send fast path.
	email, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
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

	// EnqueueTx (not Send) so ProcessBatch below is the only thing that
	// ever claims/sends this row — this test is about ProcessBatch, not the
	// new direct-send fast path.
	email, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
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

	// EnqueueTx (not Send) so ProcessBatch below is the only thing that
	// ever claims/sends this row — this test is about ProcessBatch, not the
	// new direct-send fast path.
	email, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
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

func TestSendTemplate_EndToEnd(t *testing.T) {
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

	email, err := svc.SendTemplate("test.welcome", []string{"a@example.com"}, nil, nil, map[string]any{
		"Name": "Alice", "Code": "123456",
	})
	if err != nil {
		t.Fatalf("SendTemplate: %v", err)
	}
	if email.Subject != "Welcome Alice" {
		t.Errorf("subject not rendered: %q", email.Subject)
	}
	if email.BodyText != "Hi Alice, your code is 123456." {
		t.Errorf("bodyText not rendered: %q", email.BodyText)
	}

	// Overriding the template changes what a subsequent SendTemplate call
	// renders, without affecting the email already enqueued above.
	if _, err := svc.UpdateTemplate("test.welcome", "New subject {{.Name}}", "New body {{.Name}}", nil); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}

	email2, err := svc.SendTemplate("test.welcome", []string{"a@example.com"}, nil, nil, map[string]any{"Name": "Bob"})
	if err != nil {
		t.Fatalf("SendTemplate (after override): %v", err)
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

// waitForEmailStatus polls GetEmail until pred(email) is true or deadline
// elapses, for asserting on Send's asynchronous direct-send goroutine
// (see service.go's tryDeliverDirect) without a synchronization hook into
// production code.
func waitForEmailStatus(t *testing.T, svc *Service, id string, pred func(*Email) bool) *Email {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := svc.GetEmail(id)
		if err != nil {
			t.Fatalf("GetEmail: %v", err)
		}
		if got != nil && pred(got) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for email %s to reach the expected state; last seen: %+v", id, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSend_DeliversDirectly proves Send attempts delivery immediately
// in the background, without ever calling ProcessBatch.
func TestSend_DeliversDirectly(t *testing.T) {
	db := testDB(t)
	sender := &fakeSender{}
	svc := newTestServiceWithSender(t, db, sender)

	email, err := svc.Send(&SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := waitForEmailStatus(t, svc, email.ID.String(), func(e *Email) bool {
		return e.Status == EmailStatusSent
	})
	if got.SentAt == nil {
		t.Error("expected SentAt to be set")
	}
	if sender.callCount() != 1 {
		t.Errorf("expected exactly 1 send attempt, got %d", sender.callCount())
	}
}

// TestSend_DirectSendFailureFallsBackToQueue proves a failed direct-send
// attempt falls back through the exact same finalizeFailure/backoff path a
// failed tick attempt would, leaving the row queued for the next tick
// rather than stuck or lost.
func TestSend_DirectSendFailureFallsBackToQueue(t *testing.T) {
	db := testDB(t)
	sender := &fakeSender{err: errors.New("smtp: connection refused")}
	svc := newTestServiceWithSender(t, db, sender)

	email, err := svc.Send(&SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// claimByID increments Attempts at claim time, while status is still
	// "sending" — wait for finalizeFailure to actually complete and put the
	// row back to "queued" with NextAttemptAt rescheduled, not just for
	// Attempts to tick up mid-flight.
	got := waitForEmailStatus(t, svc, email.ID.String(), func(e *Email) bool {
		return e.Status == EmailStatusQueued && e.Attempts >= 1
	})
	if got.LastError == nil || *got.LastError != sender.err.Error() {
		t.Errorf("expected LastError to be recorded, got %v", got.LastError)
	}
	if !got.NextAttemptAt.After(time.Now()) {
		t.Error("expected NextAttemptAt to be rescheduled into the future")
	}
}

// TestClaimByID_AlreadyClaimedRow_IsNoOp is the concrete proof that a
// direct-send attempt racing the periodic tick can never double-claim: once
// a row is no longer in EmailStatusQueued, claimByID must return (nil, nil)
// rather than claiming it again.
func TestClaimByID_AlreadyClaimedRow_IsNoOp(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	email, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}

	// Simulate the periodic tick's claimBatch having claimed this row first.
	if err := db.Model(&Email{}).Where("id = ?", email.ID).Update("status", EmailStatusSending).Error; err != nil {
		t.Fatalf("failed to simulate a live claim: %v", err)
	}

	claimed, err := svc.claimByID(email.ID.String())
	if err != nil {
		t.Fatalf("claimByID: %v", err)
	}
	if claimed != nil {
		t.Errorf("expected claimByID to be a no-op on an already-claimed row, got: %+v", claimed)
	}
}

// makeTerminalEmail enqueues (via EnqueueTx, so it never races the
// direct-send goroutine) and then stamps it directly into a terminal
// status with the given timestamp, simulating an email that reached that
// state a while ago.
func makeTerminalEmail(t *testing.T, db *gorm.DB, svc *Service, status EmailStatus, timestampColumn string, at time.Time) *Email {
	t.Helper()
	email, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	err = db.Model(&Email{}).Where("id = ?", email.ID).Updates(map[string]interface{}{
		"status":        status,
		timestampColumn: &at,
	}).Error
	if err != nil {
		t.Fatalf("failed to stamp terminal email: %v", err)
	}
	return email
}

func TestPurgeOldEmails_DeletesOldSentAndCancelled_KeepsRecent(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	cfg := RetentionConfig{MaxAge: time.Hour, FailedMaxAge: 48 * time.Hour}
	now := time.Now()

	oldSent := makeTerminalEmail(t, db, svc, EmailStatusSent, "sent_at", now.Add(-2*time.Hour))
	oldCancelled := makeTerminalEmail(t, db, svc, EmailStatusCancelled, "cancelled_at", now.Add(-2*time.Hour))
	recentSent := makeTerminalEmail(t, db, svc, EmailStatusSent, "sent_at", now.Add(-10*time.Minute))

	deleted, err := svc.PurgeOldEmails(cfg)
	if err != nil {
		t.Fatalf("PurgeOldEmails: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 rows deleted, got %d", deleted)
	}

	for _, id := range []string{oldSent.ID.String(), oldCancelled.ID.String()} {
		got, err := svc.GetEmail(id)
		if err != nil {
			t.Fatalf("GetEmail(%s): %v", id, err)
		}
		if got != nil {
			t.Errorf("expected %s to have been purged, still found: %+v", id, got)
		}
	}

	got, err := svc.GetEmail(recentSent.ID.String())
	if err != nil {
		t.Fatalf("GetEmail(recentSent): %v", err)
	}
	if got == nil {
		t.Error("expected the recent sent email to survive purging")
	}
}

func TestPurgeOldEmails_FailedUsesLongerWindow(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	cfg := RetentionConfig{MaxAge: time.Hour, FailedMaxAge: 48 * time.Hour}
	now := time.Now()

	// Older than MaxAge but younger than FailedMaxAge — must survive, since
	// failed emails get the longer window.
	withinFailedWindow := makeTerminalEmail(t, db, svc, EmailStatusFailed, "gave_up_at", now.Add(-2*time.Hour))
	// Older than FailedMaxAge — must be purged.
	pastFailedWindow := makeTerminalEmail(t, db, svc, EmailStatusFailed, "gave_up_at", now.Add(-72*time.Hour))

	deleted, err := svc.PurgeOldEmails(cfg)
	if err != nil {
		t.Fatalf("PurgeOldEmails: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 row deleted, got %d", deleted)
	}

	got, err := svc.GetEmail(withinFailedWindow.ID.String())
	if err != nil {
		t.Fatalf("GetEmail(withinFailedWindow): %v", err)
	}
	if got == nil {
		t.Error("expected the failed email within FailedMaxAge to survive purging")
	}

	got, err = svc.GetEmail(pastFailedWindow.ID.String())
	if err != nil {
		t.Fatalf("GetEmail(pastFailedWindow): %v", err)
	}
	if got != nil {
		t.Errorf("expected the failed email past FailedMaxAge to have been purged, still found: %+v", got)
	}
}

func TestPurgeOldEmails_NeverTouchesQueuedOrSending(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	// A retention window so short it would delete anything terminal — proving
	// queued/sending survive isn't just because the window was too lenient.
	cfg := RetentionConfig{MaxAge: time.Nanosecond, FailedMaxAge: time.Nanosecond}

	queued, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}

	sending, err := svc.EnqueueTx(db, &SendRequest{To: []string{"a@example.com"}, Subject: "x", BodyText: "x"})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	if err := db.Model(&Email{}).Where("id = ?", sending.ID).Update("status", EmailStatusSending).Error; err != nil {
		t.Fatalf("failed to simulate a live claim: %v", err)
	}

	if _, err := svc.PurgeOldEmails(cfg); err != nil {
		t.Fatalf("PurgeOldEmails: %v", err)
	}

	for _, id := range []string{queued.ID.String(), sending.ID.String()} {
		got, err := svc.GetEmail(id)
		if err != nil {
			t.Fatalf("GetEmail(%s): %v", id, err)
		}
		if got == nil {
			t.Errorf("expected %s to survive purging regardless of retention window", id)
		}
	}
}

// --- ListEmails ---

func TestListEmails_DefaultOrderIsNewestFirst(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	first, err := svc.Send(&SendRequest{To: []string{"a@example.com"}, Subject: "first", BodyText: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // ensure a distinct created_at ordering
	second, err := svc.Send(&SendRequest{To: []string{"b@example.com"}, Subject: "second", BodyText: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	result, err := svc.ListEmails(datatables.Query{})
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if len(result.Items) != 2 || result.Items[0].ID != second.ID || result.Items[1].ID != first.ID {
		t.Errorf("expected newest-first default order, got %+v", result.Items)
	}
}

func TestListEmails_FiltersBySubjectContains(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	if _, err := svc.Send(&SendRequest{To: []string{"a@example.com"}, Subject: "invoice ready", BodyText: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := svc.Send(&SendRequest{To: []string{"b@example.com"}, Subject: "welcome aboard", BodyText: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	result, err := svc.ListEmails(datatables.Query{Filters: map[string]string{"subject": "invoice"}})
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if result.TotalCount != 1 || len(result.Items) != 1 || result.Items[0].Subject != "invoice ready" {
		t.Errorf("expected only the invoice email to match, got count=%d items=%+v", result.TotalCount, result.Items)
	}
}

func TestListEmails_FiltersByStatusEq(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	queued, err := svc.Send(&SendRequest{To: []string{"a@example.com"}, Subject: "a", BodyText: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := svc.Send(&SendRequest{To: []string{"b@example.com"}, Subject: "b", BodyText: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := db.Model(&Email{}).Where("id = ?", queued.ID).Update("status", EmailStatusSent).Error; err != nil {
		t.Fatalf("failed to mark email sent: %v", err)
	}

	result, err := svc.ListEmails(datatables.Query{Filters: map[string]string{"status": string(EmailStatusSent)}})
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if result.TotalCount != 1 || len(result.Items) != 1 || result.Items[0].ID != queued.ID {
		t.Errorf("expected only the sent email to match, got count=%d items=%+v", result.TotalCount, result.Items)
	}
}

func TestListEmails_SortBySubjectAscOverridesDefault(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	if _, err := svc.Send(&SendRequest{To: []string{"a@example.com"}, Subject: "zeta", BodyText: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := svc.Send(&SendRequest{To: []string{"b@example.com"}, Subject: "alpha", BodyText: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	result, err := svc.ListEmails(datatables.Query{SortBy: "subject", SortDir: datatables.Asc})
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if len(result.Items) != 2 || result.Items[0].Subject != "alpha" || result.Items[1].Subject != "zeta" {
		t.Errorf("expected subject asc order, got %+v", result.Items)
	}
}

func TestListEmails_RejectsUnknownFilterColumn(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	_, err := svc.ListEmails(datatables.Query{Filters: map[string]string{"does_not_exist": "x"}})
	if !errors.Is(err, datatables.ErrUnknownFilterColumn) {
		t.Errorf("expected ErrUnknownFilterColumn, got %v", err)
	}
}

func TestListEmails_TotalCountReflectsFiltersNotPagination(t *testing.T) {
	db := testDB(t)
	svc := newTestServiceWithSender(t, db, &fakeSender{})

	for i := 0; i < 5; i++ {
		if _, err := svc.Send(&SendRequest{To: []string{"a@example.com"}, Subject: "invoice ready", BodyText: "hi"}); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	if _, err := svc.Send(&SendRequest{To: []string{"b@example.com"}, Subject: "welcome aboard", BodyText: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	result, err := svc.ListEmails(datatables.Query{
		Filters:  map[string]string{"subject": "invoice"},
		Page:     0,
		PageSize: 2,
	})
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if result.TotalCount != 5 {
		t.Errorf("expected TotalCount 5 (independent of PageSize), got %d", result.TotalCount)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 items on this page, got %d", len(result.Items))
	}
}
