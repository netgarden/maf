package jobs

import (
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/netgarden/orderedlist"
)

// jobRunner is the subset of Service the Manager needs, so scheduling logic
// can be tested without a real database.
type jobRunner interface {
	RunJob(job *Job) (JobState, *Job)
}

func NewManager(runner jobRunner) *Manager {

	m := &Manager{}
	m.runner = runner
	m.jobs = make(map[string]*JobItem)
	m.jobsByTime = orderedlist.New(compareJobItemTime)
	m.handlers = make(map[string]Handler)

	return m
}

type Manager struct {
	runner        jobRunner
	jobsMutex     sync.Mutex
	jobs          map[string]*JobItem
	jobsByTime    *orderedlist.List[*JobItem]
	handlersMutex sync.Mutex
	handlers      map[string]Handler
	running       atomic.Bool
}

func compareJobItemTime(a, b *JobItem) int {
	return a.nextRun.Compare(b.nextRun)
}

func (m *Manager) AddHandler(id string, handler Handler) {

	m.handlersMutex.Lock()
	defer m.handlersMutex.Unlock()

	m.handlers[id] = handler
}

func (m *Manager) GetHandler(handlerID string) Handler {

	m.handlersMutex.Lock()
	defer m.handlersMutex.Unlock()

	return m.handlers[handlerID]
}

func (m *Manager) AddJob(job *Job) {

	m.jobsMutex.Lock()
	defer m.jobsMutex.Unlock()

	m.addJobLocked(job)
}

func (m *Manager) addJobLocked(job *Job) {

	var nextRun time.Time
	nextRunSet := false
	if job.LastRunStart != nil {
		nextRun = job.LastRunStart.Add(time.Duration(job.Interval) * time.Second)
		nextRunSet = nextRun.After(time.Now())
	}

	if !nextRunSet {
		// Jitter a brand-new job's first run within its interval, so many
		// jobs registered at the same instant (e.g. on app startup) don't
		// all fire simultaneously.
		nextRun = time.Now().Add(time.Duration(rand.Int63n(job.Interval)) * time.Second)
	}

	jobItem := &JobItem{
		job:     job,
		nextRun: nextRun,
	}

	m.jobs[job.ID.String()] = jobItem
	m.jobsByTime.Add(jobItem)
}

func (m *Manager) UpdateJob(job *Job) {

	m.jobsMutex.Lock()
	defer m.jobsMutex.Unlock()

	m.removeJobLocked(job)
	m.addJobLocked(job)
}

func (m *Manager) RemoveJob(job *Job) {

	m.jobsMutex.Lock()
	defer m.jobsMutex.Unlock()

	m.removeJobLocked(job)
}

func (m *Manager) removeJobLocked(job *Job) {
	m.jobsByTime.Remove(m.jobs[job.ID.String()])
	delete(m.jobs, job.ID.String())
}

// Run drives the scheduler loop until Stop is called. It's meant to run in
// its own goroutine (see NewService).
func (m *Manager) Run() {

	if !m.running.CompareAndSwap(false, true) {
		slog.Error("jobs: Manager already running")
		return
	}

	for m.running.Load() {

		ranJob := m.tryRunJob()

		delay := 10 * time.Millisecond
		if !ranJob {
			delay = 100 * time.Millisecond
		}
		time.Sleep(delay)
	}
}

func (m *Manager) Stop() {
	m.running.Store(false)
}

// tryRunJob pops the earliest-due job (if any) and runs it. The DB/lock
// round-trip in runner.RunJob deliberately happens without holding
// jobsMutex, so AddJob/UpdateJob/RemoveJob from other goroutines are never
// blocked on it.
func (m *Manager) tryRunJob() bool {

	jobItem, ok := m.popDueJob()
	if !ok {
		return false
	}

	state, job := m.runner.RunJob(jobItem.job)
	if job != nil {
		jobItem.job = job
	}

	return m.rescheduleAfterRun(jobItem, state)
}

func (m *Manager) popDueJob() (*JobItem, bool) {

	m.jobsMutex.Lock()
	defer m.jobsMutex.Unlock()

	jobItem, ok := m.jobsByTime.Peek()
	if !ok || jobItem.nextRun.After(time.Now()) {
		return nil, false
	}

	m.jobsByTime.RemoveAt(0)

	if _, exists := m.jobs[jobItem.job.ID.String()]; !exists {
		// Removed between Peek and here; nothing to run.
		return nil, false
	}

	return jobItem, true
}

// rescheduleAfterRun re-inserts jobItem into the schedule after a run
// completes, unless a concurrent UpdateJob or RemoveJob replaced or dropped
// it while it was running. Pointer identity against the current map entry
// is what detects that: UpdateJob/RemoveJob already fully handled
// rescheduling (or removal) for their own fresh JobItem, so re-adding here
// too would leave a duplicate entry in jobsByTime for the same job.
func (m *Manager) rescheduleAfterRun(jobItem *JobItem, state JobState) bool {

	m.jobsMutex.Lock()
	defer m.jobsMutex.Unlock()

	current, exists := m.jobs[jobItem.job.ID.String()]
	if !exists || current != jobItem {
		// Removed, or replaced by a concurrent UpdateJob, while running —
		// that call already left the schedule (and map) in the right
		// state; there's nothing left for us to do.
		return state == JobStateOK
	}

	// current == jobItem: no concurrent Update/Remove happened.
	if state == JobStateRemoved {
		delete(m.jobs, jobItem.job.ID.String())
		return false
	}

	lastRun := time.Now()
	if jobItem.job.LastRunStart != nil {
		lastRun = *jobItem.job.LastRunStart
	}
	jobItem.nextRun = lastRun.Add(time.Duration(jobItem.job.Interval) * time.Second)

	m.jobsByTime.Add(jobItem)

	return state == JobStateOK
}
