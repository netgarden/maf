package mailer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// requireMailpit skips the test if TestMain failed to start the Mailpit
// container (e.g. Docker unavailable) — same ergonomics as testDB's skip.
func requireMailpit(t *testing.T) {
	t.Helper()
	if testMailpitErr != nil {
		t.Skipf("mailpit testcontainer unavailable (Docker required): %v", testMailpitErr)
	}
}

// testSMTPSender returns an SMTPSender pointed at the Mailpit container
// TestMain started, and registers a cleanup that clears Mailpit's mailbox
// — Mailpit is shared by every test in this package's run, so clearing it
// per test is what gives them isolation from each other (mirroring
// testDB's row-reset for Postgres).
func testSMTPSender(t *testing.T) *SMTPSender {
	t.Helper()
	requireMailpit(t)

	sender, err := NewSMTPSender(SMTPConfig{
		Host:       testSMTPHost,
		Port:       testSMTPPort,
		Encryption: EncryptionNone,
		From:       "sender@example.com",
		Timeout:    10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}

	t.Cleanup(func() { mailpitClear(t) })

	return sender
}

type mailpitAddress struct {
	Name    string
	Address string
}

type mailpitListItem struct {
	ID      string
	To      []mailpitAddress
	Cc      []mailpitAddress
	Bcc     []mailpitAddress
	Subject string
}

type mailpitListResponse struct {
	Total    int
	Messages []mailpitListItem
}

type mailpitMessageDetail struct {
	To      []mailpitAddress
	Cc      []mailpitAddress
	Bcc     []mailpitAddress
	Subject string
	Text    string
	HTML    string
}

func mailpitGet(t *testing.T, path string, out any) {
	t.Helper()
	resp, err := http.Get(testMailpitAPIBase + path)
	if err != nil {
		t.Fatalf("mailpit GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mailpit GET %s: unexpected status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("mailpit GET %s: decode: %v", path, err)
	}
}

// mailpitClear deletes every message currently in Mailpit's mailbox.
func mailpitClear(t *testing.T) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, testMailpitAPIBase+"/api/v1/messages", nil)
	if err != nil {
		t.Fatalf("mailpit DELETE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mailpit DELETE: %v", err)
	}
	defer resp.Body.Close()
}

// mailpitOnlyMessage asserts Mailpit received exactly one message and
// returns its full detail (To/Cc/Bcc/Subject/Text/HTML).
func mailpitOnlyMessage(t *testing.T) mailpitMessageDetail {
	t.Helper()

	var list mailpitListResponse
	mailpitGet(t, "/api/v1/messages", &list)
	if list.Total != 1 {
		t.Fatalf("expected exactly 1 message in mailpit, got %d", list.Total)
	}

	var detail mailpitMessageDetail
	mailpitGet(t, fmt.Sprintf("/api/v1/message/%s", list.Messages[0].ID), &detail)
	return detail
}

func addressesContain(addrs []mailpitAddress, address string) bool {
	for _, a := range addrs {
		if a.Address == address {
			return true
		}
	}
	return false
}

// TestSMTPSender_Send_DeliversWithCorrectContent is the actual end-to-end
// proof that SMTPSender speaks real SMTP correctly — every other test in
// this package exercises Service against the Sender interface via
// fakeSender, never SMTPSender's own dial/MAIL FROM/RCPT TO/DATA sequence
// against a real server.
func TestSMTPSender_Send_DeliversWithCorrectContent(t *testing.T) {
	sender := testSMTPSender(t)

	html := "<p>hello</p>"
	err := sender.Send(context.Background(), &Message{
		To:       []string{"to@example.com"},
		Cc:       []string{"cc@example.com"},
		Bcc:      []string{"bcc@example.com"},
		Subject:  "Integration test",
		BodyText: "hello world",
		BodyHTML: &html,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	msg := mailpitOnlyMessage(t)

	if msg.Subject != "Integration test" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Integration test")
	}
	if !addressesContain(msg.To, "to@example.com") {
		t.Errorf("To = %v, expected to contain to@example.com", msg.To)
	}
	if !addressesContain(msg.Cc, "cc@example.com") {
		t.Errorf("Cc = %v, expected to contain cc@example.com", msg.Cc)
	}
	// Bcc must reach the server (proven by it showing up at all — Mailpit
	// only classifies a recipient as Bcc when it was never in a header,
	// exactly mime.go's own design) despite never appearing in any header.
	if !addressesContain(msg.Bcc, "bcc@example.com") {
		t.Errorf("Bcc = %v, expected to contain bcc@example.com", msg.Bcc)
	}
	if msg.Text != "hello world" {
		t.Errorf("Text = %q, want %q", msg.Text, "hello world")
	}
	if msg.HTML != html {
		t.Errorf("HTML = %q, want %q", msg.HTML, html)
	}
}

// TestSMTPSender_Send_TextOnlyOmitsHTMLPart proves a nil BodyHTML really
// results in no HTML part being sent at all, not an empty one.
func TestSMTPSender_Send_TextOnlyOmitsHTMLPart(t *testing.T) {
	sender := testSMTPSender(t)

	err := sender.Send(context.Background(), &Message{
		To:       []string{"to@example.com"},
		Subject:  "Text only",
		BodyText: "just text",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	msg := mailpitOnlyMessage(t)

	// TrimRight: a single-part (non-multipart) body's raw bytes end exactly
	// where the message ends, so a trailing CRLF from the DATA transport
	// itself is preserved by Mailpit's MIME parsing — insignificant for
	// plain text and not something buildMessage adds itself (see
	// TestBuildMessage_TextOnly_NoMultipart for the exact-bytes proof of
	// what buildMessage produces).
	if got := strings.TrimRight(msg.Text, "\r\n"); got != "just text" {
		t.Errorf("Text = %q, want %q", got, "just text")
	}
	if msg.HTML != "" {
		t.Errorf("expected no HTML part, got %q", msg.HTML)
	}
}
