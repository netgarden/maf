package jobs

import (
	"time"

	"github.com/netgarden/maf/database"
)

// Job is a recurring unit of work identified by (HandlerID, TargetID).
// TargetID is nil for a single global job (e.g. "the session cleaner");
// non-nil TargetID lets one Handler run independently per target (e.g. one
// sync job per configured provider).
type Job struct {
	database.EntityBase
	Label     string
	HandlerID string  `gorm:"index:job_target,unique"`
	TargetID  *string `gorm:"index:job_target,unique"`

	// Interval is how often the job should run, in seconds.
	Interval int64
	// Timeout is the maximum expected run duration, in seconds. It bounds
	// the context passed to Handler.Run and the lock lease taken while the
	// job runs — see README.md for what it does and does not guarantee.
	Timeout int64

	LastRunStart           *time.Time
	LastRunHost            *string
	LastRunEnd             *time.Time // nil while the job is currently running
	LastSuccessfulRunStart *time.Time
	LastSuccessfulRunEnd   *time.Time
	LastSuccessfulRunHost  *string
	LastRunError           *string `gorm:"type:text"`
}
