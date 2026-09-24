package openapi

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrDrift reports that a checked-in document differs from a fresh
// generation.
var ErrDrift = errors.New("openapi: document is stale")

// EmbeddedOutputPath is the generated copy embedded by the serving binary.
const EmbeddedOutputPath = "internal/transport/openapidoc/rpcs.openapi.yaml"

// Write generates the document and writes it to outPath (relative paths are
// resolved against repoRoot).
func Write(repoRoot, outPath string) error {
	data, err := Generate(repoRoot)
	if err != nil {
		return err
	}
	target := resolve(repoRoot, outPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return err
	}
	if outPath == DefaultOutputPath {
		if err := os.WriteFile(resolve(repoRoot, EmbeddedOutputPath), data, 0o644); err != nil {
			return fmt.Errorf("openapi: write embedded publication: %w", err)
		}
	}
	return nil
}

// Check returns ErrDrift when the document at outPath differs from a fresh
// generation. CRLF line endings in the checked-in file are ignored.
func Check(repoRoot, outPath string) error {
	want, err := Generate(repoRoot)
	if err != nil {
		return err
	}
	got, err := os.ReadFile(resolve(repoRoot, outPath))
	if err != nil {
		return fmt.Errorf("%w: %v; regenerate with `%s`", ErrDrift, err, RegenerateCommand)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		return fmt.Errorf("%w: %s differs from a fresh generation; regenerate with `%s`", ErrDrift, outPath, RegenerateCommand)
	}
	if outPath == DefaultOutputPath {
		embedded, err := os.ReadFile(resolve(repoRoot, EmbeddedOutputPath))
		if err != nil || !bytes.Equal(bytes.ReplaceAll(embedded, []byte("\r\n"), []byte("\n")), want) {
			return fmt.Errorf("%w: %s differs from a fresh generation; regenerate with `%s`", ErrDrift, EmbeddedOutputPath, RegenerateCommand)
		}
	}
	return nil
}

func resolve(repoRoot, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(repoRoot, filepath.FromSlash(p))
}
