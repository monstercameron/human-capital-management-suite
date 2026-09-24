package pgtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbedded_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestEmbedded_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

// TestTodo_REV_103_07 proves lock timeout behavior with an injected clock, so
// the test does not depend on host scheduling or elapsed wall time.
func TestTodo_REV_103_07(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "prepare.lock")
	if err := os.WriteFile(lockPath, []byte("held"), 0o644); err != nil {
		t.Fatalf("seed held lock: %v", err)
	}

	start := time.Unix(100, 0)
	now := start
	opts := lockOptions{
		poll:     10 * time.Millisecond,
		wait:     35 * time.Millisecond,
		staleAge: time.Hour,
		now:      func() time.Time { return now },
		sleep:    func(d time.Duration) { now = now.Add(d) },
	}
	called := false
	err := withDirectoryLockOpts(dir, opts, func() error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), lockPath) {
		t.Fatalf("lock error = %v, want timeout naming %q", err, lockPath)
	}
	if called {
		t.Fatal("callback ran while the lock remained held")
	}
	if !now.After(start.Add(opts.wait)) {
		t.Fatalf("injected clock = %s, want it past timeout %s", now, start.Add(opts.wait))
	}
}

// TestTodo_REV_103_07_Golden pins the PostgreSQL version and platform cache
// names used to provision offline test binaries.
func TestTodo_REV_103_07_Golden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		goos, goarch, cacheArch, fetchArch, filename string
	}{
		{"linux", "amd64", "amd64", "amd64", "embedded-postgres-binaries-linux-amd64-17.5.0.txz"},
		{"linux", "arm64", "arm64v8", "arm64v8", "embedded-postgres-binaries-linux-arm64v8-17.5.0.txz"},
		{"darwin", "arm64", "arm64v8", "arm64v8", "embedded-postgres-binaries-darwin-arm64v8-17.5.0.txz"},
		{"windows", "arm64", "arm64", "amd64", "embedded-postgres-binaries-windows-arm64-17.5.0.txz"},
	}
	for _, tc := range cases {
		platform := resolvePlatform(tc.goos, tc.goarch)
		if platform.cacheArch != tc.cacheArch || platform.fetchArch != tc.fetchArch {
			t.Errorf("resolvePlatform(%s, %s) = %+v, want cache=%s fetch=%s", tc.goos, tc.goarch, platform, tc.cacheArch, tc.fetchArch)
		}
		if got := platform.cacheFileName(tc.goos); got != tc.filename {
			t.Errorf("cache filename = %q, want %q", got, tc.filename)
		}
	}
}
