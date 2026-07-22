package maf

import (
	"errors"
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

// initFailingModule always fails Initialize(), simulating e.g. bad config
// or an unreachable dependency for one module partway through startup.
type initFailingModule struct {
	fakeModule
}

func (m *initFailingModule) Initialize() error { return errors.New("boom") }

// stopTrackingModule records whether Stop() was ever called on it.
type stopTrackingModule struct {
	fakeModule
	stopped *bool
}

func (m *stopTrackingModule) Initialize() error { return nil }
func (m *stopTrackingModule) Stop() error {
	*m.stopped = true
	return nil
}

// TestDoStop_SkipsModulesNeverInitialized is the regression test for a real
// crash: if module A's Initialize() fails, later modules in startOrder
// (here, B) never get their own Initialize() called - but Start() still
// unwinds via Stop() on failure. doStop() must not call Stop() on a module
// that was never initialized, since its Stop() may assume state Initialize()
// would have set up (as github.com/netgarden/maf/jobs's Module did, until
// this was fixed - Stop() dereferenced a service field only ever assigned in
// Initialize(), nil-panicking whenever startup failed before jobs.Initialize
// ran).
func TestDoStop_SkipsModulesNeverInitialized(t *testing.T) {
	stopped := false
	m := New(&fakeApplication{modules: []Module{
		&initFailingModule{fakeModule: fakeModule{id: "a"}},
		&stopTrackingModule{fakeModule: fakeModule{id: "b"}, stopped: &stopped},
	}})

	if err := m.Start(false); err == nil {
		t.Fatal("expected startup to fail")
	}
	if stopped {
		t.Fatal("expected Stop() not to be called on a module whose Initialize() never ran")
	}
}

// TestDoStop_StopsModulesInitializedBeforeALaterFailure proves the fix
// isn't overly broad: a module that DID successfully initialize before a
// later phase failed elsewhere must still be stopped.
func TestDoStop_StopsModulesInitializedBeforeALaterFailure(t *testing.T) {
	stopped := false
	m := New(&fakeApplication{modules: []Module{
		&stopTrackingModule{fakeModule: fakeModule{id: "a"}, stopped: &stopped},
		&postInitFailingModule{fakeModule: fakeModule{id: "b"}},
	}})

	if err := m.Start(false); err == nil {
		t.Fatal("expected startup to fail")
	}
	if !stopped {
		t.Fatal("expected Stop() to still be called on a module that completed Initialize()")
	}
}

// postInitFailingModule succeeds Initialize() but fails PostInitialize(),
// so every module's Initialize() completes before startup fails.
type postInitFailingModule struct {
	fakeModule
}

func (m *postInitFailingModule) Initialize() error     { return nil }
func (m *postInitFailingModule) PostInitialize() error { return errors.New("boom") }

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
