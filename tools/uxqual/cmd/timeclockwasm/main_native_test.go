//go:build !js || !wasm

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNativeUnsupportedExplainsWasmBuild(t *testing.T) {
	var stderr bytes.Buffer
	if got := nativeUnsupported(&stderr); got != 2 {
		t.Fatalf("nativeUnsupported() exit code = %d, want 2", got)
	}
	want := nativeUnsupportedMessage + "\n"
	if got := stderr.String(); got != want {
		t.Fatalf("nativeUnsupported() stderr = %q, want %q", got, want)
	}
}

func TestNativeInvocationExitsWithWasmBuildGuidance(t *testing.T) {
	const helperEnv = "HCM_TIMECLOCK_NATIVE_HELPER"
	if os.Getenv(helperEnv) == "1" {
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeInvocationExitsWithWasmBuildGuidance$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("native command error = %v, want exit status 2; output: %s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != nativeUnsupportedMessage {
		t.Fatalf("native command stderr = %q, want %q", got, nativeUnsupportedMessage)
	}
}
