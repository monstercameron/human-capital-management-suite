// Package agentdocref defines the shared, authority-free document reference value.
package agentdocref

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

type VersionMode string

const (
	ModePinned          VersionMode = "PINNED"
	ModeLatestPublished VersionMode = "LATEST_PUBLISHED"
	// ModePinnedLabel pins one exact semantic label (AGENTUX-065).
	ModePinnedLabel VersionMode = "PINNED_LABEL"
	// ModeCompatible follows compatible versions from a base label
	// (AGENTUX-065): corrections are followed, rewrites stop the line.
	ModeCompatible       VersionMode = "COMPATIBLE"
	MaxPersonaReferences             = 8
	MaxRequestReferences             = 5
	MaxContentCharacters             = 48000
	MaxLabelRunes                    = 120
)

type Reference struct {
	DocumentID    string      `json:"document_id"`
	VersionMode   VersionMode `json:"version_mode"`
	PinnedVersion uint64      `json:"pinned_version,omitempty"`
	// PinnedLabel is the exact label a PINNED_LABEL reference names.
	PinnedLabel string `json:"pinned_label,omitempty"`
	// CompatibleBase is the oldest label a COMPATIBLE reference accepts.
	CompatibleBase string `json:"compatible_base,omitempty"`
	SectionAnchor  string `json:"section_anchor,omitempty"`
	Label          string `json:"label"`
}

// VersionSelector renders which versions the reference follows: =1.2.0 for
// an exact pin, ^1 for a compatible line, #2 for a sequence pin, latest
// for the newest publication.
func (r Reference) VersionSelector() string {
	switch r.VersionMode {
	case ModePinnedLabel:
		return "=" + r.PinnedLabel
	case ModeCompatible:
		if base, err := semver.Parse(r.CompatibleBase); err == nil && base.Minor == 0 && base.Patch == 0 {
			return fmt.Sprintf("^%d", base.Major)
		}
		return "^" + r.CompatibleBase
	case ModePinned:
		return fmt.Sprintf("#%d", r.PinnedVersion)
	default:
		return "latest"
	}
}

var (
	ErrReferenceLimit          = errors.New("agent document reference limit exceeded")
	ErrInvalidDocumentID       = errors.New("invalid agent document reference document id")
	ErrInvalidVersionMode      = errors.New("invalid agent document reference version mode")
	ErrPinnedVersionRequired   = errors.New("agent document reference pinned version required")
	ErrUnexpectedPinnedVersion = errors.New("agent document reference pinned version is only valid in PINNED mode")
	ErrPinnedLabelRequired     = errors.New("agent document reference pinned label required")
	ErrCompatibleBaseRequired  = errors.New("agent document reference compatible base required")
	ErrUnexpectedVersionLabel  = errors.New("agent document reference version label is only valid in its own mode")
	ErrInvalidVersionLabel     = errors.New("agent document reference version label is not a semantic version")
	ErrInvalidSectionAnchor    = errors.New("invalid agent document reference section anchor")
	ErrEmptyLabel              = errors.New("agent document reference label is required")
	ErrLabelTooLong            = errors.New("agent document reference label is too long")
	ErrInvalidLabelUTF8        = errors.New("agent document reference label is not valid UTF-8")
	ErrLabelControl            = errors.New("agent document reference label contains a control character")
	ErrLabelURL                = errors.New("agent document reference label contains a URL scheme")
	ErrDuplicateDocument       = errors.New("duplicate agent document reference")
	ErrDuplicateLabel          = errors.New("duplicate agent document reference label")
)

func Validate(refs []Reference, limit int) error {
	if limit < 0 || len(refs) > limit {
		return fmt.Errorf("%w: got %d, limit %d", ErrReferenceLimit, len(refs), limit)
	}
	ids, labels := map[string]struct{}{}, map[string]struct{}{}
	for _, ref := range refs {
		if !validDocumentID(ref.DocumentID) {
			return ErrInvalidDocumentID
		}
		if ref.VersionMode != ModePinned && ref.VersionMode != ModeLatestPublished && ref.VersionMode != ModePinnedLabel && ref.VersionMode != ModeCompatible {
			return ErrInvalidVersionMode
		}
		if ref.VersionMode == ModePinned && ref.PinnedVersion == 0 {
			return ErrPinnedVersionRequired
		}
		if ref.VersionMode != ModePinned && ref.PinnedVersion != 0 {
			return ErrUnexpectedPinnedVersion
		}
		if ref.VersionMode == ModePinnedLabel {
			if ref.PinnedLabel == "" {
				return ErrPinnedLabelRequired
			}
			if _, err := semver.Parse(ref.PinnedLabel); err != nil {
				return ErrInvalidVersionLabel
			}
		} else if ref.PinnedLabel != "" {
			return ErrUnexpectedVersionLabel
		}
		if ref.VersionMode == ModeCompatible {
			if ref.CompatibleBase == "" {
				return ErrCompatibleBaseRequired
			}
			if _, err := semver.Parse(ref.CompatibleBase); err != nil {
				return ErrInvalidVersionLabel
			}
		} else if ref.CompatibleBase != "" {
			return ErrUnexpectedVersionLabel
		}
		if ref.SectionAnchor != "" && !validSectionAnchor(ref.SectionAnchor) {
			return ErrInvalidSectionAnchor
		}
		if !utf8.ValidString(ref.Label) {
			return ErrInvalidLabelUTF8
		}
		if strings.TrimSpace(ref.Label) == "" {
			return ErrEmptyLabel
		}
		if utf8.RuneCountInString(ref.Label) > MaxLabelRunes {
			return ErrLabelTooLong
		}
		for _, r := range ref.Label {
			if unicode.IsControl(r) {
				return ErrLabelControl
			}
		}
		if strings.Contains(strings.ToLower(ref.Label), "://") {
			return ErrLabelURL
		}
		if _, ok := ids[ref.DocumentID]; ok {
			return ErrDuplicateDocument
		}
		ids[ref.DocumentID] = struct{}{}
		labelKey := strings.ToLower(strings.TrimSpace(ref.Label))
		if _, ok := labels[labelKey]; ok {
			return ErrDuplicateLabel
		}
		labels[labelKey] = struct{}{}
	}
	return nil
}

func validDocumentID(value string) bool {
	// Match documenthubstore's canonical doc: link ID component.
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validSectionAnchor(value string) bool {
	if !utf8.ValidString(value) || strings.ToLower(value) != value {
		return false
	}
	previousHyphen := true
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			previousHyphen = false
			continue
		}
		if r != '-' || previousHyphen {
			return false
		}
		previousHyphen = true
	}
	return !previousHyphen
}
