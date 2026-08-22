package locks

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NewService constructs a Service backed by the given database connection.
func NewService(db *gorm.DB) *Service {
	return &Service{
		db: db,
	}
}

// Service acquires and releases leased locks stored in Postgres.
type Service struct {
	db *gorm.DB
}

// Lock blocks until it acquires the named lock or timeout elapses, retrying
// as soon as a currently-held lock's lease is expected to expire.
func (s *Service) Lock(namespace LockNamespace, id string, lockTimeout time.Duration, timeout time.Duration) (*Lock, error) {

	timeLimit := time.Now().Add(timeout)

	for {

		lock, valid, err := s.tryLock(namespace, id, lockTimeout)
		if err != nil {
			return nil, err
		}

		if valid {
			return lock, nil
		}

		now := time.Now()
		if now.After(timeLimit) {
			return nil, errors.New("timeout")
		}

		// Wake at the earlier of our own deadline and the current lock's
		// lease expiry, so we retry promptly instead of oversleeping.
		var sleepDuration time.Duration
		if timeLimit.Before(lock.ExpiresAt) {
			sleepDuration = timeLimit.Sub(now)
		} else {
			sleepDuration = lock.ExpiresAt.Sub(now)
		}

		time.Sleep(sleepDuration)
	}
}

// TryLock makes a single, non-blocking attempt to acquire the named lock
// with a lease valid for timeout. It returns (nil, nil), not an error, when
// the lock is currently held by someone else.
func (s *Service) TryLock(namespace LockNamespace, id string, timeout time.Duration) (*Lock, error) {

	lock, valid, err := s.tryLock(namespace, id, timeout)
	if err != nil {
		return nil, err
	}

	if !valid {
		return nil, nil
	}

	return lock, nil
}

func (s *Service) tryLock(namespace LockNamespace, id string, timeout time.Duration) (*Lock, bool, error) {

	var lock *Lock
	var valid bool

	err := s.db.Transaction(func(tx *gorm.DB) error {

		var err error

		err = s.LockDBTransaction(tx, namespace)
		if err != nil {
			return err
		}

		lock, err = s.getLock(tx, namespace, id)
		if err != nil {
			return err
		}

		if lock != nil && lock.ExpiresAt.After(time.Now()) {
			valid = false
			return nil
		}

		exists := true
		if lock == nil {
			exists = false
			lock = &Lock{}
		}

		lock.ID = id
		lock.Namespace = namespace
		lock.CreatedAt = time.Now()
		lock.ExpiresAt = lock.CreatedAt.Add(timeout)
		lock.Key = uuid.New().String()

		if exists {
			err = tx.Updates(lock).Error
		} else {
			err = tx.Create(lock).Error
		}

		if err == nil {
			valid = true
		}

		return err
	})
	if err != nil {
		return nil, false, err
	}

	return lock, valid, nil
}

// Renew extends lock's lease by timeout from now, but only if it is still
// the current lease holder for its (Namespace, ID) — same ownership check
// as Unlock, via the lease's Key token (TryLock itself has no notion of
// caller identity, so a plain re-TryLock can't be used for this: it always
// fails while any lease, including the caller's own, is unexpired). Returns
// (nil, nil), not an error, if the lease has since expired and been taken
// over by someone else — mirroring TryLock's "not held" signal — callers
// should treat that as having lost the lease and stop acting as its holder.
func (s *Service) Renew(lock *Lock, timeout time.Duration) (*Lock, error) {

	var renewed *Lock

	err := s.db.Transaction(func(tx *gorm.DB) error {

		if err := s.LockDBTransaction(tx, lock.Namespace); err != nil {
			return err
		}

		dbLock, err := s.getLock(tx, lock.Namespace, lock.ID)
		if err != nil {
			return err
		}
		if dbLock == nil || dbLock.Key != lock.Key {
			return nil
		}

		dbLock.ExpiresAt = time.Now().Add(timeout)
		if err := tx.Updates(dbLock).Error; err != nil {
			return err
		}

		renewed = dbLock

		return nil
	})
	if err != nil {
		return nil, err
	}

	return renewed, nil
}

// Unlock releases lock, but only if it is still the current lease holder
// for its (Namespace, ID) — an Unlock for a lease that has since expired
// and been replaced by a new acquisition is a safe no-op.
func (s *Service) Unlock(lock *Lock) error {
	return s.db.Transaction(func(tx *gorm.DB) error {

		var err error

		err = s.LockDBTransaction(tx, lock.Namespace)
		if err != nil {
			return err
		}

		dbLock, err := s.getLock(tx, lock.Namespace, lock.ID)
		if err != nil {
			return err
		}
		if dbLock == nil {
			return nil
		}

		if dbLock.Key != lock.Key {
			return nil
		}

		return tx.Delete(dbLock).Error
	})
}

func (s *Service) getLock(db *gorm.DB, namespace LockNamespace, id string) (*Lock, error) {

	lock := &Lock{
		Namespace: namespace,
		ID:        id,
	}
	db = db.First(lock)

	err := db.Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return lock, nil
}

// LockDBTransaction takes the Postgres advisory lock that serializes
// acquire/release of every lock within namespace for the life of the given
// transaction. Exported so callers can extend the critical section of a
// custom lock operation across the same DB transaction if needed.
func (s *Service) LockDBTransaction(db *gorm.DB, namespace LockNamespace) error {
	// Exec, not Raw: Raw builds a query but only sends it to the server
	// once a finisher (Scan/Row/Exec/...) runs — a bare db.Raw(...).Error
	// with no finisher never executes at all and always reports a nil
	// error, silently skipping the lock entirely. Confirmed directly: a
	// db.Raw() call referencing a nonexistent SQL function still returned
	// Error == nil.
	return db.Exec("SELECT pg_advisory_xact_lock(?)", namespace).Error
}

// RunExclusive runs fn inside a new transaction serialized against every
// other RunExclusive (or manual LockDBTransaction) call for the same
// namespace across every replica sharing this database — the lock is
// released automatically when the transaction ends, whether fn returns nil
// or an error. Meant for one-off startup/bootstrap operations (e.g. "create
// a default record if it doesn't exist yet") where every replica runs the
// same check-then-act logic concurrently and only one should actually act;
// for a lease held across work that isn't a single DB transaction, use
// Lock/TryLock instead.
func (s *Service) RunExclusive(namespace LockNamespace, fn func(tx *gorm.DB) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.LockDBTransaction(tx, namespace); err != nil {
			return err
		}
		return fn(tx)
	})
}
