package jobs

import (
	"sync"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/netgarden/maf/database"
)

// mockRunner lets tests control exactly when RunJob returns, so scheduling
// and concurrency behavior can be tested deterministically without a
// database or real time.Sleep-based races.
type mockRunner struct {
	mu    sync.Mutex
	calls []*Job

	block   bool
	entered chan struct{}
	proceed chan struct{}

	state JobState
	job   *Job
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		entered: make(chan struct{}, 10),
		proceed: make(chan struct{}),
		state:   JobStateOK,
	}
}

func (r *mockRunner) RunJob(job *Job) (JobState, *Job) {
	r.mu.Lock()
	r.calls = append(r.calls, job)
	block := r.block
	r.mu.Unlock()

	if block {
		r.entered <- struct{}{}
		<-r.proceed
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.job != nil {
		return r.state, r.job
	}
	return r.state, job
}

func (r *mockRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func newTestJob(interval int64) *Job {
	return &Job{
		EntityBase: database.EntityBase{ID: uuid.NewV4()},
		HandlerID:  "test-handler",
		Interval:   interval,
		Timeout:    interval,
	}
}

// seed inserts a job directly with a fixed nextRun, bypassing AddJob's
// randomized initial-jitter so tests can control scheduling order exactly.
func seed(m *Manager, job *Job, nextRun time.Time) *JobItem {
	m.jobsMutex.Lock()
	defer m.jobsMutex.Unlock()

	item := &JobItem{job: job, nextRun: nextRun}
	m.jobs[job.ID.String()] = item
	m.jobsByTime.Add(item)
	return item
}

func TestManager_RunsEarliestDueJobFirst(t *testing.T) {
	runner := newMockRunner()
	m := NewManager(runner)

	late := newTestJob(60)
	early := newTestJob(60)

	seed(m, late, time.Now().Add(-1*time.Second))
	seed(m, early, time.Now().Add(-5*time.Second))

	if ran := m.tryRunJob(); !ran {
		t.Fatal("expected a due job to run")
	}

	if runner.callCount() != 1 || runner.calls[0].ID != early.ID {
		t.Fatalf("expected the earliest-due job (early) to run first, got %v", runner.calls)
	}
}

func TestManager_SkipsWhenNothingDue(t *testing.T) {
	runner := newMockRunner()
	m := NewManager(runner)

	seed(m, newTestJob(60), time.Now().Add(time.Hour))

	if m.tryRunJob() {
		t.Fatal("expected tryRunJob to report false when nothing is due")
	}
	if runner.callCount() != 0 {
		t.Fatal("expected the runner not to be called")
	}
}

func TestManager_ReschedulesAfterRun(t *testing.T) {
	runner := newMockRunner()
	m := NewManager(runner)

	job := newTestJob(60)
	seed(m, job, time.Now().Add(-time.Second))

	m.tryRunJob()

	m.jobsMutex.Lock()
	item, exists := m.jobs[job.ID.String()]
	m.jobsMutex.Unlock()

	if !exists {
		t.Fatal("expected job to remain scheduled after a normal run")
	}
	if !item.nextRun.After(time.Now()) {
		t.Fatalf("expected job to be rescheduled into the future, got nextRun=%v", item.nextRun)
	}
	if m.jobsByTime.Len() != 1 {
		t.Fatalf("expected exactly one scheduled entry, got %d", m.jobsByTime.Len())
	}
}

func TestManager_JobStateRemoved_DropsJob(t *testing.T) {
	runner := newMockRunner()
	runner.state = JobStateRemoved
	m := NewManager(runner)

	job := newTestJob(60)
	seed(m, job, time.Now().Add(-time.Second))

	if ran := m.tryRunJob(); ran {
		t.Fatal("expected tryRunJob to report false for a removed job")
	}

	m.jobsMutex.Lock()
	_, exists := m.jobs[job.ID.String()]
	m.jobsMutex.Unlock()

	if exists {
		t.Fatal("expected the removed job to be dropped from the schedule")
	}
	if m.jobsByTime.Len() != 0 {
		t.Fatalf("expected no scheduled entries left, got %d", m.jobsByTime.Len())
	}
}

func TestManager_DoesNotHoldMutexDuringRun(t *testing.T) {
	runner := newMockRunner()
	runner.block = true
	m := NewManager(runner)

	job := newTestJob(60)
	seed(m, job, time.Now().Add(-time.Second))

	done := make(chan bool, 1)
	go func() {
		done <- m.tryRunJob()
	}()

	select {
	case <-runner.entered:
	case <-time.After(time.Second):
		t.Fatal("mock runner was never called")
	}

	// While the "DB call" is blocked, AddJob must still complete promptly —
	// this is the regression check for the mutex being held across the
	// whole DB round-trip.
	addDone := make(chan struct{})
	go func() {
		m.AddJob(newTestJob(60))
		close(addDone)
	}()

	select {
	case <-addDone:
	case <-time.After(time.Second):
		t.Fatal("AddJob was blocked by an in-flight run — jobsMutex is held during the DB round-trip")
	}

	close(runner.proceed)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tryRunJob never completed")
	}
}

func TestManager_ConcurrentUpdateDuringRun_NoDuplicateSchedule(t *testing.T) {
	runner := newMockRunner()
	runner.block = true
	m := NewManager(runner)

	job := newTestJob(60)
	seed(m, job, time.Now().Add(-time.Second))

	done := make(chan bool, 1)
	go func() {
		done <- m.tryRunJob()
	}()

	select {
	case <-runner.entered:
	case <-time.After(time.Second):
		t.Fatal("mock runner was never called")
	}

	// Update the same job while its run is in flight.
	updated := newTestJob(120)
	updated.EntityBase = job.EntityBase
	m.UpdateJob(updated)

	close(runner.proceed)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tryRunJob never completed")
	}

	if m.jobsByTime.Len() != 1 {
		t.Fatalf("expected exactly one scheduled entry after a concurrent update, got %d (duplicate/ghost entry)", m.jobsByTime.Len())
	}

	m.jobsMutex.Lock()
	item := m.jobs[job.ID.String()]
	m.jobsMutex.Unlock()

	if item.job.Interval != 120 {
		t.Fatalf("expected the concurrently-updated job (interval=120) to win, got interval=%d", item.job.Interval)
	}
}

func TestManager_RemoveJob_WhileRunning_NotRescheduled(t *testing.T) {
	runner := newMockRunner()
	runner.block = true
	m := NewManager(runner)

	job := newTestJob(60)
	seed(m, job, time.Now().Add(-time.Second))

	done := make(chan bool, 1)
	go func() {
		done <- m.tryRunJob()
	}()

	select {
	case <-runner.entered:
	case <-time.After(time.Second):
		t.Fatal("mock runner was never called")
	}

	m.RemoveJob(job)
	close(runner.proceed)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tryRunJob never completed")
	}

	if m.jobsByTime.Len() != 0 {
		t.Fatalf("expected the removed job not to be rescheduled, got %d scheduled entries", m.jobsByTime.Len())
	}
	m.jobsMutex.Lock()
	_, exists := m.jobs[job.ID.String()]
	m.jobsMutex.Unlock()
	if exists {
		t.Fatal("expected the removed job to stay removed")
	}
}

func TestManager_RunLoop_StartStop(t *testing.T) {
	runner := newMockRunner()
	m := NewManager(runner)

	job := newTestJob(60)
	seed(m, job, time.Now().Add(-time.Second))

	go m.Run()

	deadline := time.Now().Add(2 * time.Second)
	for runner.callCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runner.callCount() == 0 {
		t.Fatal("expected the run loop to execute the due job")
	}

	m.Stop()
}
