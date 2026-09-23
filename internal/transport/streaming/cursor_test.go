package streaming_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
)

// testKey is 32 bytes, the floor [streaming.NewSigner] accepts. It never
// authenticates anything outside this package's own tests.
var testKey = []byte("streaming-test-signing-key-32byt")

func mustSigner(t *testing.T, key []byte) streaming.Signer {
	t.Helper()
	s, err := streaming.NewSigner(key)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

func TestNewSignerRefusesAShortKey(t *testing.T) {
	if _, err := streaming.NewSigner([]byte("too-short")); err != streaming.ErrKeyTooShort {
		t.Fatalf("NewSigner with a short key = %v, want ErrKeyTooShort", err)
	}
}

// rotationPreviousKey is the retired key the rotation test mints under. It
// never authenticates anything outside this package's own tests.
var rotationPreviousKey = []byte("streaming-previous-key-32bytes!!")

// TestSignerRotatesWithoutBreakingInflightCursors is INTAPI-006's rotation
// proof for stream cursors: the retired key still verifies while a rotation
// is in progress, minting always uses the active key, and a short retired
// key is refused rather than silently accepted.
func TestSignerRotatesWithoutBreakingInflightCursors(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cursor := streaming.Cursor{Tenant: "tenant-a", StreamID: "journey/intent-1", Sequence: 7, ExpiresAt: now.Add(5 * time.Minute)}

	oldSigner := mustSigner(t, rotationPreviousKey)
	preRotation, err := oldSigner.Encode(cursor)
	if err != nil {
		t.Fatalf("Encode under the retired key: %v", err)
	}
	rotated, err := streaming.NewSignerWithPrevious(testKey, rotationPreviousKey)
	if err != nil {
		t.Fatalf("NewSignerWithPrevious: %v", err)
	}
	if _, err := rotated.Decode(preRotation, now, cursor.Tenant, cursor.StreamID); err != nil {
		t.Fatalf("retired-key cursor after rotation: %v", err)
	}
	postRotation, err := rotated.Encode(cursor)
	if err != nil {
		t.Fatalf("Encode under the active key: %v", err)
	}
	if _, err := rotated.Decode(postRotation, now, cursor.Tenant, cursor.StreamID); err != nil {
		t.Fatalf("active-key cursor: %v", err)
	}
	// Without the retired key the old cursor fails closed, and the new
	// cursor never verified under the retired key alone.
	fresh, err := streaming.NewSigner(testKey)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if _, err := fresh.Decode(preRotation, now, cursor.Tenant, cursor.StreamID); !errors.Is(err, streaming.ErrCursorForged) {
		t.Fatalf("retired-key cursor without the retired key = %v, want forged", err)
	}
	previousOnly, err := streaming.NewSigner(rotationPreviousKey)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if _, err := previousOnly.Decode(postRotation, now, cursor.Tenant, cursor.StreamID); !errors.Is(err, streaming.ErrCursorForged) {
		t.Fatalf("active-key cursor under the retired key = %v, want forged", err)
	}
	if _, err := streaming.NewSignerWithPrevious(testKey, []byte("too-short")); !errors.Is(err, streaming.ErrKeyTooShort) {
		t.Fatalf("short retired key = %v, want ErrKeyTooShort", err)
	}
}

// TestCursorRoundTripsThroughEncodeDecode is the cursor's ordinary path: what
// a signer encodes, the same signer decodes back unchanged, for the tenant
// and stream it was minted for.
func TestCursorRoundTripsThroughEncodeDecode(t *testing.T) {
	signer := mustSigner(t, testKey)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	want := streaming.Cursor{
		Tenant:    "tenant-a",
		StreamID:  "journey/intent-1",
		Sequence:  42,
		ExpiresAt: now.Add(5 * time.Minute),
	}
	token, err := signer.Encode(want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if token == "" {
		t.Fatal("Encode returned an empty token")
	}
	got, err := signer.Decode(token, now, want.Tenant, want.StreamID)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Tenant != want.Tenant || got.StreamID != want.StreamID || got.Sequence != want.Sequence {
		t.Fatalf("Decode = %+v, want %+v", got, want)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
}

// TestEncodeRefusesAnUnscopedCursor pins that a cursor naming no tenant or no
// stream id is refused at mint time rather than producing a token nothing
// can ever be checked against.
func TestEncodeRefusesAnUnscopedCursor(t *testing.T) {
	signer := mustSigner(t, testKey)
	cases := []streaming.Cursor{
		{StreamID: "s"},
		{Tenant: "t"},
	}
	for _, c := range cases {
		if _, err := signer.Encode(c); err == nil {
			t.Fatalf("Encode(%+v) succeeded, want a refusal", c)
		}
	}
}

// TestDecodeIsOpaqueToTenantAndStreamValuesContainingDelimiters proves the
// base64url encoding of tenant and stream id inside the token body: a value
// containing the field delimiter itself round-trips exactly, which a naive
// string-join encoding would not survive.
func TestDecodeIsOpaqueToTenantAndStreamValuesContainingDelimiters(t *testing.T) {
	signer := mustSigner(t, testKey)
	now := time.Unix(0, 0)
	c := streaming.Cursor{
		Tenant:    "tenant.with.dots",
		StreamID:  "journey/intent.42",
		Sequence:  1,
		ExpiresAt: now.Add(time.Hour),
	}
	token, err := signer.Encode(c)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := signer.Decode(token, now, c.Tenant, c.StreamID)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Tenant != c.Tenant || got.StreamID != c.StreamID {
		t.Fatalf("Decode = %+v, want tenant=%q stream=%q", got, c.Tenant, c.StreamID)
	}
}

// TestTodo_PROTO_007_Golden pins the exact wire encoding of a cursor token
// for fixed inputs. It exists so a future change to the field order, the
// delimiter, the base64 alphabet or the signature encoding is caught as a
// deliberate, reviewed format bump rather than a silent drift that still
// happens to round-trip inside this package's own tests.
func TestTodo_PROTO_007_Golden(t *testing.T) {
	signer := mustSigner(t, testKey)
	c := streaming.Cursor{
		Tenant:    "tenant-golden",
		StreamID:  "journey/intent-golden",
		Sequence:  7,
		ExpiresAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
	token, err := signer.Encode(c)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	const want = "1.dGVuYW50LWdvbGRlbg.am91cm5leS9pbnRlbnQtZ29sZGVu.7.1788609600.OSox_9RmlJkK4nvmsNLjmqKoheObIrHhprkyZWWt0NM"
	if token != want {
		t.Fatalf("cursor token drifted from the pinned encoding:\n got:  %s\n want: %s", token, want)
	}
	// The pinned token must still decode correctly - the golden value is not
	// merely opaque bytes to compare, it is a live cursor.
	got, err := signer.Decode(token, c.ExpiresAt.Add(-time.Second), c.Tenant, c.StreamID)
	if err != nil {
		t.Fatalf("the pinned token failed to decode: %v", err)
	}
	if got != c {
		t.Fatalf("decoded pinned token = %+v, want %+v", got, c)
	}
}

// TestTodo_PROTO_007_Security exercises the refusal half of the cursor
// contract in isolation from any stream: a forged signature, an expired
// cursor and a cursor naming a foreign tenant or stream are each refused
// with their own sentinel, and none of them yields a usable [Cursor].
func TestTodo_PROTO_007_Security(t *testing.T) {
	signer := mustSigner(t, testKey)
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	base := streaming.Cursor{Tenant: "tenant-a", StreamID: "stream-1", Sequence: 3, ExpiresAt: now.Add(time.Hour)}
	token, err := signer.Encode(base)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	t.Run("forged signature is refused", func(t *testing.T) {
		tampered := token[:len(token)-1]
		if strings.HasSuffix(token, "A") {
			tampered += "B"
		} else {
			tampered += "A"
		}
		if _, err := signer.Decode(tampered, now, base.Tenant, base.StreamID); err != streaming.ErrCursorForged {
			t.Fatalf("Decode(tampered) = %v, want ErrCursorForged", err)
		}
	})

	t.Run("a token minted under a different key is refused", func(t *testing.T) {
		other := mustSigner(t, []byte("a-completely-different-32-b-key!"))
		foreignToken, err := other.Encode(base)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if _, err := signer.Decode(foreignToken, now, base.Tenant, base.StreamID); err != streaming.ErrCursorForged {
			t.Fatalf("Decode(cross-key token) = %v, want ErrCursorForged", err)
		}
	})

	t.Run("an expired cursor is refused", func(t *testing.T) {
		expired := base
		expired.ExpiresAt = now.Add(-time.Second)
		expiredToken, err := signer.Encode(expired)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if _, err := signer.Decode(expiredToken, now, base.Tenant, base.StreamID); err != streaming.ErrCursorExpired {
			t.Fatalf("Decode(expired) = %v, want ErrCursorExpired", err)
		}
		// A cursor expiring exactly at now is also refused: ExpiresAt is an
		// exclusive bound, not an inclusive one.
		atNow := base
		atNow.ExpiresAt = now
		atNowToken, _ := signer.Encode(atNow)
		if _, err := signer.Decode(atNowToken, now, base.Tenant, base.StreamID); err != streaming.ErrCursorExpired {
			t.Fatalf("Decode(expires-at-now) = %v, want ErrCursorExpired", err)
		}
	})

	t.Run("a foreign tenant is refused", func(t *testing.T) {
		if _, err := signer.Decode(token, now, "someone-elses-tenant", base.StreamID); err != streaming.ErrCursorForeign {
			t.Fatalf("Decode(foreign tenant) = %v, want ErrCursorForeign", err)
		}
	})

	t.Run("a foreign stream id is refused", func(t *testing.T) {
		if _, err := signer.Decode(token, now, base.Tenant, "someone-elses-stream"); err != streaming.ErrCursorForeign {
			t.Fatalf("Decode(foreign stream) = %v, want ErrCursorForeign", err)
		}
	})

	t.Run("a malformed token is refused, not panicked on", func(t *testing.T) {
		for _, bad := range []string{"", "not-a-cursor", "1.2.3", strings.Repeat("a.", 50) + "z"} {
			if _, err := signer.Decode(bad, now, base.Tenant, base.StreamID); err == nil {
				t.Fatalf("Decode(%q) succeeded, want a refusal", bad)
			}
		}
	})

	t.Run("revocation terminates with the typed sentinel", func(t *testing.T) {
		revoked := streaming.Terminate(streaming.ErrCursorForeign)
		if revoked == nil {
			t.Fatal("Terminate(non-nil) returned nil")
		}
		if !errors.Is(revoked, streaming.ErrRevoked) {
			t.Fatalf("Terminate result %v does not wrap ErrRevoked", revoked)
		}
		if streaming.Terminate(nil) != nil {
			t.Fatal("Terminate(nil) must report \"keep going\" by returning nil")
		}
	})
}

// FuzzTodo_PROTO_007 fuzzes the cursor parser: [Signer.Decode] must never
// panic on arbitrary input, and whenever it does report success the
// returned [Cursor] must actually match what the caller asked to verify
// against - a parser that panics or that hands back a cursor for the wrong
// tenant would be exploitable by anyone who can present an arbitrary string
// as a resume cursor, which every caller of a resumable stream can.
func FuzzTodo_PROTO_007(f *testing.F) {
	signer, err := streaming.NewSigner(testKey)
	if err != nil {
		f.Fatalf("NewSigner: %v", err)
	}
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	valid, _ := signer.Encode(streaming.Cursor{
		Tenant: "tenant-a", StreamID: "stream-1", Sequence: 1, ExpiresAt: now.Add(time.Hour),
	})
	seeds := []string{
		"", "not-a-cursor", "1.2.3", valid,
		valid[:len(valid)-1], valid + "x", "1..." + valid,
		"0." + valid, strings.Repeat(".", 10),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, token string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Decode(%q) panicked: %v", token, r)
			}
		}()
		c, err := signer.Decode(token, now, "tenant-a", "stream-1")
		if err != nil {
			return
		}
		if c.Tenant != "tenant-a" || c.StreamID != "stream-1" {
			t.Fatalf("Decode(%q) succeeded with a cursor for the wrong tenant/stream: %+v", token, c)
		}
		if !now.Before(c.ExpiresAt) {
			t.Fatalf("Decode(%q) succeeded with an already-expired cursor: %+v", token, c)
		}
	})
}
