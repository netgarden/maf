package web

import (
	"github.com/flosch/pongo2/v6"
	"github.com/netgarden/maf"
	"github.com/netgarden/maf/config"
	"github.com/netgarden/maf/mergefs"
	"io/fs"
)

func NewModule(moduleConfig *ModuleConfig) *Module {
	return &Module{
		moduleConfig: moduleConfig,
	}
}

type Module struct {
	moduleConfig *ModuleConfig
	manager      *maf.Manager
	config       *Config
	server       *Server
}

func (m *Module) GetID() string {
	return "web"
}

func (m *Module) GetName() string {
	return "Web"
}

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

func (m *Module) GetConfigSchema() []config.Item {
	return []config.Item{
		{Key: "web.port", Type: config.Int, Default: 8080},
		{Key: "web.baseUrl", Type: config.String},
		{Key: "web.reloadTemplates", Type: config.Bool},
	}
}

func (m *Module) GetConfig() *Config {
	return m.config
}

func (m *Module) Initialize() error {
	return nil
}

func (m *Module) PreStart() error {

	cfg := m.manager.GetConfig().Sub("web")
	m.config = &Config{
		Port:            uint16(cfg.GetInt("port")),
		BaseUrl:         cfg.GetString("baseUrl"),
		ReloadTemplates: cfg.GetBool("reloadTemplates"),
	}

	m.server = NewServer(m)

	for _, module := range m.manager.GetModulesList() {

		webModule, ok := module.(WebModule)
		if !ok {
			continue
		}

		if tagsProvider, ok := module.(WebModuleTemplateTagsProvider); ok {
			for name, tag := range tagsProvider.GetWebTemplateTags(m.server.GetRenderer()) {
				err := pongo2.RegisterTag(name, tag)
				if err != nil {
					return err
				}
			}
		}

		if filtersProvider, ok := module.(WebModuleTemplateFiltersProvider); ok {
			for name, filter := range filtersProvider.GetWebTemplateFilters() {
				err := pongo2.RegisterFilter(name, filter)
				if err != nil {
					return err
				}
			}
		}

		routerGroup := m.server.GetGroup()
		controllers := webModule.GetWebControllers()
		for _, controller := range controllers {
			controller.Register(routerGroup)
		}

	}

	m.server.PreStart()

	return nil
}

func (m *Module) Start() error {
	return m.server.Start()
}

func (m *Module) Stop() error {
	if m.server == nil {
		return nil
	}
	return m.server.Stop()
}

func (m *Module) getStaticFS() fs.FS {

	fsList := make([]fs.FS, 0)

	for _, module := range m.manager.GetModulesList() {

		webModule, ok := module.(WebModuleStaticFSProvider)
		if !ok {
			continue
		}

		tplFS := webModule.GetWebStaticFS()
		if tplFS == nil {
			continue
		}

		fsList = append(fsList, tplFS)
	}

	if len(fsList) == 0 {
		return nil
	}

	return mergefs.Merge(fsList...)
}

func (m *Module) getTemplatesFS() fs.FS {

	fsList := make([]fs.FS, 0)

	for _, module := range m.manager.GetModulesList() {

		webModule, ok := module.(WebModuleTemplatesFSProvider)
		if !ok {
			continue
		}

		tplFS := webModule.GetWebTemplatesFS()
		if tplFS == nil {
			continue
		}

		fsList = append(fsList, tplFS)
	}

	if len(fsList) == 0 {
		return nil
	}

	return mergefs.Merge(fsList...)
}
