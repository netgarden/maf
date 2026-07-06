package mailer

import (
	"context"
	"log/slog"

	"github.com/netgarden/maf/jobs"
)

// NewTickHandler adapts Service.ProcessBatch to jobs.Handler, so mailer's
// queue-processing tick is scheduled and coordinated across replicas by
// jobs' own per-tick lease exactly like any other recurring job.
func NewTickHandler(service *Service) jobs.Handler {
	return &tickHandler{service: service}
}

type tickHandler struct {
	service *Service
}

// Run has no return value (see jobs.Handler) — the wrapper that calls it
// only records a panic as the job's LastRunError, so a normal (expected,
// transient) processing error is logged here rather than propagated.
func (h *tickHandler) Run(ctx context.Context, job *jobs.Job) {
	if err := h.service.ProcessBatch(ctx); err != nil {
		slog.Error("mailer: queue processing tick failed", slog.Any("error", err))
	}
}

// NewRetentionHandler adapts Service.PurgeOldEmails to jobs.Handler, so old
// terminal-status emails are cleaned up on their own recurring tick,
// coordinated across replicas exactly like the send tick.
func NewRetentionHandler(service *Service, cfg RetentionConfig) jobs.Handler {
	return &retentionHandler{service: service, cfg: cfg}
}

type retentionHandler struct {
	service *Service
	cfg     RetentionConfig
}

func (h *retentionHandler) Run(ctx context.Context, job *jobs.Job) {
	deleted, err := h.service.PurgeOldEmails(h.cfg)
	if err != nil {
		slog.Error("mailer: retention purge failed", slog.Any("error", err))
		return
	}
	if deleted > 0 {
		slog.Info("mailer: retention purge deleted old emails", slog.Int64("count", deleted))
	}
}
