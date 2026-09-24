package ledger_test

import (
	"encoding/hex"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// FuzzTodo_DATA_003 checks the canonical digest contract over arbitrary schema
// references and payload bytes: deterministic output, exact length, a valid
// sha256 digest, and sensitivity to either component of the preimage.
func FuzzTodo_DATA_003(f *testing.F) {
	f.Add([]byte{}, "schema@1")
	f.Add([]byte{0, 1, 0xff}, "hcmnext.intents.v1.BusinessIntent@1")
	f.Add([]byte("payload"), "schema-with-separators:/@2")

	f.Fuzz(func(t *testing.T, payload []byte, schemaRef string) {
		if len(payload) > ledger.MaxInlinePayloadBytes*2 || len(schemaRef) > 4096 {
			t.Skip()
		}
		d := ledger.SHA256Digester{}
		algorithm, digest, length, err := d.Digest(payload, schemaRef)
		if schemaRef == "" {
			if err == nil {
				t.Fatal("empty schema reference was accepted")
			}
			return
		}
		if err != nil {
			t.Fatalf("digest valid schema reference: %v", err)
		}
		if algorithm != ledger.Algorithm || length != len(payload) {
			t.Fatalf("digest metadata is %q/%d, want %q/%d", algorithm, length, ledger.Algorithm, len(payload))
		}
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != 32 {
			t.Fatalf("digest %q is not a 32-byte hex sha256 value (decode error %v)", digest, err)
		}
		_, repeated, repeatedLength, err := d.Digest(payload, schemaRef)
		if err != nil || repeated != digest || repeatedLength != length {
			t.Fatalf("same preimage produced %q/%d with error %v, want %q/%d", repeated, repeatedLength, err, digest, length)
		}

		changedPayload := append(append([]byte(nil), payload...), 0)
		_, payloadDigest, _, err := d.Digest(changedPayload, schemaRef)
		if err != nil {
			t.Fatalf("digest changed payload: %v", err)
		}
		if payloadDigest == digest {
			t.Fatal("changing payload bytes did not change the canonical digest")
		}
		_, schemaDigest, _, err := d.Digest(payload, schemaRef+"\x00")
		if err != nil {
			t.Fatalf("digest changed schema reference: %v", err)
		}
		if schemaDigest == digest {
			t.Fatal("changing schema reference did not change the canonical digest")
		}
	})
}

// TestTodo_DATA_003_Mutation detects ambiguous framing and omitted preimage
// fields: schema/payload pairs with the same unframed concatenation must differ,
// as must a one byte payload change or schema revision change.
func TestTodo_DATA_003_Mutation(t *testing.T) {
	d := ledger.SHA256Digester{}
	base := []byte("c")
	cases := []struct {
		name      string
		payload   []byte
		schemaRef string
	}{
		{name: "different payload", payload: []byte("d"), schemaRef: "ab"},
		{name: "different schema revision", payload: base, schemaRef: "ab@2"},
		// Both concatenate to "abc" without length-prefix framing.
		{name: "ambiguous split", payload: []byte("bc"), schemaRef: "a"},
	}
	_, want, wantLength, err := d.Digest(base, "ab")
	if err != nil {
		t.Fatalf("digest baseline: %v", err)
	}
	if wantLength != len(base) {
		t.Fatalf("baseline canonical length is %d, want %d", wantLength, len(base))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got, _, err := d.Digest(tc.payload, tc.schemaRef)
			if err != nil {
				t.Fatalf("digest mutant input: %v", err)
			}
			if got == want {
				t.Fatalf("digest for schema %q and payload %q aliases the baseline", tc.schemaRef, tc.payload)
			}
		})
	}
}
