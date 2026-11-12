package importdraft

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Canon builds a deterministic, self-delimiting byte stream for digesting.
// Every write is length-prefixed so that no two distinct sequences of writes
// can ever collide onto the same bytes, and every method that takes a
// collection sorts it first so that a digest never depends on slice or map
// build order - only on content.
//
// The writer lives in this leaf package so that the value types the browser
// client links (batches, drafts, checkpoints) can compute their own content
// digests without importing the import pipeline that owns the rest of the
// canonical forms. It is a tiny length-prefixed field stream rather than the
// shared canonical-bytes engine, which the client graph must not reach.
type Canon struct {
	buf []byte
}

// NewCanon starts a digest stream tagged with a schema name and version.
func NewCanon(schema string, version int) *Canon {
	w := &Canon{}
	w.Str(schema)
	w.U64(uint64(version))
	return w
}

// Str appends a length-prefixed UTF-8 string.
func (w *Canon) Str(s string) *Canon {
	var lenBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(lenBuf[:], uint64(len(s)))
	w.buf = append(w.buf, lenBuf[:n]...)
	w.buf = append(w.buf, s...)
	return w
}

// U64 appends an unsigned varint.
func (w *Canon) U64(v uint64) *Canon {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	w.buf = append(w.buf, buf[:n]...)
	return w
}

// I64 appends a signed value via zigzag encoding, so negative numbers do not
// produce a variable-width two's-complement pattern.
func (w *Canon) I64(v int64) *Canon {
	return w.U64(uint64(v<<1) ^ uint64(v>>63))
}

// Bool appends one byte, 0 or 1.
func (w *Canon) Bool(b bool) *Canon {
	if b {
		w.buf = append(w.buf, 1)
	} else {
		w.buf = append(w.buf, 0)
	}
	return w
}

// Strings appends a count followed by each string in the given order. The
// caller sorts first when order must not be significant.
func (w *Canon) Strings(ss []string) *Canon {
	w.U64(uint64(len(ss)))
	for _, s := range ss {
		w.Str(s)
	}
	return w
}

// DigestHex returns the lowercase hex sha256 of the accumulated stream,
// prefixed with the algorithm tag so a digest can never be mistaken for one
// computed under a different algorithm.
func (w *Canon) DigestHex() string {
	sum := sha256.Sum256(w.buf)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Bytes returns a copy of the accumulated stream, for callers that need the
// canonical payload itself rather than its digest.
func (w *Canon) Bytes() []byte {
	return append([]byte(nil), w.buf...)
}
