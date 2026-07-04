package mailer

import (
	"net/mail"
	"strings"
	"testing"
)

func TestBuildMessage_PlainText_Headers(t *testing.T) {
	msg := &Message{
		To:       []string{"alice@example.com"},
		Cc:       []string{"bob@example.com"},
		Bcc:      []string{"secret@example.com"},
		Subject:  "Hello",
		BodyText: "hi there",
	}

	raw, err := buildMessage("sender@example.com", msg)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("failed to parse built message: %v", err)
	}

	if got := parsed.Header.Get("From"); got != "sender@example.com" {
		t.Errorf("From: got %q", got)
	}
	if got := parsed.Header.Get("To"); got != "alice@example.com" {
		t.Errorf("To: got %q", got)
	}
	if got := parsed.Header.Get("Cc"); got != "bob@example.com" {
		t.Errorf("Cc: got %q", got)
	}
	if got := parsed.Header.Get("Subject"); got != "Hello" {
		t.Errorf("Subject: got %q", got)
	}
}

func TestBuildMessage_NeverIncludesBccHeader(t *testing.T) {
	msg := &Message{
		To:       []string{"alice@example.com"},
		Bcc:      []string{"secret@example.com"},
		Subject:  "Hello",
		BodyText: "hi there",
	}

	raw, err := buildMessage("sender@example.com", msg)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	rawStr := string(raw)
	if strings.Contains(strings.ToLower(rawStr), "bcc") {
		t.Fatalf("built message must never contain the word \"bcc\" anywhere, got:\n%s", rawStr)
	}
}

func TestBuildMessage_TextOnly_NoMultipart(t *testing.T) {
	msg := &Message{
		To:       []string{"alice@example.com"},
		Subject:  "Hello",
		BodyText: "hi there",
	}

	raw, err := buildMessage("sender@example.com", msg)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	rawStr := string(raw)
	if strings.Contains(rawStr, "multipart") {
		t.Errorf("text-only message should not be multipart, got:\n%s", rawStr)
	}
	if !strings.Contains(rawStr, "hi there") {
		t.Error("expected body text to be present")
	}
}

func TestBuildMessage_HTMLBody_ProducesMultipartAlternative(t *testing.T) {
	html := "<p>hi there</p>"
	msg := &Message{
		To:       []string{"alice@example.com"},
		Subject:  "Hello",
		BodyText: "hi there",
		BodyHTML: &html,
	}

	raw, err := buildMessage("sender@example.com", msg)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("failed to parse built message: %v", err)
	}

	contentType := parsed.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/alternative") {
		t.Fatalf("expected multipart/alternative Content-Type, got %q", contentType)
	}

	rawStr := string(raw)
	if !strings.Contains(rawStr, "hi there") {
		t.Error("expected plain text part to be present")
	}
	if !strings.Contains(rawStr, html) {
		t.Error("expected HTML part to be present")
	}
	if !strings.Contains(rawStr, "text/plain") || !strings.Contains(rawStr, "text/html") {
		t.Error("expected both text/plain and text/html part Content-Types to be present")
	}
}

func TestBuildMessage_SanitizesHeaderInjection(t *testing.T) {
	msg := &Message{
		To:       []string{"alice@example.com"},
		Subject:  "Hello\r\nBcc: attacker@example.com",
		BodyText: "hi there",
	}

	raw, err := buildMessage("sender@example.com", msg)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("failed to parse built message: %v", err)
	}

	// The real injection-prevention goal: stripping the CR/LF must not let a
	// crafted Subject value create a genuinely separate header field. It's
	// fine (expected, even) for the mangled text to still appear within the
	// single Subject header's own value.
	if got := parsed.Header.Get("Bcc"); got != "" {
		t.Fatalf("newline in Subject injected a separate Bcc header: %q", got)
	}
}
