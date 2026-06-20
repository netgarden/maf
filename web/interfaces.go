package web

import (
	"github.com/flosch/pongo2/v6"
	"io/fs"
	"github.com/netgarden/maf"
)

type Controller interface {
	Register(app *Group)
}

type WebModule interface {
	maf.Module
	GetWebControllers() []Controller
}

type WebModuleStaticFSProvider interface {
	WebModule
	GetWebStaticFS() fs.FS
}

type WebModuleTemplatesFSProvider interface {
	WebModule
	GetWebTemplatesFS() fs.FS
}

type WebModuleTemplateTagsProvider interface {
	WebModule
	GetWebTemplateTags(renderer *Renderer) map[string]pongo2.TagParser
}

type WebModuleTemplateFiltersProvider interface {
	WebModule
	GetWebTemplateFilters() map[string]pongo2.FilterFunction
}
