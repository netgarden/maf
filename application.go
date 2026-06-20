package maf

type Application interface {
	GetID() string
	GetName() string
	GetModules() []Module
	GetConfigFilePath() string
}
