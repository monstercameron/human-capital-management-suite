// Package semver is the one semantic-version value shared by documents,
// agents and gates (AGENTUX-065): documents label their versions with it at
// publication, agents seal it on publication, and gates parse and compare
// with it. Precedence follows SemVer 2.0.0 exactly; labels are plain
// MAJOR.MINOR.PATCH triples starting at 1.0.0, with no prerelease.
package semver

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalid is a version that is not a plain MAJOR.MINOR.PATCH triple.
var ErrInvalid = errors.New("semver: invalid version")

// Version is one semantic version: three plain numbers, major at least one.
// The zero value is not a version; it is "no label yet".
type Version struct{ Major, Minor, Patch uint32 }

// Bump is how much changed: the author's plain choice at publication.
type Bump int

const (
	// BumpPatch is "Fixes wording".
	BumpPatch Bump = iota
	// BumpMinor is "Adds or clarifies".
	BumpMinor
	// BumpMajor is "Changes what it says or does".
	BumpMajor
)

// FirstVersion is the label of a first publication.
func FirstVersion() Version { return Version{Major: 1} }

// Parse reads a plain triple, with or without the display's leading "v".
// Prerelease and build metadata are refused: labels are releases.
func Parse(s string) (Version, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	var n [3]uint32
	for i, part := range parts {
		u, err := strconv.ParseUint(part, 10, 32)
		if err != nil || strconv.FormatUint(u, 10) != part {
			return Version{}, fmt.Errorf("%w: %q", ErrInvalid, s)
		}
		n[i] = uint32(u)
	}
	if n[0] == 0 {
		return Version{}, fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	return Version{n[0], n[1], n[2]}, nil
}

// String renders the plain triple.
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Display renders the label every surface shows.
func (v Version) Display() string { return "v" + v.String() }

// DisplaySequence renders the label for a legacy sequence with no stored
// label: sequence n reads vN.0.0, the migration's documented rule.
func DisplaySequence(sequence uint64) string { return "v" + strconv.FormatUint(sequence, 10) + ".0.0" }

// Compare reports -1, 0, or 1 by SemVer precedence.
func (v Version) Compare(w Version) int {
	if v.Major != w.Major {
		if v.Major < w.Major {
			return -1
		}
		return 1
	}
	if v.Minor != w.Minor {
		if v.Minor < w.Minor {
			return -1
		}
		return 1
	}
	switch {
	case v.Patch < w.Patch:
		return -1
	case v.Patch > w.Patch:
		return 1
	default:
		return 0
	}
}

// Less is whether v precedes w.
func (v Version) Less(w Version) bool { return v.Compare(w) < 0 }

// SatisfiesCaret is whether v follows base compatibly: the caret rule for
// labels, which never have a zero major, is the same major and no older.
func (v Version) SatisfiesCaret(base Version) bool {
	return v.Major == base.Major && v.Compare(base) >= 0
}

// Next computes the label a bump choice names from v. From no label it is the
// first version whatever the choice: a first publication is 1.0.0.
func (v Version) Next(bump Bump) Version {
	if v == (Version{}) {
		return FirstVersion()
	}
	switch bump {
	case BumpMajor:
		if v.Major == ^uint32(0) {
			return Version{^uint32(0), ^uint32(0), ^uint32(0)}
		}
		return Version{v.Major + 1, 0, 0}
	case BumpMinor:
		if v.Minor == ^uint32(0) {
			if v.Major == ^uint32(0) {
				return Version{^uint32(0), ^uint32(0), ^uint32(0)}
			}
			return Version{v.Major + 1, 0, 0}
		}
		return Version{v.Major, v.Minor + 1, 0}
	default:
		if v.Patch == ^uint32(0) {
			if v.Minor < ^uint32(0) {
				return Version{v.Major, v.Minor + 1, 0}
			}
			if v.Major < ^uint32(0) {
				return Version{v.Major + 1, 0, 0}
			}
			return v
		}
		return Version{v.Major, v.Minor, v.Patch + 1}
	}
}
