package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/netgarden/maf/locks"
)

func NewJobWrapper(job *Job, handler Handler, lock *locks.Lock) *JobWrapper {
	return &JobWrapper{
		job:     job,
		handler: handler,
		lock:    lock,
	}
}

type JobWrapper struct {
	job     *Job
	handler Handler
	lock    *locks.Lock
}

// Run executes the job's handler with a context deadline derived from the
// job's Timeout, then calls close with the result. Note that a Handler
// that ignores ctx keeps running past its timeout — Go has no way to force
// another goroutine to stop; see README.md.
func (w *JobWrapper) Run(close func(jobID string, jobError *string, lock *locks.Lock)) {
	defer func() {

		jobError := ""
		if r := recover(); r != nil {
			jobError = w.handlePanic(r)
		}

		var jobErrorRet *string
		if jobError != "" {
			jobErrorRet = &jobError
		}

		close(w.job.ID.String(), jobErrorRet, w.lock)
	}()

	if w.handler == nil {
		panic(fmt.Sprintf("no handler registered for job handlerId %q", w.job.HandlerID))
	}

	slog.Info("jobs: job starting",
		slog.String("id", w.job.ID.String()),
		slog.String("handlerId", w.job.HandlerID),
		slog.Any("targetId", w.job.TargetID),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(w.job.Timeout)*time.Second)
	defer cancel()

	w.handler.Run(ctx, w.job)

	slog.Info("jobs: job finished",
		slog.String("id", w.job.ID.String()),
		slog.String("handlerId", w.job.HandlerID),
		slog.Any("targetId", w.job.TargetID),
	)
}

func (w *JobWrapper) handlePanic(v any) string {

	slog.Error("jobs: job run panicked",
		slog.String("id", w.job.ID.String()),
		slog.String("handlerId", w.job.HandlerID),
		slog.Any("targetId", w.job.TargetID),
		slog.Any("panic", v),
	)

	return fmt.Sprintf("%v", v)
}
