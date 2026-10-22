// Package semanticversion holds the SemVer 2.0.0 rules workflow versions follow.
// It is a leaf on purpose: the workflow designer page checks and bumps a
// version number in the browser, and importing internal/workflow/version for
// that brought the workflow engine, its stores and the transaction layer into
// the client (CHATBUG-014). internal/workflow/version keeps its own exported
// functions and delegates to these.
package semanticversion

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

var semanticVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

// Validate accepts canonical SemVer 2.0.0 values without the Go module
// convention's leading "v". Workflow version identities therefore look like
// "1.4.0", optionally followed by prerelease or build metadata.
func Validate(value string) error {
	if value == "" {
		return fmt.Errorf("semantic version is empty")
	}
	if value != strings.TrimSpace(value) || strings.HasPrefix(value, "v") || !semanticVersionPattern.MatchString(value) || !semver.IsValid("v"+value) {
		return fmt.Errorf("%q is not a canonical semantic version", value)
	}
	return nil
}

// Compare reports -1, 0, or 1 according to SemVer precedence. Build metadata
// is deliberately ignored, as required by SemVer 2.0.0.
func Compare(left, right string) (int, error) {
	if err := Validate(left); err != nil {
		return 0, err
	}
	if err := Validate(right); err != nil {
		return 0, err
	}
	return semver.Compare("v"+left, "v"+right), nil
}

// NextPatch returns the next stable patch version. Prerelease and build
// metadata are discarded because the result names a new release line.
func NextPatch(value string) (string, error) {
	if err := Validate(value); err != nil {
		return "", err
	}
	core := strings.TrimPrefix(semver.Canonical("v"+value), "v")
	if index := strings.IndexAny(core, "-+"); index >= 0 {
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("read semantic version core %q", core)
	}
	patch := []byte(parts[2])
	carry := byte(1)
	for index := len(patch) - 1; index >= 0 && carry == 1; index-- {
		if patch[index] == '9' {
			patch[index] = '0'
			continue
		}
		patch[index]++
		carry = 0
	}
	if carry == 1 {
		patch = append([]byte{'1'}, patch...)
	}
	return parts[0] + "." + parts[1] + "." + string(patch), nil
}
