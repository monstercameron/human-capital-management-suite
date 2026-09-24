package pgtest

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

const (
	// postgresVersion is the server every test in this repository runs against.
	postgresVersion = embeddedpostgres.V17
	// versionString must match postgresVersion; it names the cached archive.
	versionString = "17.5.0"
	// binaryRepository is the zonky Maven repository embedded-postgres fetches from.
	binaryRepository = "https://repo1.maven.org/maven2"
)

// binaryPlatform describes which zonky artifact this machine needs.
type binaryPlatform struct {
	// cacheArch is the architecture embedded-postgres itself will look for in the
	// cache directory. It must match the library's own naming exactly or the
	// library will try to download again.
	cacheArch string
	// fetchArch is the architecture actually published by zonky for this machine.
	// On windows/arm64 it is amd64, which runs under the Windows x64 emulator.
	fetchArch string
	// emulated records that fetchArch differs from the machine's own architecture.
	emulated bool
}

// resolvePlatform mirrors embedded-postgres' own version strategy so that the
// archive we place in the cache is the archive it will look for, and picks a
// published artifact when the machine's own architecture has no build.
func resolvePlatform(goos, goarch string) binaryPlatform {
	cacheArch := goarch
	switch goos {
	case "linux":
		if goarch == "arm64" {
			cacheArch = "arm64v8"
		}
	case "darwin":
		if goarch == "arm64" {
			cacheArch = "arm64v8"
		}
	}

	fetchArch := cacheArch
	emulated := false
	// zonky publishes no windows/arm64 build. Windows on ARM64 runs x64 binaries
	// through its emulator, so the amd64 artifact is the correct one to fetch.
	if goos == "windows" && goarch == "arm64" {
		fetchArch = "amd64"
		emulated = true
	}

	return binaryPlatform{cacheArch: cacheArch, fetchArch: fetchArch, emulated: emulated}
}

// cacheFileName is the archive name embedded-postgres derives for this machine.
func (p binaryPlatform) cacheFileName(goos string) string {
	return fmt.Sprintf("embedded-postgres-binaries-%s-%s-%s.txz", goos, p.cacheArch, versionString)
}

func (p binaryPlatform) jarURL(goos string) string {
	return fmt.Sprintf(
		"%s/io/zonky/test/postgres/embedded-postgres-binaries-%s-%s/%s/embedded-postgres-binaries-%s-%s-%s.jar",
		binaryRepository, goos, p.fetchArch, versionString, goos, p.fetchArch, versionString)
}

// cacheDir returns the directory holding the downloaded PostgreSQL archive. It
// is deliberately outside the repository.
func cacheDir() (string, error) {
	if dir := os.Getenv(EnvCacheDir); dir != "" {
		return dir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "hcm-next", "embedded-postgres"), nil
}

// ensureArchive makes sure the cache holds the PostgreSQL archive under the name
// embedded-postgres will look for. When the machine's architecture has no
// published build we download the emulated one ourselves and drop it into place,
// which is the only supported way to override the library's version strategy:
// Config exposes CachePath and BinaryRepositoryURL but not VersionStrategy.
func ensureArchive(dir string, platform binaryPlatform) (string, error) {
	archive := filepath.Join(dir, platform.cacheFileName(runtime.GOOS))
	if info, err := os.Stat(archive); err == nil && !info.IsDir() && info.Size() > 0 {
		return archive, nil
	}
	if !platform.emulated {
		// The library can fetch this platform itself; let it.
		return archive, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create cache directory %s: %w", dir, err)
	}

	url := platform.jarURL(runtime.GOOS)
	body, err := download(url)
	if err != nil {
		return "", err
	}

	txz, err := extractArchiveFromJar(body, url)
	if err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(dir, "pg-archive-*")
	if err != nil {
		return "", fmt.Errorf("create temporary archive: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(txz); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("write temporary archive: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("close temporary archive: %w", err)
	}
	if err := os.Rename(tmpName, archive); err != nil {
		// Another process may have won the race; that is fine.
		_ = os.Remove(tmpName)
		if info, statErr := os.Stat(archive); statErr != nil || info.Size() == 0 {
			return "", fmt.Errorf("publish archive %s: %w", archive, err)
		}
	}
	return archive, nil
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return body, nil
}

// extractArchiveFromJar pulls the single .txz entry out of a zonky jar.
func extractArchiveFromJar(jar []byte, url string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(jar), int64(len(jar)))
	if err != nil {
		return nil, fmt.Errorf("open jar from %s: %w", url, err)
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !strings.HasSuffix(file.Name, ".txz") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s in jar: %w", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s in jar: %w", file.Name, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s in jar: %w", file.Name, closeErr)
		}
		return content, nil
	}
	return nil, fmt.Errorf("no .txz entry in jar from %s", url)
}

// freePort asks the operating system for an unused localhost port.
func freePort() (uint32, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve port: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return 0, errors.New("reserve port: unexpected address type")
	}
	port := uint32(addr.Port)
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("release reserved port: %w", err)
	}
	return port, nil
}

// lockOptions controls how long withDirectoryLockOpts waits for a contended
// lock, how often it polls, and how old an abandoned lock file must be before
// it is reclaimed. withDirectoryLock always runs with defaultLockOptions;
// tests pass their own short-lived lockOptions value instead of mutating
// package state, so they never race the real cache lock that a concurrently
// running test may be legitimately holding for the shared embedded server.
type lockOptions struct {
	poll     time.Duration
	wait     time.Duration
	staleAge time.Duration
	now      func() time.Time
	sleep    func(time.Duration)
}

var defaultLockOptions = lockOptions{
	poll:     250 * time.Millisecond,
	wait:     10 * time.Minute,
	staleAge: 15 * time.Minute,
}

// isRetryableLockErr reports whether err from creating the lock file means
// "someone else holds it right now" rather than a permanent failure such as
// an invalid path or a full disk. Besides the usual already-exists error,
// Windows can surface ERROR_ACCESS_DENIED ("Access is denied") from
// OpenFile(O_CREATE|O_EXCL) instead of ERROR_FILE_EXISTS during the brief
// window where another process is itself mid-create or mid-delete of the
// same path; os.IsExist alone does not recognise that as contention and
// would abort the whole embedded-server startup instead of retrying.
func isRetryableLockErr(err error) bool {
	return os.IsExist(err) || os.IsPermission(err)
}

// withDirectoryLock serializes the one-time archive download across concurrently
// running `go test` processes. Once the archive is cached every process extracts
// and starts its own server independently.
func withDirectoryLock(dir string, fn func() error) error {
	return withDirectoryLockOpts(dir, defaultLockOptions, fn)
}

func withDirectoryLockOpts(dir string, opts lockOptions, fn func() error) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache directory %s: %w", dir, err)
	}
	lockPath := filepath.Join(dir, "prepare.lock")
	now := opts.now
	if now == nil {
		now = time.Now
	}
	sleep := opts.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	deadline := now().Add(opts.wait)
	attempts := 0
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = file.Close()
			defer func() { _ = os.Remove(lockPath) }()
			return fn()
		}
		attempts++
		if !isRetryableLockErr(err) {
			return fmt.Errorf("acquire lock %s: %w", lockPath, err)
		}
		// Reclaim a lock left behind by a killed process.
		if info, statErr := os.Stat(lockPath); statErr == nil && now().Sub(info.ModTime()) > opts.staleAge {
			_ = os.Remove(lockPath)
			continue
		}
		if now().After(deadline) {
			return fmt.Errorf("timed out waiting for lock %s after %d attempt(s) over %s (last error: %v)",
				lockPath, attempts, opts.wait, err)
		}
		sleep(opts.poll)
	}
}

// removeWithRetry deletes a directory that a just-exited process may still hold
// open. It gives up quietly: a leftover directory under the system temp folder
// is untidy, not a test failure.
func removeWithRetry(path string) error {
	var err error
	for attempt := range 20 {
		if err = os.RemoveAll(path); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
	}
	return nil
}
