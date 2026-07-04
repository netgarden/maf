package maf

type Module interface {
	GetID() string
	GetName() string
	SetManager(manager *Manager)
}

// ModuleDependenciesProvider lets a module declare which other module IDs
// must be registered in the same Application for it to work. The Manager
// checks this for every module right after registration, before config
// loading or any lifecycle phase runs, so a missing dependency fails
// startup immediately with a clear error instead of surfacing later as a
// nil-pointer panic or type assertion failure inside Initialize.
type ModuleDependenciesProvider interface {
	Module
	GetDependencies() []string
}

type ModuleConfigSchemaProvider interface {
	Module
	GetConfigSchema() []ConfigItem
}

type ModuleConfigConsumer interface {
	Module
	SetConfig(config *Config)
}

type ModuleLoggingProvider interface {
	Module
	SetupLogging() error
}

type ModulePreInitialize interface {
	Module
	PreInitialize() error
}

type ModuleInitialize interface {
	Module
	Initialize() error
}

type ModulePostInitialize interface {
	Module
	PostInitialize() error
}

type ModulePreStart interface {
	Module
	PreStart() error
}

type ModuleStart interface {
	Module
	Start() error
}

type ModuleStop interface {
	Module
	Stop() error
}
