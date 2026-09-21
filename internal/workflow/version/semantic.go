package version

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

var semanticVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

// ValidateSemanticVersion accepts canonical SemVer 2.0.0 values without the
// Go module convention's leading "v". Workflow version identities therefore
// look like "1.4.0", optionally followed by prerelease or build metadata.
func ValidateSemanticVersion(value string) error {
	if value == "" {
		return fmt.Errorf("semantic version is empty")
	}
	if value != strings.TrimSpace(value) || strings.HasPrefix(value, "v") || !semanticVersionPattern.MatchString(value) || !semver.IsValid("v"+value) {
		return fmt.Errorf("%q is not a canonical semantic version", value)
	}
	return nil
}

// CompareSemanticVersions reports -1, 0, or 1 according to SemVer precedence.
// Build metadata is deliberately ignored, as required by SemVer 2.0.0.
func CompareSemanticVersions(left, right string) (int, error) {
	if err := ValidateSemanticVersion(left); err != nil {
		return 0, err
	}
	if err := ValidateSemanticVersion(right); err != nil {
		return 0, err
	}
	return semver.Compare("v"+left, "v"+right), nil
}

// NextPatchVersion returns the next stable patch version. Prerelease and build
// metadata are discarded because the result names a new release line.
func NextPatchVersion(value string) (string, error) {
	if err := ValidateSemanticVersion(value); err != nil {
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
