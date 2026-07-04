package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"

	"github.com/netgarden/maf/database"
	"github.com/netgarden/maf/locks"
)

type JobState int8

const (
	JobStateUnknown JobState = -1
	// JobStateOK means the caller now holds the lock and should run the job.
	JobStateOK JobState = 0
	// JobStateRemoved means the job no longer exists in the database.
	JobStateRemoved JobState = 1
	// JobStateRunning means another instance currently holds the lock.
	// lockJob only checks for this after ruling out JobStateUpdate, so it's
	// only actually observable when Timeout > Interval — with the more
	// typical Timeout <= Interval, any concurrent attempt within Interval
	// of the last run reports JobStateUpdate first. Both states are
	// handled identically by callers (don't run; reschedule from the
	// refreshed Job), so this only affects observability, not scheduling.
	JobStateRunning JobState = 2
	// JobStateUpdate means the job was already started (by any instance,
	// including this one) less than Interval ago — nothing to do but pick
	// up whatever the database now has.
	JobStateUpdate JobState = 3
)

// JobsLockNamespace is the locks.LockNamespace jobs coordinates on. Pick a
// different namespace for other lock users so they don't serialize against
// job scheduling — see the "Concurrency characteristics" note in
// github.com/netgarden/maf/locks's README.
const JobsLockNamespace locks.LockNamespace = 1

// Handler runs a single job invocation. Implementations should respect
// ctx's deadline (derived from the job's Timeout) and return promptly once
// it's done — see README.md: Go cannot forcibly cancel a goroutine that
// ignores it.
type Handler interface {
	Run(ctx context.Context, job *Job)
}

type JobItem struct {
	job     *Job
	nextRun time.Time
}

type LockJobResult struct {
	state JobState
	job   *Job
	lock  *locks.Lock
}

func NewService(db *gorm.DB, locksService *locks.Service) *Service {

	s := &Service{}
	s.db = db
	s.locksService = locksService
	s.manager = NewManager(s)

	go s.manager.Run()

	return s
}

type Service struct {
	db           *gorm.DB
	locksService *locks.Service
	manager      *Manager
}

// Stop stops the scheduler loop. Jobs already running are not interrupted.
func (s *Service) Stop() {
	s.manager.Stop()
}

func (s *Service) RegisterHandler(id string, handler Handler) {
	s.manager.AddHandler(id, handler)
}

func (s *Service) AddJob(handlerID string, targetID string, label string, interval int64, timeout int64) (*Job, error) {

	if interval <= 0 {
		return nil, errors.New("interval must be positive")
	}

	var job *Job

	err := s.db.Transaction(func(tx *gorm.DB) error {

		var err error

		err = s.locksService.LockDBTransaction(tx, JobsLockNamespace)
		if err != nil {
			return err
		}

		job, err = s.getJobByTarget(tx, handlerID, targetID)
		if err != nil {
			return err
		}

		var tmpTargetID *string = nil
		if targetID != "" {
			tmpTargetID = &targetID
		}

		if job == nil {
			job = &Job{
				Label:     label,
				HandlerID: handlerID,
				TargetID:  tmpTargetID,
				Interval:  interval,
				Timeout:   timeout,
			}
			err = tx.Create(job).Error
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	s.manager.AddJob(job)

	return job, nil
}

func (s *Service) UpdateJob(handlerID string, targetID string, label string, interval int64, timeout int64) (*Job, error) {

	if interval <= 0 {
		return nil, errors.New("interval must be positive")
	}

	var job *Job

	err := s.db.Transaction(func(tx *gorm.DB) error {

		var err error

		err = s.locksService.LockDBTransaction(tx, JobsLockNamespace)
		if err != nil {
			return err
		}

		job, err = s.getJobByTarget(tx, handlerID, targetID)
		if err != nil {
			return err
		}
		if job == nil {
			return fmt.Errorf("job not found")
		}

		job.Label = label
		job.Interval = interval
		job.Timeout = timeout

		return tx.Updates(job).Error
	})
	if err != nil {
		return nil, err
	}

	s.manager.UpdateJob(job)

	return job, nil
}

func (s *Service) RemoveJob(handlerID string, targetID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {

		err := s.locksService.LockDBTransaction(tx, JobsLockNamespace)
		if err != nil {
			return err
		}

		job, err := s.getJobByTarget(tx, handlerID, targetID)
		if err != nil {
			return err
		}
		if job == nil {
			return nil
		}

		if err := tx.Delete(job).Error; err != nil {
			return err
		}

		s.manager.RemoveJob(job)

		return nil
	})
}

// RunJob attempts to acquire and run job now, out of its normal schedule.
// It returns the resulting state and, when the DB row has changed (e.g. a
// concurrent update), the refreshed Job.
func (s *Service) RunJob(job *Job) (JobState, *Job) {

	res, err := s.lockJob(job.ID.String())
	if err != nil {
		slog.Error("jobs: RunJob failed to lock job",
			slog.Any("error", err),
			slog.String("id", job.ID.String()),
			slog.String("handlerId", job.HandlerID),
			slog.Any("targetId", job.TargetID),
		)
		return JobStateUnknown, nil
	}

	if res.state != JobStateOK {
		return res.state, res.job
	}

	jobWrapper := NewJobWrapper(res.job, s.manager.GetHandler(res.job.HandlerID), res.lock)

	go jobWrapper.Run(s.endRunJob)

	return res.state, res.job
}

func (s *Service) endRunJob(jobID string, jobError *string, lock *locks.Lock) {
	if err := s.unlockJob(jobID, jobError, lock); err != nil {
		slog.Error("jobs: endRunJob failed to unlock job",
			slog.Any("error", err),
			slog.String("id", jobID),
		)
	}
}

func (s *Service) lockJob(jobID string) (*LockJobResult, error) {

	res := &LockJobResult{
		state: JobStateUnknown,
	}

	jobUUID, err := uuid.FromString(jobID)
	if err != nil {
		return res, err
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {

		job, err := s.getJob(tx, jobID)
		if err != nil {
			return err
		}
		if job == nil {
			res.state = JobStateRemoved
			return nil
		}

		res.job = job

		if job.LastRunStart != nil {
			nextRun := job.LastRunStart.Add(time.Duration(job.Interval) * time.Second)
			if nextRun.After(time.Now()) {
				res.state = JobStateUpdate
				return nil
			}
		}

		// The lease lasts as long as the job's own Timeout, not its
		// (typically much longer) Interval: if this instance crashes
		// mid-run, another instance can retry once the job's expected
		// max run time has passed, rather than waiting a full Interval.
		lock, err := s.locksService.TryLock(JobsLockNamespace, job.ID.String(), time.Duration(job.Timeout)*time.Second)
		if err != nil {
			return err
		}
		if lock == nil {
			res.state = JobStateRunning
			return nil
		}

		hostname, err := os.Hostname()
		if err != nil {
			slog.Warn("jobs: unable to determine hostname", slog.Any("error", err))
			hostname = "unknown"
		}

		err = tx.Model(&Job{EntityBase: database.EntityBase{ID: jobUUID}}).Updates(
			map[string]interface{}{
				"last_run_start": time.Now(),
				"last_run_end":   nil,
				"last_run_host":  &hostname,
				"last_run_error": nil,
			},
		).Error
		if err != nil {
			return err
		}

		res.state = JobStateOK
		res.lock = lock

		return nil
	})
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (s *Service) unlockJob(jobID string, jobError *string, lock *locks.Lock) error {

	jobUUID, err := uuid.FromString(jobID)
	if err != nil {
		return err
	}

	return s.db.Transaction(func(tx *gorm.DB) error {

		job, err := s.getJob(tx, jobID)
		if err != nil {
			return err
		}
		if job == nil {
			// Job was deleted while running; still release the lock below.
			return s.locksService.Unlock(lock)
		}

		now := time.Now()
		data := map[string]interface{}{
			"last_run_end":   &now,
			"last_run_error": jobError,
		}

		if jobError == nil {
			data["last_successful_run_host"] = job.LastRunHost
			data["last_successful_run_start"] = job.LastRunStart
			data["last_successful_run_end"] = &now
		}

		if err := tx.Model(&Job{EntityBase: database.EntityBase{ID: jobUUID}}).Updates(data).Error; err != nil {
			return err
		}

		return s.locksService.Unlock(lock)
	})
}

func (s *Service) GetJob(id string) (*Job, error) {
	return s.getJob(s.db, id)
}

func (s *Service) getJob(db *gorm.DB, id string) (*Job, error) {

	job := &Job{}
	err := db.First(job, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return job, nil
}

func (s *Service) getJobByTarget(db *gorm.DB, handlerID string, targetID string) (*Job, error) {

	job := &Job{}
	tx := db.Model(job).Where("handler_id = ?", handlerID)
	if targetID == "" {
		tx = tx.Where("target_id is null")
	} else {
		tx = tx.Where("target_id = ?", targetID)
	}

	err := tx.First(job).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return job, nil
}
