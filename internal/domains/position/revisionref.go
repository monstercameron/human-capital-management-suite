package position

// PROMOUX-004 REFACTOR: "the browser carries only the selected position
// revision reference; validation and reservation remain owned by Position."
//
// RevisionRef is that one reference: a position identity bound to the exact
// revision an authorized picker disclosed it as, encoded as a single opaque
// token. It is deliberately not two independent fields (a position id and a
// revision) that a caller could submit separately and have recombined after
// the fact -- encoding them together is what makes "which position" and
// "which revision of it" travel as one atomic fact from picker to server,
// so a client can never pair a real position id with an unrelated or
// fabricated revision.
//
// A RevisionRef says nothing on its own about whether the position it names
// exists, is vacant, is compatible with a proposal, is effective at any
// particular date, or is free to reserve: every one of those remains the
// Position domain's own re-derived answer (CheckCompatibility,
// CalculateCapacity, and the reservation-ownership port), never inferred
// from the reference alone.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrInvalidRevisionRef is returned when a reference cannot be decoded into
// a well-formed position and revision, including the zero value and a
// caller-guessed token that was never issued by a picker.
var ErrInvalidRevisionRef = errors.New("position: invalid revision reference")

// revisionRefSeparator joins the two encoded fields. Both fields are
// base64url-encoded without padding, whose alphabet is A-Z a-z 0-9 '-' '_',
// so '.' can never appear inside a segment and the split is unambiguous.
//
// PROMOUX-015: it was previously the control byte 0x1F. A proposal submits
// this token as its POSITION subject id, and intent subject ids must be
// canonical text with no control characters (internal/intent
// requireCanonicalText), so every picker-issued reference was refused as a
// malformed request and no real position could ever be proposed.
const revisionRefSeparator = "."

// RevisionRef is the opaque wire token a picker discloses for one candidate
// and a promotion proposal later submits back unchanged. Its zero value
// ("") is deliberately invalid -- see [RevisionRef.Decode] -- so an unset
// selection always fails closed rather than resolving to some default
// position.
type RevisionRef string

// EncodeRevisionRef binds pos and rev into one opaque reference. Both must
// be valid and rev must be a specified (non-zero) revision: a reference to
// "whatever the current revision happens to be" would defeat the whole
// point of binding a proposal to the exact revision it was shown.
func EncodeRevisionRef(pos values.EntityRef, rev values.RevisionToken) (RevisionRef, error) {
	if err := pos.Validate(); err != nil {
		return "", fmt.Errorf("%w: position: %w", ErrInvalidRevisionRef, err)
	}
	if pos.Kind != KindPosition {
		return "", fmt.Errorf("%w: subject kind is %q, want %q", ErrInvalidRevisionRef, pos.Kind, KindPosition)
	}
	if !rev.IsSpecified() {
		return "", fmt.Errorf("%w: revision is unspecified", ErrInvalidRevisionRef)
	}
	posText, err := pos.MarshalText()
	if err != nil {
		return "", fmt.Errorf("%w: position: %w", ErrInvalidRevisionRef, err)
	}
	revText, err := rev.MarshalText()
	if err != nil {
		return "", fmt.Errorf("%w: revision: %w", ErrInvalidRevisionRef, err)
	}
	// The two canonical texts are base64url-encoded individually before
	// joining: [values.RevisionToken.Canonical] is not itself guaranteed
	// free of the separator byte for every future selector this type gains,
	// and encoding both sides the same way means this format never has to
	// be revisited if that changes.
	return RevisionRef(encodeSegment(posText) + revisionRefSeparator + encodeSegment(revText)), nil
}

// Decode reverses [EncodeRevisionRef]. Every failure -- the zero value, a
// malformed token, a guessed string that was never issued by a picker, or a
// well-formed token whose fields fail their own validation -- returns
// [ErrInvalidRevisionRef] and nothing else: a caller must not be able to
// distinguish "not our encoding" from "your position field looks
// suspicious" from the error alone, because that distinction itself would
// be a hint about what a valid reference looks like.
func (r RevisionRef) Decode() (values.EntityRef, values.RevisionToken, error) {
	posPart, revPart, ok := strings.Cut(string(r), revisionRefSeparator)
	if !ok || posPart == "" || revPart == "" {
		return values.EntityRef{}, values.RevisionToken{}, fmt.Errorf("%w: malformed reference", ErrInvalidRevisionRef)
	}
	posText, err := decodeSegment(posPart)
	if err != nil {
		return values.EntityRef{}, values.RevisionToken{}, fmt.Errorf("%w: malformed position segment", ErrInvalidRevisionRef)
	}
	revText, err := decodeSegment(revPart)
	if err != nil {
		return values.EntityRef{}, values.RevisionToken{}, fmt.Errorf("%w: malformed revision segment", ErrInvalidRevisionRef)
	}
	var pos values.EntityRef
	if err := pos.UnmarshalText(posText); err != nil {
		return values.EntityRef{}, values.RevisionToken{}, fmt.Errorf("%w: position: %w", ErrInvalidRevisionRef, err)
	}
	if pos.Kind != KindPosition {
		return values.EntityRef{}, values.RevisionToken{}, fmt.Errorf("%w: subject kind is %q, want %q", ErrInvalidRevisionRef, pos.Kind, KindPosition)
	}
	var rev values.RevisionToken
	if err := rev.UnmarshalText(revText); err != nil {
		return values.EntityRef{}, values.RevisionToken{}, fmt.Errorf("%w: revision: %w", ErrInvalidRevisionRef, err)
	}
	if !rev.IsSpecified() {
		return values.EntityRef{}, values.RevisionToken{}, fmt.Errorf("%w: revision is unspecified", ErrInvalidRevisionRef)
	}
	return pos, rev, nil
}

// String returns the reference as wire text, exactly what a picker option's
// value attribute and a proposal's submitted field both carry.
func (r RevisionRef) String() string { return string(r) }

func encodeSegment(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func decodeSegment(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
