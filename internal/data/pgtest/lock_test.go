package pgtest

// White-box tests for the prepare.lock contention hardening: two processes
// racing to prepare the same embedded-postgres cache directory have been
// observed to fail with "prepare.lock: Access is denied" on Windows instead
// of one waiting for the other. These tests exercise withDirectoryLockOpts
// directly, with short custom lockOptions, so they never touch the package
// state a concurrently running test may be using to prepare the real,
// shared embedded server.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsRetryableLockErr(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	t.Run("AlreadyExists", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(dir, "already-exists")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
		_, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			t.Fatal("expected O_EXCL open of an existing file to fail")
		}
		if !isRetryableLockErr(err) {
			t.Fatalf("isRetryableLockErr(%v) = false, want true (already-exists must be retried)", err)
		}
	})

	t.Run("PermissionDenied", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(dir, "read-only")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatalf("make read-only: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

		_, err := os.OpenFile(path, os.O_WRONLY, 0o644)
		if err == nil {
			// Running as an account that bypasses file permissions (e.g. an
			// elevated/root CI runner). The classification itself is still
			// exercised by the synthetic case below; nothing more to prove
			// on this machine.
			t.Skip("this account can write a read-only file; cannot exercise a real permission-denied error here")
		}
		if !os.IsPermission(err) {
			t.Skipf("opening a read-only file for write did not produce a permission error on this platform: %v", err)
		}
		if !isRetryableLockErr(err) {
			t.Fatalf("isRetryableLockErr(%v) = false, want true (Windows' Access-is-denied variant of lock contention must be retried)", err)
		}
	})

	t.Run("SyntheticPermissionDenied", func(t *testing.T) {
		t.Parallel()
		// Belt-and-braces: prove the classification itself, independent of
		// whether this OS/account enforces file permissions, by wrapping
		// the sentinel isRetryableLockErr checks against.
		err := &os.PathError{Op: "open", Path: "prepare.lock", Err: os.ErrPermission}
		if !isRetryableLockErr(err) {
			t.Fatalf("isRetryableLockErr(%v) = false, want true", err)
		}
	})

	t.Run("NonRetryable", func(t *testing.T) {
		t.Parallel()
		// A parent directory that does not exist yields "no such file or
		// directory" / "the system cannot find the path specified" - neither
		// already-exists nor permission-denied, so it must fail fast rather
		// than retry for ten minutes.
		path := filepath.Join(dir, "missing-parent", "prepare.lock")
		_, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			t.Fatal("expected opening a file under a missing directory to fail")
		}
		if os.IsExist(err) || os.IsPermission(err) {
			t.Skipf("this error unexpectedly classifies as exist/permission, cannot prove the negative case: %v", err)
		}
		if isRetryableLockErr(err) {
			t.Fatalf("isRetryableLockErr(%v) = true, want false (a genuinely broken path must not retry silently)", err)
		}
	})
}

func TestWithDirectoryLockUncontended(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := lockOptions{poll: 5 * time.Millisecond, wait: time.Second, staleAge: time.Hour}

	called := 0
	if err := withDirectoryLockOpts(dir, opts, func() error {
		called++
		return nil
	}); err != nil {
		t.Fatalf("withDirectoryLockOpts: %v", err)
	}
	if called != 1 {
		t.Fatalf("fn called %d times, want 1", called)
	}
	if _, err := os.Stat(filepath.Join(dir, "prepare.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file left behind after a successful, uncontended acquisition: err=%v", err)
	}
}

func TestWithDirectoryLockPropagatesFnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := lockOptions{poll: 5 * time.Millisecond, wait: time.Second, staleAge: time.Hour}

	sentinel := errors.New("boom")
	err := withDirectoryLockOpts(dir, opts, func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("withDirectoryLockOpts error = %v, want %v", err, sentinel)
	}
	// The lock must still be released even though fn failed, or every
	// subsequent attempt in this process would hang.
	if _, err := os.Stat(filepath.Join(dir, "prepare.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file left behind after fn returned an error: err=%v", err)
	}
}

// TestWithDirectoryLockRetriesWhileHeld is the RED/GREEN proof for the
// hardening: a lock file left behind (by another process, or a synthetic
// "Access is denied" style failure) blocks acquisition until it is released,
// rather than either failing immediately or hanging forever.
func TestWithDirectoryLockRetriesWhileHeld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	enteredWait := make(chan struct{}, 1)
	releaseWait := make(chan struct{})
	opts := lockOptions{
		poll: 15 * time.Millisecond, wait: 5 * time.Second, staleAge: time.Hour,
		sleep: func(time.Duration) {
			enteredWait <- struct{}{}
			<-releaseWait
		},
	}

	lockPath := filepath.Join(dir, "prepare.lock")
	if err := os.WriteFile(lockPath, []byte("held by another process"), 0o644); err != nil {
		t.Fatalf("seed held lock: %v", err)
	}

	var called int32
	done := make(chan error, 1)
	go func() {
		done <- withDirectoryLockOpts(dir, opts, func() error {
			atomic.AddInt32(&called, 1)
			return nil
		})
	}()

	select {
	case <-enteredWait:
	case err := <-done:
		t.Fatalf("withDirectoryLockOpts returned (err=%v) while the lock was still held; it must wait", err)
	case <-time.After(3 * time.Second):
		t.Fatal("withDirectoryLockOpts did not reach its retry wait")
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("fn ran before the held lock was released")
	}

	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("release held lock: %v", err)
	}
	close(releaseWait)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("withDirectoryLockOpts: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("withDirectoryLockOpts did not unblock within 3s of the lock being released")
	}
	if got := atomic.LoadInt32(&called); got != 1 {
		t.Fatalf("fn called %d times, want exactly 1", got)
	}
}

// TestWithDirectoryLockTimesOutWithClearError proves the retry is bounded
// (a genuinely stuck lock does not hang a test run forever) and that the
// resulting error names the lock path, so a real "Access is denied" report
// points straight at the file to inspect.
func TestWithDirectoryLockTimesOutWithClearError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := lockOptions{poll: 10 * time.Millisecond, wait: 100 * time.Millisecond, staleAge: time.Hour}

	lockPath := filepath.Join(dir, "prepare.lock")
	if err := os.WriteFile(lockPath, []byte("held forever"), 0o644); err != nil {
		t.Fatalf("seed held lock: %v", err)
	}

	called := false
	now := time.Unix(100, 0)
	waits := 0
	opts.now = func() time.Time { return now }
	opts.sleep = func(d time.Duration) {
		waits++
		now = now.Add(d)
	}
	err := withDirectoryLockOpts(dir, opts, func() error {
		called = true
		return nil
	})

	if err == nil {
		t.Fatal("expected a timeout error while the lock file is held for longer than the wait budget")
	}
	if called {
		t.Fatal("fn ran despite the lock never being released")
	}
	if !strings.Contains(err.Error(), lockPath) {
		t.Fatalf("error %q does not name the lock path %q", err.Error(), lockPath)
	}
	if waits == 0 || now.Before(time.Unix(100, 0).Add(opts.wait)) {
		t.Fatalf("lock wait did not advance through the configured %s timeout: waits=%d now=%s", opts.wait, waits, now)
	}
}

// TestWithDirectoryLockReclaimsStaleLock proves a lock file abandoned by a
// killed process (old mtime) is reclaimed rather than blocking every future
// test run against this cache directory.
func TestWithDirectoryLockReclaimsStaleLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := lockOptions{poll: 10 * time.Millisecond, wait: 3 * time.Second, staleAge: 30 * time.Millisecond}

	lockPath := filepath.Join(dir, "prepare.lock")
	if err := os.WriteFile(lockPath, []byte("orphaned"), 0o644); err != nil {
		t.Fatalf("seed orphaned lock: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("backdate orphaned lock: %v", err)
	}

	called := false
	if err := withDirectoryLockOpts(dir, opts, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("withDirectoryLockOpts: %v", err)
	}
	if !called {
		t.Fatal("fn did not run; the stale lock should have been reclaimed")
	}
}
