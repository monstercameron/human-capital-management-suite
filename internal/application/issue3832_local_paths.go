package application

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// EnvLocalAgentDevDir names the directory that holds the local development
// agent files (qualified deployments, signing material, evaluation key, support
// objects). Both the preparation command and a served local-dev process read
// it, so they agree no matter which directory each was started from.
const EnvLocalAgentDevDir = "HCMNEXT_LOCAL_AGENT_DEV_DIR"

// EnvLocalAgentModelSigningFile names the local persona signing material file
// by itself, for a person who keeps the seeds somewhere other than the
// directory above.
const EnvLocalAgentModelSigningFile = "HCMNEXT_LOCAL_AGENT_MODEL_SIGNING_FILE"

// localAgentDevFile resolves one of the local-dev file constants to an absolute
// path: inside EnvLocalAgentDevDir when it is set, else beside the working
// directory as before. The result is always absolute so a log line names the
// file that was actually searched. Callers use it only in the local-dev
// profile, so production never reads seeds from a configurable path.
func localAgentDevFile(constPath string) string {
	if dir := strings.TrimSpace(os.Getenv(EnvLocalAgentDevDir)); dir != "" {
		return absLocalAgentPath(filepath.Join(dir, path.Base(constPath)))
	}
	return absLocalAgentPath(filepath.FromSlash(constPath))
}

// localAgentDevRoot is the directory form of localAgentDevFile.
func localAgentDevRoot() string {
	return filepath.Dir(localAgentDevFile(localPersonaModelDeploymentPath))
}

// localPersonaSigningFile is the signing material path, honouring its own
// override first.
func localPersonaSigningFile() string {
	if file := strings.TrimSpace(os.Getenv(EnvLocalAgentModelSigningFile)); file != "" {
		return absLocalAgentPath(file)
	}
	return localAgentDevFile(localPersonaModelSigningPath)
}

func absLocalAgentPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
