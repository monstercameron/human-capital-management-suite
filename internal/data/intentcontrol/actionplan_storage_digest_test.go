package intentcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
)

func TestActionPlanBindingUsesBareContentDigestOnlyAtStorageBoundary(t *testing.T) {
	payload := []byte(`{"tenant":"tenant-1","plan_digest":"sha256:abc"}`)
	sum := sha256.Sum256(payload)
	canonical := "sha256:" + hex.EncodeToString(sum[:])
	stored, err := bareSHA256Digest(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if stored != hex.EncodeToString(sum[:]) || "sha256:"+stored != canonical {
		t.Fatalf("storage encoding %q did not preserve canonical digest %q", stored, canonical)
	}
	if got := sha256.Sum256(payload); hex.EncodeToString(got[:]) != stored {
		t.Fatalf("stored digest %q no longer verifies canonical payload", stored)
	}
	if !idempotency.ValidDigest(stored) || idempotency.ValidDigest(canonical) {
		t.Fatalf("stored reference %q and canonical digest %q do not match the idempotency content_digest contract", stored, canonical)
	}
}

func TestActionPlanBindingStorageDigestRejectsNoncanonicalForms(t *testing.T) {
	for _, value := range []string{"", "not-a-digest", strings.Repeat("a", 64), "sha256:" + strings.Repeat("A", 64)} {
		if _, err := bareSHA256Digest(value); err == nil {
			t.Errorf("bareSHA256Digest(%q) unexpectedly succeeded", value)
		}
	}
}
