package pgtest

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// runtimePrefix names the per-process runtime directories startEmbedded
// creates under the system temp folder. Each one holds a full extracted
// PostgreSQL distribution plus a cluster, so a test process that is killed
// before its stop function runs (a go test timeout, an interrupted lane)
// leaves well over a hundred megabytes behind. sweepStaleRuntimes reclaims
// those before the next server starts.
const runtimePrefix = "hcmnext-pg-"

// staleRuntimeAge is how old a sibling runtime directory must be before the
// sweep considers it abandoned. No single test package runs this long, and a
// live server is additionally protected by its postmaster.pid liveness check.
const staleRuntimeAge = 2 * time.Hour

// maxRuntimesPerSweep bounds startup work when a machine has accumulated many
// abandoned clusters. Later test processes continue reclaiming eligible dirs.
const maxRuntimesPerSweep = 8

// sweepStaleRuntimes removes abandoned runtime directories under base that
// were created by earlier pgtest processes. A directory is abandoned when it
// is older than maxAge and no postmaster recorded in its data/postmaster.pid
// is still alive. Removal failures are ignored: on Windows a directory whose
// server is still running cannot be removed, which is the safe outcome.
// It returns the paths it removed.
func sweepStaleRuntimes(base string, maxAge time.Duration, now time.Time) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var removed []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), runtimePrefix) {
			continue
		}
		path := filepath.Join(base, entry.Name())
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < maxAge {
			continue
		}
		if postmasterAlive(filepath.Join(path, "data", "postmaster.pid")) {
			continue
		}
		if err := os.RemoveAll(path); err == nil {
			removed = append(removed, path)
			if len(removed) == maxRuntimesPerSweep {
				break
			}
		}
	}
	return removed
}

// postmasterAlive reports whether the process named on the first line of a
// PostgreSQL postmaster.pid file still exists. A missing or unreadable file
// means no live server.
func postmasterAlive(pidFile string) bool {
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	first, _, _ := strings.Cut(string(raw), "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(first))
	if err != nil || pid <= 0 {
		return false
	}
	return processExists(pid)
}

// processExists reports whether a process with the given id is running. On
// Windows os.FindProcess opens a handle and fails for a dead process; on other
// systems it always succeeds, so a zero signal probes liveness instead.
func processExists(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
