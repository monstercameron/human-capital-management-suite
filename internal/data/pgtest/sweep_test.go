package pgtest

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func writePidFile(t *testing.T, runtimeDir string, pid int) {
	t.Helper()
	dataDir := filepath.Join(runtimeDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	body := strconv.Itoa(pid) + "\n" + runtimeDir + "\n"
	if err := os.WriteFile(filepath.Join(dataDir, "postmaster.pid"), []byte(body), 0o644); err != nil {
		t.Fatalf("write postmaster.pid: %v", err)
	}
}

func makeRuntimeDir(t *testing.T, base, name string, age time.Duration, now time.Time) string {
	t.Helper()
	path := filepath.Join(base, name)
	if err := os.MkdirAll(filepath.Join(path, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	stamp := now.Add(-age)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("chtimes %s: %v", name, err)
	}
	return path
}

func TestSweepStaleRuntimesRemovesOnlyOldDeadRuntimes(t *testing.T) {
	base := t.TempDir()
	now := time.Now()

	oldNoPid := makeRuntimeDir(t, base, runtimePrefix+"old-nopid", 3*time.Hour, now)
	oldDeadPid := makeRuntimeDir(t, base, runtimePrefix+"old-dead", 3*time.Hour, now)
	// A pid this large is never a live process on any supported platform.
	writePidFile(t, oldDeadPid, 1<<30)
	oldLive := makeRuntimeDir(t, base, runtimePrefix+"old-live", 3*time.Hour, now)
	writePidFile(t, oldLive, os.Getpid())
	fresh := makeRuntimeDir(t, base, runtimePrefix+"fresh", 10*time.Minute, now)
	unrelated := makeRuntimeDir(t, base, "other-old", 3*time.Hour, now)
	// Age is re-stamped after the pid files were written into the old dirs.
	for _, p := range []string{oldDeadPid, oldLive} {
		stamp := now.Add(-3 * time.Hour)
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatalf("restamp: %v", err)
		}
	}

	removed := sweepStaleRuntimes(base, staleRuntimeAge, now)

	want := map[string]bool{oldNoPid: true, oldDeadPid: true}
	if len(removed) != len(want) {
		t.Fatalf("removed %v, want exactly %v", removed, want)
	}
	for _, p := range removed {
		if !want[p] {
			t.Errorf("removed unexpected %s", p)
		}
	}
	for _, p := range []string{oldNoPid, oldDeadPid} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err=%v)", p, err)
		}
	}
	for _, p := range []string{oldLive, fresh, unrelated} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should have survived: %v", p, err)
		}
	}
}

func TestSweepStaleRuntimesToleratesMissingBase(t *testing.T) {
	if got := sweepStaleRuntimes(filepath.Join(t.TempDir(), "absent"), staleRuntimeAge, time.Now()); got != nil {
		t.Fatalf("removed %v from a missing base", got)
	}
}

func TestSweepStaleRuntimesBoundsRemovalPerCall(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	paths := make([]string, maxRuntimesPerSweep+2)
	for i := range paths {
		paths[i] = makeRuntimeDir(t, base, runtimePrefix+strconv.Itoa(i), 3*time.Hour, now)
	}

	first := sweepStaleRuntimes(base, staleRuntimeAge, now)
	if len(first) != maxRuntimesPerSweep {
		t.Fatalf("first sweep removed %d directories, want at most %d", len(first), maxRuntimesPerSweep)
	}
	remaining := 0
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			remaining++
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
	}
	if remaining != len(paths)-maxRuntimesPerSweep {
		t.Fatalf("first sweep left %d directories, want %d", remaining, len(paths)-maxRuntimesPerSweep)
	}
	second := sweepStaleRuntimes(base, staleRuntimeAge, now)
	if len(second) != remaining {
		t.Fatalf("second sweep removed %d directories, want %d", len(second), remaining)
	}
}

func TestPostmasterAliveReadsTheFirstLine(t *testing.T) {
	dir := t.TempDir()
	if postmasterAlive(filepath.Join(dir, "missing.pid")) {
		t.Fatal("missing pid file reported alive")
	}
	writePidFile(t, dir, os.Getpid())
	if !postmasterAlive(filepath.Join(dir, "data", "postmaster.pid")) {
		t.Fatal("this process reported dead")
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "postmaster.pid"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if postmasterAlive(filepath.Join(dir, "data", "postmaster.pid")) {
		t.Fatal("garbage pid reported alive")
	}
}
