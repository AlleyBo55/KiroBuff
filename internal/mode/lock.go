package mode

// Locking around the read-then-write in On and Off.
//
// Without it the cap is advisory only. Seven concurrent `kirobuff mode on`
// invocations each read "0 active", each saw room under the cap of 6, and each
// created a symlink: seven modes active. Classic time-of-check to time-of-use,
// and easy to hit for real because a shell loop with & is how people enable
// several modes at once.
//
// The lock is an O_EXCL create, which is atomic on every filesystem this runs
// on and needs no platform-specific syscall. A lock older than lockStale is
// treated as abandoned, because a process killed mid-operation must not wedge
// the command forever.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	lockName    = ".kirobuff.lock"
	lockStale   = 30 * time.Second
	lockTimeout = 5 * time.Second
	lockPoll    = 20 * time.Millisecond
)

// ErrLocked means another kirobuff process held the lock for too long.
var ErrLocked = errors.New("another kirobuff process is holding the mode lock")

// lockPath keeps the lock beside the library rather than in the steering
// directory, so it never looks like a steering file to Kiro CLI.
func (l Layout) lockPath() string {
	return filepath.Join(l.Library, lockName)
}

// withLock runs fn while holding an exclusive lock.
func withLock(l Layout, fn func() error) error {
	if err := os.MkdirAll(l.Library, 0o755); err != nil {
		return err
	}
	path := l.lockPath()

	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			// Record the holder so a stale lock can be explained.
			// Best effort: the pid only helps a human diagnose a stale lock.
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			defer func() { _ = os.Remove(path) }()
			return fn()
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}

		// Someone holds it. Break an abandoned lock rather than waiting forever
		// on a process that died.
		if fi, statErr := os.Stat(path); statErr == nil &&
			time.Since(fi.ModTime()) > lockStale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w (%s); remove it if no kirobuff is running", ErrLocked, path)
		}
		time.Sleep(lockPoll)
	}
}
