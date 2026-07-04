package maf

import (
	"strings"
	"testing"
)

// recordingModule appends its ID to a shared log on Initialize and Stop, so
// tests can observe the actual order the Manager drives modules through
// their lifecycle, not just whether it succeeds.
type recordingModule struct {
	fakeModuleWithDeps
	initLog *[]string
	stopLog *[]string
}

func (m *recordingModule) Initialize() error {
	*m.initLog = append(*m.initLog, m.GetID())
	return nil
}

func (m *recordingModule) Stop() error {
	*m.stopLog = append(*m.stopLog, m.GetID())
	return nil
}

func newRecordingModule(id string, deps []string, initLog, stopLog *[]string) *recordingModule {
	return &recordingModule{
		fakeModuleWithDeps: fakeModuleWithDeps{
			fakeModule:   fakeModule{id: id},
			dependencies: deps,
		},
		initLog: initLog,
		stopLog: stopLog,
	}
}

func TestResolveStartOrder_ReordersOutOfOrderRegistration(t *testing.T) {
	var initLog, stopLog []string

	// "auth" is registered before the "security" module it depends on —
	// the Manager must still initialize security first.
	m := New(&fakeApplication{modules: []Module{
		newRecordingModule("auth", []string{"security"}, &initLog, &stopLog),
		newRecordingModule("security", nil, &initLog, &stopLog),
	}})

	if err := m.Start(false); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if got, want := strings.Join(initLog, ","), "security,auth"; got != want {
		t.Fatalf("expected init order %q, got %q", want, got)
	}
}

func TestResolveStartOrder_StableForUnrelatedModules(t *testing.T) {
	var initLog, stopLog []string

	m := New(&fakeApplication{modules: []Module{
		newRecordingModule("a", nil, &initLog, &stopLog),
		newRecordingModule("b", nil, &initLog, &stopLog),
		newRecordingModule("c", nil, &initLog, &stopLog),
	}})

	if err := m.Start(false); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if got, want := strings.Join(initLog, ","), "a,b,c"; got != want {
		t.Fatalf("expected registration order preserved (%q) when there's no dependency constraint, got %q", want, got)
	}
}

func TestResolveStartOrder_TransitiveChainRegisteredOutOfOrder(t *testing.T) {
	var initLog, stopLog []string

	// a depends on b, b depends on c; registered scrambled as [a, c, b].
	m := New(&fakeApplication{modules: []Module{
		newRecordingModule("a", []string{"b"}, &initLog, &stopLog),
		newRecordingModule("c", nil, &initLog, &stopLog),
		newRecordingModule("b", []string{"c"}, &initLog, &stopLog),
	}})

	if err := m.Start(false); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if got, want := strings.Join(initLog, ","), "c,b,a"; got != want {
		t.Fatalf("expected dependency order c,b,a regardless of registration order, got %q", got)
	}
}

func TestStop_ReversesStartOrder(t *testing.T) {
	var initLog, stopLog []string

	m := New(&fakeApplication{modules: []Module{
		newRecordingModule("security", nil, &initLog, &stopLog),
		newRecordingModule("auth", []string{"security"}, &initLog, &stopLog),
	}})

	if err := m.Start(false); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	m.Stop()

	if got, want := strings.Join(initLog, ","), "security,auth"; got != want {
		t.Fatalf("expected init order %q, got %q", want, got)
	}
	if got, want := strings.Join(stopLog, ","), "auth,security"; got != want {
		t.Fatalf("expected stop order %q (reverse of init order), got %q", want, got)
	}
}

func TestResolveStartOrder_DirectCycle(t *testing.T) {
	var initLog, stopLog []string

	m := New(&fakeApplication{modules: []Module{
		newRecordingModule("a", []string{"b"}, &initLog, &stopLog),
		newRecordingModule("b", []string{"a"}, &initLog, &stopLog),
	}})

	err := m.Start(false)
	if err == nil {
		t.Fatal("expected Start to fail for a circular dependency")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Fatalf("expected error to mention a circular dependency, got: %v", err)
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("expected error to name both modules in the cycle, got: %v", err)
	}
	if len(initLog) != 0 {
		t.Fatalf("expected no module to be initialized when a cycle is detected, got: %v", initLog)
	}
}

func TestResolveStartOrder_TransitiveCycle(t *testing.T) {
	var initLog, stopLog []string

	// a -> b -> c -> a
	m := New(&fakeApplication{modules: []Module{
		newRecordingModule("a", []string{"b"}, &initLog, &stopLog),
		newRecordingModule("b", []string{"c"}, &initLog, &stopLog),
		newRecordingModule("c", []string{"a"}, &initLog, &stopLog),
	}})

	err := m.Start(false)
	if err == nil {
		t.Fatal("expected Start to fail for a circular dependency")
	}
	for _, id := range []string{"a", "b", "c"} {
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("expected error to name module %q, got: %v", id, err)
		}
	}
}

func TestResolveStartOrder_SelfDependency(t *testing.T) {
	var initLog, stopLog []string

	m := New(&fakeApplication{modules: []Module{
		newRecordingModule("a", []string{"a"}, &initLog, &stopLog),
	}})

	err := m.Start(false)
	if err == nil {
		t.Fatal("expected Start to fail for a module depending on itself")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Fatalf("expected error to mention a circular dependency, got: %v", err)
	}
}

func TestResolveStartOrder_CycleAlongsideUnrelatedModules(t *testing.T) {
	var initLog, stopLog []string

	// "standalone" has no relation to the a<->b cycle and must not mask it.
	m := New(&fakeApplication{modules: []Module{
		newRecordingModule("standalone", nil, &initLog, &stopLog),
		newRecordingModule("a", []string{"b"}, &initLog, &stopLog),
		newRecordingModule("b", []string{"a"}, &initLog, &stopLog),
	}})

	err := m.Start(false)
	if err == nil {
		t.Fatal("expected Start to fail for a circular dependency")
	}
	if strings.Contains(err.Error(), "standalone") {
		t.Fatalf("expected the reported cycle not to include an unrelated module, got: %v", err)
	}
}
