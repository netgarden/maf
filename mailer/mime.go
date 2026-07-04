package mailer

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"
)

// Message is the fully-rendered content of an email, ready to send. From
// is not part of Message: every email sent through a given mailer instance
// uses the same configured SMTP account's From address, supplied
// separately by whatever calls buildMessage.
type Message struct {
	To  []string
	Cc  []string
	Bcc []string

	Subject  string
	BodyText string
	BodyHTML *string
}

// AllRecipients returns every address that should receive the message via
// SMTP RCPT TO — To, Cc, and Bcc combined. This is the only place Bcc
// addresses appear anywhere in the delivery path: buildMessage never puts
// them in a header, which is the entire mechanism that makes Bcc blind.
func (m *Message) AllRecipients() []string {
	all := make([]string, 0, len(m.To)+len(m.Cc)+len(m.Bcc))
	all = append(all, m.To...)
	all = append(all, m.Cc...)
	all = append(all, m.Bcc...)
	return all
}

// buildMessage renders m into a full RFC 5322 message (headers + body),
// ready to be streamed to an SMTP DATA command. Bcc is deliberately never
// written to any header here.
func buildMessage(from string, m *Message) ([]byte, error) {

	var buf bytes.Buffer

	writeHeader(&buf, "From", from)
	if len(m.To) > 0 {
		writeHeader(&buf, "To", strings.Join(m.To, ", "))
	}
	if len(m.Cc) > 0 {
		writeHeader(&buf, "Cc", strings.Join(m.Cc, ", "))
	}
	writeHeader(&buf, "Subject", m.Subject)
	writeHeader(&buf, "MIME-Version", "1.0")
	writeHeader(&buf, "Date", time.Now().Format(time.RFC1123Z))

	if m.BodyHTML == nil {
		writeHeader(&buf, "Content-Type", "text/plain; charset=utf-8")
		buf.WriteString("\r\n")
		buf.WriteString(m.BodyText)
		return buf.Bytes(), nil
	}

	mw := multipart.NewWriter(&buf)
	writeHeader(&buf, "Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", mw.Boundary()))
	buf.WriteString("\r\n")

	textPart, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}})
	if err != nil {
		return nil, err
	}
	if _, err := textPart.Write([]byte(m.BodyText)); err != nil {
		return nil, err
	}

	htmlPart, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/html; charset=utf-8"}})
	if err != nil {
		return nil, err
	}
	if _, err := htmlPart.Write([]byte(*m.BodyHTML)); err != nil {
		return nil, err
	}

	if err := mw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func writeHeader(buf *bytes.Buffer, name, value string) {
	fmt.Fprintf(buf, "%s: %s\r\n", name, sanitizeHeaderValue(value))
}

// sanitizeHeaderValue strips CR/LF from a value about to be placed in a
// single header line, preventing header injection if e.g. a rendered
// Subject template (or a caller bug) contains a newline.
func sanitizeHeaderValue(v string) string {
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	return v
}
