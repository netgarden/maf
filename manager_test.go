package maf

import (
	"strings"
	"testing"
)

// fakeModule is a minimal Module with no other lifecycle hooks.
type fakeModule struct {
	id      string
	manager *Manager
}

func (m *fakeModule) GetID() string               { return m.id }
func (m *fakeModule) GetName() string             { return m.id }
func (m *fakeModule) SetManager(manager *Manager) { m.manager = manager }

// fakeModuleWithDeps additionally declares dependencies.
type fakeModuleWithDeps struct {
	fakeModule
	dependencies []string
}

func (m *fakeModuleWithDeps) GetDependencies() []string { return m.dependencies }

type fakeApplication struct {
	modules []Module
}

func (a *fakeApplication) GetID() string             { return "test-app" }
func (a *fakeApplication) GetName() string           { return "Test App" }
func (a *fakeApplication) GetModules() []Module      { return a.modules }
func (a *fakeApplication) GetConfigFilePath() string { return "" }

func TestCheckDependencies_NoDependencyDeclarations(t *testing.T) {
	m := New(&fakeApplication{modules: []Module{
		&fakeModule{id: "a"},
		&fakeModule{id: "b"},
	}})

	if err := m.Start(false); err != nil {
		t.Fatalf("expected no error for modules with no declared dependencies, got: %v", err)
	}
}

func TestCheckDependencies_SatisfiedDependency(t *testing.T) {
	m := New(&fakeApplication{modules: []Module{
		&fakeModule{id: "database"},
		&fakeModuleWithDeps{
			fakeModule:   fakeModule{id: "auth"},
			dependencies: []string{"database"},
		},
	}})

	if err := m.Start(false); err != nil {
		t.Fatalf("expected no error when the dependency is registered, got: %v", err)
	}
}

func TestCheckDependencies_MissingDependencyFailsStartup(t *testing.T) {
	m := New(&fakeApplication{modules: []Module{
		&fakeModuleWithDeps{
			fakeModule:   fakeModule{id: "auth"},
			dependencies: []string{"database"},
		},
	}})

	err := m.Start(false)
	if err == nil {
		t.Fatal("expected startup to fail when a dependency is not registered")
	}
	if !strings.Contains(err.Error(), "auth") || !strings.Contains(err.Error(), "database") {
		t.Fatalf("expected the error to name both the dependent and missing module, got: %v", err)
	}
}

func TestCheckDependencies_MissingDependency_DoesNotRunLifecycle(t *testing.T) {
	// A module whose dependency is missing must fail before any lifecycle
	// phase runs — not partway through.
	started := false
	m := New(&fakeApplication{modules: []Module{
		&startTrackingModule{
			fakeModuleWithDeps: fakeModuleWithDeps{
				fakeModule:   fakeModule{id: "auth"},
				dependencies: []string{"database"},
			},
			started: &started,
		},
	}})

	if err := m.Start(false); err == nil {
		t.Fatal("expected startup to fail")
	}
	if started {
		t.Fatal("expected Start() to never be called when dependency checking fails first")
	}
}

type startTrackingModule struct {
	fakeModuleWithDeps
	started *bool
}

func (m *startTrackingModule) Start() error {
	*m.started = true
	return nil
}

func TestCheckDependencies_MultipleMissingDependencies_ReportsFirst(t *testing.T) {
	m := New(&fakeApplication{modules: []Module{
		&fakeModuleWithDeps{
			fakeModule:   fakeModule{id: "jobs"},
			dependencies: []string{"locks", "database"},
		},
	}})

	err := m.Start(false)
	if err == nil {
		t.Fatal("expected startup to fail")
	}
	if !strings.Contains(err.Error(), "jobs") || !strings.Contains(err.Error(), "locks") {
		t.Fatalf("expected the error to name the dependent module and the first missing dependency, got: %v", err)
	}
}
