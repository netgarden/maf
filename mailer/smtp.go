package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

// Encryption modes for SMTPConfig.Encryption.
const (
	EncryptionNone     = "none"
	EncryptionSTARTTLS = "starttls"
	EncryptionTLS      = "tls"
)

type SMTPConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string // EncryptionNone | EncryptionSTARTTLS | EncryptionTLS
	From       string
	Timeout    time.Duration
}

func (c SMTPConfig) validate() error {
	switch c.Encryption {
	case EncryptionNone, EncryptionSTARTTLS, EncryptionTLS:
		return nil
	default:
		return fmt.Errorf("mailer: unknown smtp encryption mode %q (expected %q, %q, or %q)", c.Encryption, EncryptionNone, EncryptionSTARTTLS, EncryptionTLS)
	}
}

// Sender delivers a single rendered Message. Service depends on this
// interface rather than *SMTPSender directly so send-logic and backoff
// tests can inject a fake with no network involved.
type Sender interface {
	Send(ctx context.Context, msg *Message) error
}

// NewSMTPSender validates cfg (failing fast on an unknown encryption mode
// at startup rather than at first send) and returns a Sender backed by it.
func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &SMTPSender{cfg: cfg}, nil
}

// SMTPSender drives net/smtp.Client directly rather than using
// net/smtp.SendMail, which can't express implicit TLS (port 465) and gives
// no client handle for building a Cc/Bcc-aware message.
type SMTPSender struct {
	cfg SMTPConfig
}

func (s *SMTPSender) Send(ctx context.Context, msg *Message) error {

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	conn, err := s.dial(addr)
	if err != nil {
		return fmt.Errorf("mailer: dial %s: %w", addr, err)
	}

	deadline := time.Now().Add(s.cfg.Timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return err
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mailer: smtp handshake: %w", err)
	}
	defer client.Close()

	if s.cfg.Encryption == EncryptionSTARTTLS {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return fmt.Errorf("mailer: smtp server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
			return fmt.Errorf("mailer: starttls: %w", err)
		}
	}

	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("mailer: smtp auth: %w", err)
		}
	}

	if err := client.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("mailer: MAIL FROM: %w", err)
	}

	recipients := msg.AllRecipients()
	if len(recipients) == 0 {
		return fmt.Errorf("mailer: message has no recipients")
	}
	for _, addr := range recipients {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("mailer: RCPT TO %s: %w", addr, err)
		}
	}

	raw, err := buildMessage(s.cfg.From, msg)
	if err != nil {
		return fmt.Errorf("mailer: build message: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("mailer: DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		w.Close()
		return fmt.Errorf("mailer: write message body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mailer: close message body: %w", err)
	}

	return client.Quit()
}

func (s *SMTPSender) dial(addr string) (net.Conn, error) {
	if s.cfg.Encryption == EncryptionTLS {
		return tls.Dial("tcp", addr, &tls.Config{ServerName: s.cfg.Host})
	}
	return net.Dial("tcp", addr)
}
