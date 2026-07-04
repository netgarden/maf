package mailer

import (
	"bytes"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"sync"
	texttemplate "text/template"

	"gorm.io/gorm"
)

// ErrTemplateNotFound is returned by GetTemplate/ResetTemplate/EnqueueTemplate
// when templateID was never registered by any module and has no override
// either — a typo'd ID or a forgotten module import, not a normal runtime
// condition.
var ErrTemplateNotFound = errors.New("mailer: template not found")

// TemplateDefault is registered in code by the module that owns a given
// template, at that module's own Initialize() — this is what makes the
// template "work out of the box." An administrator can later override its
// content at runtime (see Service.UpdateTemplate); the override, if
// present, always wins.
type TemplateDefault struct {
	// ID is the template's stable identifier. Convention:
	// "<moduleID>.<name>", e.g. "auth.new-user-credentials", to avoid
	// collisions between modules — not enforced by code, just convention.
	ID string

	Subject  string
	BodyText string
	BodyHTML *string

	// Description is free text shown to administrators in the admin UI,
	// e.g. "Variables: Username, TemporaryPassword, LoginURL" — there's no
	// structured variable introspection, just this human-readable hint.
	Description string
}

// resolvedTemplate is a template's effective content — either the
// registered default or an admin override — plus whether it came from an
// override, ready to render.
type resolvedTemplate struct {
	subject      string
	bodyText     string
	bodyHTML     *string
	description  string
	isCustomized bool
}

// templateRegistry holds every module's registered defaults in memory.
// Guarded by a mutex even though registration realistically only happens
// sequentially during each module's own Initialize() — cheap insurance
// against a future concurrent-registration assumption changing.
type templateRegistry struct {
	mu    sync.RWMutex
	items map[string]TemplateDefault
}

func newTemplateRegistry() *templateRegistry {
	return &templateRegistry{items: make(map[string]TemplateDefault)}
}

// RegisterTemplate validates def's content (parsing Subject/BodyText via
// text/template and BodyHTML, if set, via html/template) and adds it to
// the in-memory registry. A parse failure here is a bug in the calling
// module's own hardcoded default — returning an error lets it surface at
// that module's Initialize() rather than at first send.
func (s *Service) RegisterTemplate(def TemplateDefault) error {
	if err := validateTemplateContent(def.Subject, def.BodyText, def.BodyHTML); err != nil {
		return fmt.Errorf("mailer: invalid default template %q: %w", def.ID, err)
	}

	s.templates.mu.Lock()
	defer s.templates.mu.Unlock()
	s.templates.items[def.ID] = def

	return nil
}

// validateTemplateContent parses subject/bodyText/bodyHTML without
// rendering them, so both RegisterTemplate (module-supplied defaults) and
// UpdateTemplate (admin-supplied overrides) reject malformed template
// syntax before it's ever stored, rather than failing later in the
// background send tick.
func validateTemplateContent(subject, bodyText string, bodyHTML *string) error {
	if _, err := texttemplate.New("subject").Parse(subject); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if _, err := texttemplate.New("bodyText").Parse(bodyText); err != nil {
		return fmt.Errorf("bodyText: %w", err)
	}
	if bodyHTML != nil {
		if _, err := htmltemplate.New("bodyHTML").Parse(*bodyHTML); err != nil {
			return fmt.Errorf("bodyHtml: %w", err)
		}
	}
	return nil
}

// resolveTemplate looks up templateID's DB override, falling back to the
// in-memory registered default. Returns ErrTemplateNotFound if neither
// exists.
func (s *Service) resolveTemplate(tx *gorm.DB, templateID string) (resolvedTemplate, error) {

	var override Template
	err := tx.Where("template_id = ?", templateID).First(&override).Error
	if err == nil {
		return resolvedTemplate{
			subject:      override.Subject,
			bodyText:     override.BodyText,
			bodyHTML:     override.BodyHTML,
			description:  s.templateDescription(templateID),
			isCustomized: true,
		}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return resolvedTemplate{}, err
	}

	s.templates.mu.RLock()
	def, ok := s.templates.items[templateID]
	s.templates.mu.RUnlock()
	if !ok {
		return resolvedTemplate{}, ErrTemplateNotFound
	}

	return resolvedTemplate{
		subject:      def.Subject,
		bodyText:     def.BodyText,
		bodyHTML:     def.BodyHTML,
		description:  def.Description,
		isCustomized: false,
	}, nil
}

func (s *Service) templateDescription(templateID string) string {
	s.templates.mu.RLock()
	defer s.templates.mu.RUnlock()
	return s.templates.items[templateID].Description
}

// renderTemplate substitutes data into tmpl. BodyHTML is rendered via
// html/template, which auto-escapes data's fields — this matters because
// data can contain admin- or user-supplied strings (e.g. a username) being
// interpolated into an HTML email body.
func renderTemplate(tmpl resolvedTemplate, data any) (subject, bodyText string, bodyHTML *string, err error) {

	subject, err = renderText(tmpl.subject, data)
	if err != nil {
		return "", "", nil, fmt.Errorf("mailer: render subject: %w", err)
	}

	bodyText, err = renderText(tmpl.bodyText, data)
	if err != nil {
		return "", "", nil, fmt.Errorf("mailer: render bodyText: %w", err)
	}

	if tmpl.bodyHTML == nil {
		return subject, bodyText, nil, nil
	}

	rendered, err := renderHTML(*tmpl.bodyHTML, data)
	if err != nil {
		return "", "", nil, fmt.Errorf("mailer: render bodyHtml: %w", err)
	}

	return subject, bodyText, &rendered, nil
}

func renderText(tmplStr string, data any) (string, error) {
	t, err := texttemplate.New("t").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderHTML(tmplStr string, data any) (string, error) {
	t, err := htmltemplate.New("t").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
