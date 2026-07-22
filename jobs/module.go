// Package jobs schedules recurring work — each job runs on its own
// interval, coordinated across instances sharing a database via
// github.com/netgarden/maf/locks so only one instance runs a given job at a
// time. See README.md for the execution and timeout model.
package jobs

import (
	"errors"

	"gorm.io/gorm"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/locks"
)

func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager *maf.Manager
	db      *gorm.DB
	service *Service
}

func (m *Module) GetID() string {
	return "jobs"
}

func (m *Module) GetName() string {
	return "Jobs"
}

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

func (m *Module) GetDependencies() []string {
	return []string{"locks"}
}

func (m *Module) SetDB(db *gorm.DB) {
	m.db = db
}

func (m *Module) GetDBEntities() []interface{} {
	return []interface{}{
		&Job{},
	}
}

func (m *Module) Initialize() error {
	locksModule, ok := m.manager.GetModule("locks").(*locks.Module)
	if !ok {
		return errors.New("locks module not found or wrong type — register locks before jobs")
	}

	m.service = NewService(m.db, locksModule.GetService())

	return nil
}

func (m *Module) Stop() error {
	if m.service != nil {
		m.service.Stop()
	}
	return nil
}

// GetService returns the jobs service, available after Initialize.
func (m *Module) GetService() *Service {
	return m.service
}
