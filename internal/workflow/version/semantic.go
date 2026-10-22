package version

import "github.com/monstercameron/human-capital-management-suite/internal/workflow/semanticversion"

// ValidateSemanticVersion accepts canonical SemVer 2.0.0 values without the Go
// module convention's leading "v". Workflow version identities therefore look
// like "1.4.0", optionally followed by prerelease or build metadata. The rules
// live in internal/workflow/semanticversion so the browser client can use them
// without importing this package's stores.
func ValidateSemanticVersion(value string) error { return semanticversion.Validate(value) }

// CompareSemanticVersions reports -1, 0, or 1 according to SemVer precedence.
// Build metadata is deliberately ignored, as required by SemVer 2.0.0.
func CompareSemanticVersions(left, right string) (int, error) {
	return semanticversion.Compare(left, right)
}

// NextPatchVersion returns the next stable patch version. Prerelease and build
// metadata are discarded because the result names a new release line.
func NextPatchVersion(value string) (string, error) { return semanticversion.NextPatch(value) }
