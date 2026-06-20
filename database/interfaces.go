package database

import (
	"gorm.io/gorm"
	"github.com/netgarden/maf"
)

type Consumer interface {
	maf.Module
	SetDB(db *gorm.DB)
}

type EntitiesProvider interface {
	maf.Module
	GetDBEntities() []interface{}
}

type PreMigrationConsumer interface {
	Consumer
	DBPreMigration(db *gorm.DB) error
}
