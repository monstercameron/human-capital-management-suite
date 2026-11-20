package pgtest

import (
	"os"
	"time"
)

// Issue #98. PostgreSQL creates its temporary filesets (parallel hash joins,
// sorts that spill) as base/pgsql_tmp/pgsql_tmpNNNN.N.fileset/... inside the
// cluster. On Windows a path longer than MAX_PATH (260) makes that mkdir fail
// with ENOENT, which PostgreSQL reports as SQLSTATE 58P01 "cannot create
// temporary subdirectory". A test run whose TMP already points deep inside a
// checkout (or a candidate directory) pushes the cluster past the limit.
//
// The runtime directory therefore moves to a short root on Windows when the
// system temp directory is so long that a fileset path would approach MAX_PATH.

// runtimeNameBudget is the longest part of a cluster path below the runtime
// parent: the runtime directory name plus data\base\pgsql_tmp\ and a fileset
// directory, with room for the files PostgreSQL puts inside it.
const runtimeNameBudget = 100

// runtimePathLimit is the longest runtime parent plus budget we accept before
// moving the cluster, leaving margin under the 260 character MAX_PATH.
const runtimePathLimit = 200

// shortRuntimeDirName is the folder created at the volume root as the short
// parent of runtime directories.
const shortRuntimeDirName = "hcmnext-pg"

// runtimeBase returns the directory under which the per-process runtime
// directory is created. It is tempDir except on Windows when tempDir is long;
// then it is a short directory at the volume root.
func runtimeBase(goos, tempDir string) string {
	if goos != "windows" || len(tempDir)+runtimeNameBudget <= runtimePathLimit {
		return tempDir
	}
	// Parsed by hand, not with filepath.VolumeName, so the decision does not
	// depend on the host the code is compiled for. Only a drive letter (not a
	// UNC share) names a root we can create the short folder in.
	if len(tempDir) < 2 || tempDir[1] != ':' {
		return tempDir
	}
	return tempDir[:2] + `\` + shortRuntimeDirName
}

// createRuntimeParentDir creates the runtime directory under runtimeBase, falling back
// to the system temp directory when the short root cannot be created.
func createRuntimeParentDir(goos string) (string, error) {
	tmp := os.TempDir()
	base := runtimeBase(goos, tmp)
	if base != tmp {
		if err := os.MkdirAll(base, 0o755); err == nil {
			sweepStaleRuntimes(base, staleRuntimeAge, time.Now())
			if dir, err := os.MkdirTemp(base, runtimePrefix); err == nil {
				return dir, nil
			}
		}
	}
	return os.MkdirTemp("", runtimePrefix)
}
