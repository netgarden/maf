package locks

import "time"

// LockNamespace partitions locks. Every acquire/release for every ID within
// the same namespace serializes against a single Postgres advisory lock —
// see the "Concurrency characteristics" section in README.md.
type LockNamespace uint64

const (
	// SystemLockNamespace is provided for application-wide coordination
	// locks; define additional namespace values for unrelated lock groups.
	SystemLockNamespace LockNamespace = 0
)

// Lock is a single leased lock row: (Namespace, ID) identifies the lock,
// Key is a random token proving ownership of the current lease so a stale
// Unlock can't remove a lease that has since been re-acquired by someone
// else, and ExpiresAt is what actually makes the lock free again — a lease
// is not tied to a connection or session.
type Lock struct {
	Namespace LockNamespace `gorm:"primary_key; not null"`
	ID        string        `gorm:"primary_key; not null"`
	Key       string
	CreatedAt time.Time
	ExpiresAt time.Time
}
