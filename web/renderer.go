package web

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
	"io"
	"io/fs"
)

type Renderer struct {
	fs        fs.FS
	templates *pongo2.TemplateSet
}

// NewRenderer creates a new Renderer struct.
func NewRenderer(fs fs.FS) *Renderer {

	r := &Renderer{
		fs: fs,
	}

	templates := pongo2.NewSet("templates", r)
	r.templates = templates

	return r
}

// Abs returns absolute path to file requested.
// Search path is configured in AddDirectory method.
// And default directory is "./templates".
func (p *Renderer) Abs(base, name string) string {
	return name
}

// Get reads the path's content from your local filesystem.
func (p *Renderer) Get(path string) (io.Reader, error) {

	f, err := p.fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(data), nil
}

// RegisterTag registers a custom tag.
// It calls pongo2.RegisterTag method.
func (p *Renderer) RegisterTag(name string, parserFunc pongo2.TagParser) {
	pongo2.RegisterTag(name, parserFunc)
}

// RegisterFilter registers a custom filter.
// It calls pongo2.RegisterFilter method.
func (p *Renderer) RegisterFilter(name string, fn pongo2.FilterFunction) {
	pongo2.RegisterFilter(name, fn)
}

// SetDebug sets debug mode to the template set.
// See pongo2.TemplateSet.Debug for more information.
func (p *Renderer) SetDebug(v bool) {
	p.templates.Debug = v
}

// Render renders the view.
// Many other times, this is called in your echo handler functions.
func (p *Renderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	tmpl, err := p.templates.FromCache(name)
	if err != nil {
		return err
	}
	if tmpl == nil {
		return fmt.Errorf("template '%s' not found", name)
	}
	d, ok := data.(map[string]interface{})
	if !ok {
		return errors.New("Incorrect data format. Should be map[string]interface{}")
	}

	return tmpl.ExecuteWriter(d, w)
}
