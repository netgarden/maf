// Package locks provides distributed lease locks backed by a Postgres table
// and pg_advisory_xact_lock, for coordinating work across multiple instances
// of an application that share a database. See README.md for the concurrency
// model, usage examples, and testing instructions.
package locks

import (
	"gorm.io/gorm"

	"github.com/netgarden/maf"
)

// NewModule constructs the maf module wiring locks into an application's
// database and module lifecycle. Use NewService directly instead if you
// don't need it wired through maf.
func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager *maf.Manager
	db      *gorm.DB
	service *Service
}

func (m *Module) GetID() string {
	return "locks"
}

func (m *Module) GetName() string {
	return "Locks"
}

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

func (m *Module) SetDB(db *gorm.DB) {
	m.db = db
}

func (m *Module) GetDBEntities() []interface{} {
	return []interface{}{
		&Lock{},
	}
}

func (m *Module) Initialize() error {
	m.service = NewService(m.db)
	return nil
}

// GetService returns the locks service, available after Initialize.
func (m *Module) GetService() *Service {
	return m.service
}
