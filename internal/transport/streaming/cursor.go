// Package streaming is the transport-layer contract for a resumable,
// ordered, backpressured stream and for polling a long-running operation,
// stated once as code so every server-streaming RPC and every long-operation
// endpoint in this repository can reuse it instead of re-deriving it per
// service (PROTO-007).
//
// Semantic owner: experience-and-transport. Phase: PHASE_2.
//
// Four pieces make up the contract:
//
//   - [Signer] and [Cursor] ([cursor.go]): an opaque, tenant- and
//     stream-bound position, signed with an HMAC over tenant, stream id and
//     sequence using a key the caller supplies, and expiring. A cursor is
//     never parsed by the party that receives it back - only verified -
//     which is what "opaque" means here: nothing this package emits is a
//     wire format a client is entitled to construct or edit.
//   - [Chunk] and [OrderTracker] ([chunk.go]): the ordered unit a stream
//     sends (a sequence number, the cursor as of after it, a terminal flag)
//     and the receive-side check that a run of chunks has no gap and no
//     duplicate.
//   - [Buffer] ([buffer.go]): a fixed-capacity queue between a producer and
//     a consumer. Its capacity is the backpressure bound: a producer that
//     races ahead of a slow consumer blocks at that bound rather than
//     growing memory without limit.
//   - [Producer] ([producer.go]): ties the three together for one stream.
//     [Producer.Resume] validates a client-presented cursor and positions
//     the producer to continue the sequence rather than restart it; a
//     forged, expired or foreign cursor is refused. [Terminate] is the
//     typed status an open stream ends with when the caller's authorization
//     is revoked mid-stream, so that ending is never mistaken for the
//     resource underneath the stream having failed.
//
// [Operation] ([operation.go]) is the sibling contract for a long-running
// operation reached by polling rather than by an open stream: an id, a
// state, the same kind of opaque cursor, and a declared retry-after bound a
// poller must respect.
//
// internal/transport/journey applies the cursor half of this contract to
// hcmnext.journey.v1.JourneyService.WatchJourney additively: the response
// carries a cursor and a sequence, the request accepts a resume cursor, all
// three new, with the existing since_digest dedup left exactly as it was.
// The two resumption mechanisms are independent by design - the cursor
// authenticates and positions the stream, since_digest decides which detail
// an opening emission may skip - so a client that presents both resumes a
// numbered run without being re-sent what it already holds.
//
// WatchJourney does not need [Buffer] or [Operation] itself - its handler
// already sends synchronously on its own goroutine, so a consumer that stops
// reading stops the producer and gRPC's own flow control is the only window
// in play - which is why this package keeps the four pieces separable rather
// than folding everything into one type a caller cannot use in part. [Buffer]
// is for the other shape: a producer that computes chunks ahead of its
// consumer, which needs a declared bound because nothing else throttles it.
package streaming

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MinKeySize is the shortest signing key [NewSigner] accepts. It matches
// internal/trust's HMAC verifier floor: a key shorter than a SHA-256 block
// buys an attacker a forgery shortcut a longer key does not.
const MinKeySize = 32

// cursorFormatVersion is the first field of every encoded cursor. A future
// change to the wire format bumps it, so an old cursor is refused as
// malformed rather than misparsed under the new layout.
const cursorFormatVersion = "1"

// Sentinel errors [Signer.Decode] returns. Every one of them means the
// cursor is refused; none of them is a signal to retry with a different
// key, tenant or stream id chosen by the caller - resolving that decision
// belongs to whatever issued the cursor in the first place, never to the
// party validating it.
var (
	// ErrCursorMalformed reports a token that is not this package's wire
	// format at all: wrong shape, a field that does not parse, or a version
	// this build does not know.
	ErrCursorMalformed = errors.New("streaming: cursor is malformed")
	// ErrCursorForged reports a token whose signature does not verify under
	// the signer's key. It covers both a tampered field and a token minted
	// under a different key entirely.
	ErrCursorForged = errors.New("streaming: cursor signature does not verify")
	// ErrCursorExpired reports a token whose declared expiry is at or before
	// the validation time.
	ErrCursorExpired = errors.New("streaming: cursor has expired")
	// ErrCursorForeign reports a token that verifies and has not expired but
	// names a tenant or stream id other than the one the caller is
	// authorized for right now. A cursor minted for tenant A never resumes
	// a stream opened as tenant B, no matter how it was obtained.
	ErrCursorForeign = errors.New("streaming: cursor does not name the caller's tenant and stream")
	// ErrKeyTooShort reports a signing key shorter than [MinKeySize].
	ErrKeyTooShort = fmt.Errorf("streaming: signing key must be at least %d bytes", MinKeySize)
)

// Cursor is the decoded, opaque position in one resumable stream: which
// tenant and stream it belongs to, which sequence number it names, and when
// it stops being presentable.
//
// A Cursor is a value a caller receives from [Signer.Decode] to inspect the
// position a token names; it is never something a caller constructs by hand
// and expects [Signer.Encode] to turn into a token another party will
// accept; only whoever holds the signing key mints a cursor that verifies.
type Cursor struct {
	// Tenant is the tenant slug the cursor is scoped to.
	Tenant string
	// StreamID is the stream the cursor is scoped to, in whatever id space
	// the issuer chose (e.g. "journey/<intent-id>"). A cursor minted for one
	// stream id never resumes another, even within the same tenant.
	StreamID string
	// Sequence is the chunk sequence number this cursor names: the position
	// a resumed stream continues from is Sequence, not Sequence+1, because
	// what the cursor names is "the last chunk delivered", and Producer.Next
	// always advances before minting the next chunk's own cursor.
	Sequence uint64
	// ExpiresAt is when this cursor stops verifying, regardless of whether
	// the stream itself is still open.
	ExpiresAt time.Time
}

// Signer mints and verifies cursor tokens under caller-supplied keys. It
// holds a private copy of every key so a caller mutating a slice it passed
// to a constructor cannot change what an already-constructed Signer mints
// or verifies.
type Signer struct {
	key      []byte
	previous []byte
}

// NewSigner returns a [Signer] over key, refusing a key shorter than
// [MinKeySize].
func NewSigner(key []byte) (Signer, error) {
	return NewSignerWithPrevious(key, nil)
}

// NewSignerWithPrevious returns a [Signer] that mints under active and
// verifies under active or previous. previous is the retired key, accepted
// for verification only while in-flight cursors minted under it drain; an
// empty previous means no rotation is in progress. A previous shorter than
// [MinKeySize] is refused: nothing that short could ever have minted, so
// accepting it would only bless a misconfiguration.
func NewSignerWithPrevious(active, previous []byte) (Signer, error) {
	if len(active) < MinKeySize {
		return Signer{}, ErrKeyTooShort
	}
	if len(previous) != 0 && len(previous) < MinKeySize {
		return Signer{}, ErrKeyTooShort
	}
	return Signer{key: append([]byte(nil), active...), previous: append([]byte(nil), previous...)}, nil
}

// sign returns the HMAC-SHA256 of body under s's active key.
func (s Signer) sign(body []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(body)
	return mac.Sum(nil)
}

// verified reports whether sig is the HMAC-SHA256 of body under the active
// key or, while a rotation is in progress, the retired key.
func (s Signer) verified(body, sig []byte) bool {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(body)
	if hmac.Equal(mac.Sum(nil), sig) {
		return true
	}
	if len(s.previous) == 0 {
		return false
	}
	mac = hmac.New(sha256.New, s.previous)
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), sig)
}

// Encode returns the opaque cursor token for c. c.Tenant and c.StreamID must
// be non-empty; a cursor that does not name a tenant and a stream cannot be
// checked for either on the way back in.
func (s Signer) Encode(c Cursor) (string, error) {
	if c.Tenant == "" || c.StreamID == "" {
		return "", fmt.Errorf("%w: tenant and stream id are required", ErrCursorMalformed)
	}
	body := encodeBody(c)
	sig := s.sign([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// Decode parses token, verifies its signature under s's key, and checks that
// it has not expired and that it names exactly expectTenant and
// expectStreamID.
//
// The checks run in this order deliberately: shape and signature first (a
// token that does not verify tells an attacker nothing about why), then
// expiry, then tenant/stream identity. A caller that only wants "is this
// cursor still good for me" gets exactly one of the four sentinel errors
// back, never a partially-decoded [Cursor].
func (s Signer) Decode(token string, now time.Time, expectTenant, expectStreamID string) (Cursor, error) {
	dot := strings.LastIndexByte(token, '.')
	if dot < 0 {
		return Cursor{}, ErrCursorMalformed
	}
	body, sigPart := token[:dot], token[dot+1:]
	sig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return Cursor{}, ErrCursorMalformed
	}
	if !s.verified([]byte(body), sig) {
		return Cursor{}, ErrCursorForged
	}
	c, err := decodeBody(body)
	if err != nil {
		return Cursor{}, err
	}
	if !now.Before(c.ExpiresAt) {
		return Cursor{}, ErrCursorExpired
	}
	if c.Tenant != expectTenant || c.StreamID != expectStreamID {
		return Cursor{}, ErrCursorForeign
	}
	return c, nil
}

// encodeBody renders the signed portion of a cursor token: version, tenant,
// stream id, sequence and expiry, dot-joined. Tenant and stream id are
// base64url-encoded individually so a value containing "." or another
// delimiter cannot be mistaken for a field boundary.
func encodeBody(c Cursor) string {
	return strings.Join([]string{
		cursorFormatVersion,
		base64.RawURLEncoding.EncodeToString([]byte(c.Tenant)),
		base64.RawURLEncoding.EncodeToString([]byte(c.StreamID)),
		strconv.FormatUint(c.Sequence, 10),
		strconv.FormatInt(c.ExpiresAt.UTC().Unix(), 10),
	}, ".")
}

// decodeBody is encodeBody's inverse, returning [ErrCursorMalformed] for any
// shape or field that does not parse.
func decodeBody(body string) (Cursor, error) {
	parts := strings.Split(body, ".")
	if len(parts) != 5 || parts[0] != cursorFormatVersion {
		return Cursor{}, ErrCursorMalformed
	}
	tenant, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Cursor{}, ErrCursorMalformed
	}
	streamID, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Cursor{}, ErrCursorMalformed
	}
	seq, err := strconv.ParseUint(parts[3], 10, 64)
	if err != nil {
		return Cursor{}, ErrCursorMalformed
	}
	expUnix, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return Cursor{}, ErrCursorMalformed
	}
	return Cursor{
		Tenant:    string(tenant),
		StreamID:  string(streamID),
		Sequence:  seq,
		ExpiresAt: time.Unix(expUnix, 0).UTC(),
	}, nil
}
