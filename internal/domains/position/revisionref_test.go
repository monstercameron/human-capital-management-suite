package position_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func revRefTestPosition(t *testing.T) values.EntityRef {
	t.Helper()
	return values.EntityRef{Tenant: values.TenantId("tenant-a"), Kind: position.KindPosition, Id: "11111111-1111-4111-8111-111111111111"}
}

func revRefTestRevision(t *testing.T) values.RevisionToken {
	t.Helper()
	rev, err := values.NewSequenceRevision("job_position", 7)
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	return rev
}

// TestRevisionRefRoundTrips proves EncodeRevisionRef and Decode are inverse:
// the exact position and revision a picker discloses are the exact pair a
// proposal's submitted reference resolves back to.
func TestRevisionRefRoundTrips(t *testing.T) {
	pos := revRefTestPosition(t)
	rev := revRefTestRevision(t)
	ref, err := position.EncodeRevisionRef(pos, rev)
	if err != nil {
		t.Fatalf("EncodeRevisionRef: %v", err)
	}
	if ref == "" {
		t.Fatal("EncodeRevisionRef returned an empty reference")
	}
	gotPos, gotRev, err := ref.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if gotPos != pos {
		t.Fatalf("decoded position = %v, want %v", gotPos, pos)
	}
	cmp, err := gotRev.CompareInStream(rev)
	if err != nil || cmp != 0 {
		t.Fatalf("decoded revision does not compare equal to the encoded one: cmp=%d err=%v", cmp, err)
	}
}

// TestRevisionRefZeroValueFailsClosed proves the empty reference -- what an
// unset selection or a zero-valued struct field carries -- never decodes to
// any position: it is refused outright, never silently treated as "no
// revision constraint" or "the first position".
func TestRevisionRefZeroValueFailsClosed(t *testing.T) {
	var zero position.RevisionRef
	if _, _, err := zero.Decode(); !errors.Is(err, position.ErrInvalidRevisionRef) {
		t.Fatalf("Decode(zero value) = %v, want ErrInvalidRevisionRef", err)
	}
}

// TestRevisionRefGuessedIdentifierFailsClosed proves an arbitrary guessed
// identifier -- the RED clause's literal example -- is never a well-formed
// reference: it fails at decode, before any position read is even
// attempted, rather than being accepted and resolved against some default.
func TestRevisionRefGuessedIdentifierFailsClosed(t *testing.T) {
	for _, guess := range []string{
		"POS-ENG-MGR-101",
		"position:pos-eng-mgr-101",
		"pos-eng-mgr-101\x1frev-1",
		"pos-eng-mgr-101.rev-1",
	} {
		guess := guess
		t.Run(guess, func(t *testing.T) {
			if _, _, err := position.RevisionRef(guess).Decode(); !errors.Is(err, position.ErrInvalidRevisionRef) {
				t.Fatalf("Decode(%q) = %v, want ErrInvalidRevisionRef", guess, err)
			}
		})
	}
}

// TestRevisionRefIsCanonicalIntentSubjectText is the PROMOUX-015 regression: a
// proposal submits this token unchanged as its POSITION subject id, and
// internal/intent refuses a subject id carrying any control character. A
// token encoded with a control-byte separator made every picker-issued
// position unproposable, so every issued token must be printable, NFC and
// free of control characters, and must still round-trip.
func TestRevisionRefIsCanonicalIntentSubjectText(t *testing.T) {
	pos := revRefTestPosition(t)
	rev := revRefTestRevision(t)
	ref, err := position.EncodeRevisionRef(pos, rev)
	if err != nil {
		t.Fatalf("EncodeRevisionRef: %v", err)
	}
	for _, r := range ref.String() {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("issued reference %q carries the control character %U", ref, r)
		}
	}
	subject := intent.SubjectReference{Kind: "POSITION", SubjectID: ref.String(), AuthorityDomain: "POSITION"}
	if err := subject.Validate(); err != nil {
		t.Fatalf("an issued reference is not a valid intent subject id: %v", err)
	}
	gotPos, gotRev, err := ref.Decode()
	if err != nil || gotPos != pos {
		t.Fatalf("Decode = %v, %v; want the encoded position", gotPos, err)
	}
	if cmp, cmpErr := gotRev.CompareInStream(rev); cmpErr != nil || cmp != 0 {
		t.Fatalf("decoded revision differs from the encoded one: cmp=%d err=%v", cmp, cmpErr)
	}
}

// TestRevisionRefRejectsUnspecifiedRevision proves a caller cannot encode a
// reference that names a position but no actual revision: that would be a
// reference to "whatever the current revision happens to be", which is
// exactly the drift REFACTOR's binding exists to prevent.
func TestRevisionRefRejectsUnspecifiedRevision(t *testing.T) {
	pos := revRefTestPosition(t)
	if _, err := position.EncodeRevisionRef(pos, values.UnspecifiedRevision()); !errors.Is(err, position.ErrInvalidRevisionRef) {
		t.Fatalf("EncodeRevisionRef(unspecified revision) = %v, want ErrInvalidRevisionRef", err)
	}
}

// TestRevisionRefRejectsWrongKind proves the encoder and decoder both refuse
// a reference naming an entity that is not a position at all.
func TestRevisionRefRejectsWrongKind(t *testing.T) {
	notAPosition := values.EntityRef{Tenant: values.TenantId("tenant-a"), Kind: values.Kind("worker"), Id: "22222222-2222-4222-8222-222222222222"}
	if _, err := position.EncodeRevisionRef(notAPosition, revRefTestRevision(t)); !errors.Is(err, position.ErrInvalidRevisionRef) {
		t.Fatalf("EncodeRevisionRef(worker) = %v, want ErrInvalidRevisionRef", err)
	}
}
