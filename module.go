package maf

type Module interface {
	GetID() string
	GetName() string
	SetManager(manager *Manager)
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
