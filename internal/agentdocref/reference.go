// Package agentdocref defines the shared, authority-free document reference value.
package agentdocref

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type VersionMode string

const (
	ModePinned           VersionMode = "PINNED"
	ModeLatestPublished  VersionMode = "LATEST_PUBLISHED"
	MaxPersonaReferences             = 8
	MaxRequestReferences             = 5
	MaxContentCharacters             = 48000
	MaxLabelRunes                    = 120
)

type Reference struct {
	DocumentID    string      `json:"document_id"`
	VersionMode   VersionMode `json:"version_mode"`
	PinnedVersion uint64      `json:"pinned_version,omitempty"`
	SectionAnchor string      `json:"section_anchor,omitempty"`
	Label         string      `json:"label"`
}

var (
	ErrReferenceLimit          = errors.New("agent document reference limit exceeded")
	ErrInvalidDocumentID       = errors.New("invalid agent document reference document id")
	ErrInvalidVersionMode      = errors.New("invalid agent document reference version mode")
	ErrPinnedVersionRequired   = errors.New("agent document reference pinned version required")
	ErrUnexpectedPinnedVersion = errors.New("agent document reference pinned version is only valid in PINNED mode")
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
		if ref.VersionMode != ModePinned && ref.VersionMode != ModeLatestPublished {
			return ErrInvalidVersionMode
		}
		if ref.VersionMode == ModePinned && ref.PinnedVersion == 0 {
			return ErrPinnedVersionRequired
		}
		if ref.VersionMode == ModeLatestPublished && ref.PinnedVersion != 0 {
			return ErrUnexpectedPinnedVersion
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
