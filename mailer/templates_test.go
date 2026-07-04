package mailer

import (
	"strings"
	"testing"
)

func newTestService() *Service {
	return &Service{templates: newTemplateRegistry()}
}

func TestRegisterTemplate_AcceptsValidDefault(t *testing.T) {
	s := newTestService()

	err := s.RegisterTemplate(TemplateDefault{
		ID:       "test.welcome",
		Subject:  "Welcome {{.Name}}",
		BodyText: "Hello {{.Name}}, your code is {{.Code}}.",
	})
	if err != nil {
		t.Fatalf("expected valid template to register cleanly, got: %v", err)
	}
}

func TestRegisterTemplate_RejectsInvalidSubject(t *testing.T) {
	s := newTestService()

	err := s.RegisterTemplate(TemplateDefault{
		ID:       "test.bad-subject",
		Subject:  "Welcome {{.Name",
		BodyText: "fine",
	})
	if err == nil {
		t.Fatal("expected an unbalanced {{ in Subject to be rejected")
	}
}

func TestRegisterTemplate_RejectsInvalidBodyText(t *testing.T) {
	s := newTestService()

	err := s.RegisterTemplate(TemplateDefault{
		ID:       "test.bad-body",
		Subject:  "fine",
		BodyText: "Hello {{range}}", // range with no closing {{end}}
	})
	if err == nil {
		t.Fatal("expected an unclosed {{range}} in BodyText to be rejected")
	}
}

func TestRegisterTemplate_RejectsInvalidBodyHTML(t *testing.T) {
	s := newTestService()

	badHTML := "<p>{{.Name}</p>" // missing closing brace
	err := s.RegisterTemplate(TemplateDefault{
		ID:       "test.bad-html",
		Subject:  "fine",
		BodyText: "fine",
		BodyHTML: &badHTML,
	})
	if err == nil {
		t.Fatal("expected malformed BodyHTML template syntax to be rejected")
	}
}

func TestRenderTemplate_SubstitutesTextAndHTML(t *testing.T) {
	html := "<p>Hello {{.Name}}, code: {{.Code}}</p>"
	tmpl := resolvedTemplate{
		subject:  "Welcome {{.Name}}",
		bodyText: "Hello {{.Name}}, your code is {{.Code}}.",
		bodyHTML: &html,
	}

	subject, bodyText, bodyHTML, err := renderTemplate(tmpl, map[string]any{
		"Name": "Alice",
		"Code": "123456",
	})
	if err != nil {
		t.Fatalf("renderTemplate: %v", err)
	}

	if subject != "Welcome Alice" {
		t.Errorf("subject: got %q", subject)
	}
	if bodyText != "Hello Alice, your code is 123456." {
		t.Errorf("bodyText: got %q", bodyText)
	}
	if bodyHTML == nil || *bodyHTML != "<p>Hello Alice, code: 123456</p>" {
		t.Errorf("bodyHTML: got %v", bodyHTML)
	}
}

func TestRenderTemplate_NilBodyHTMLStaysNil(t *testing.T) {
	tmpl := resolvedTemplate{
		subject:  "Hi {{.Name}}",
		bodyText: "Hi {{.Name}}",
	}

	_, _, bodyHTML, err := renderTemplate(tmpl, map[string]any{"Name": "Alice"})
	if err != nil {
		t.Fatalf("renderTemplate: %v", err)
	}
	if bodyHTML != nil {
		t.Errorf("expected nil bodyHTML when the template has none, got %q", *bodyHTML)
	}
}

func TestRenderTemplate_HTMLEscapesData(t *testing.T) {
	html := "<p>Hello {{.Name}}</p>"
	tmpl := resolvedTemplate{
		subject:  "Hi",
		bodyText: "Hi",
		bodyHTML: &html,
	}

	// A malicious/unexpected value in data must not be able to inject markup
	// into the rendered HTML email.
	_, _, bodyHTML, err := renderTemplate(tmpl, map[string]any{
		"Name": "<script>alert(1)</script>",
	})
	if err != nil {
		t.Fatalf("renderTemplate: %v", err)
	}
	if bodyHTML == nil {
		t.Fatal("expected non-nil bodyHTML")
	}
	if strings.Contains(*bodyHTML, "<script>") {
		t.Fatalf("expected html/template to escape the injected markup, got: %s", *bodyHTML)
	}
}

func TestRenderTemplate_TextDoesNotEscape(t *testing.T) {
	// Plain-text bodies use text/template, which does not (and should not)
	// HTML-escape — a literal "<b>" typed by a user should show up as-is in
	// a plain-text email.
	tmpl := resolvedTemplate{
		subject:  "Hi",
		bodyText: "Value: {{.Value}}",
	}

	_, bodyText, _, err := renderTemplate(tmpl, map[string]any{"Value": "<b>bold</b>"})
	if err != nil {
		t.Fatalf("renderTemplate: %v", err)
	}
	if bodyText != "Value: <b>bold</b>" {
		t.Errorf("expected plain text template to leave markup unescaped, got: %q", bodyText)
	}
}
