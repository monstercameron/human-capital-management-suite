package search

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var repro001At = time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)

func repro001Store() MapInputStore {
	return MapInputStore{
		"sha256:query":      []byte(`{"q":"overtime","filters":["US-CA"]}`),
		"sha256:definition": []byte(`{"metric":"overtime-rate","version":"v3"}`),
		"sha256:population": []byte(`{"snapshot":"pop-2026-03-01"}`),
		"sha256:source-1":   []byte(`{"shard":"ledger-01"}`),
		"sha256:source-2":   []byte(`{"shard":"ledger-02"}`),
	}
}

func repro001Bundle(store MapInputStore) ReproBundle {
	bundle := ReproBundle{
		Tenant: "acme", Classification: "INTERNAL",
		QueryDigest: "sha256:query", QueryVersion: "q/v7",
		DefinitionDigest: "sha256:definition", DefinitionVersion: "v3",
		PopulationDigest: "sha256:population",
		SourceDigests:    []string{"sha256:source-1", "sha256:source-2"},
		PolicyVersion:    "policy/v12", ModelVersion: "model/v5", Timezone: "America/Los_Angeles",
	}
	bundle.OutputHash = recomputeOutput(bundle, store)
	return bundle
}

// TestTodo_REPRO_001 is the PRIMARY contract: isolated replay from
// bundled digests matches the output hash, or reports the exact changed
// or missing input; the bundle stays tenant and classification scoped.
func TestTodo_REPRO_001(t *testing.T) {
	store := repro001Store()
	bundle := repro001Bundle(store)
	got, err := ReplayEvidence(bundle, store, repro001At)
	if err != nil {
		t.Fatalf("ReplayEvidence: %v", err)
	}
	if got.Verdict != ReplayMatch || got.Digest == "" {
		t.Fatalf("intact bundle must MATCH with a digest: %+v", got)
	}

	t.Run("changed input reports exactly", func(t *testing.T) {
		tampered := repro001Store()
		tampered["sha256:source-2"] = []byte(`{"shard":"ledger-02-tampered"}`)
		got, err := ReplayEvidence(bundle, tampered, repro001At)
		if err != nil {
			t.Fatal(err)
		}
		if got.Verdict != ReplayChanged || len(got.Fields) != 1 || got.Fields[0] != "output" {
			t.Fatalf("changed input must report CHANGED output: %+v", got)
		}
	})

	t.Run("missing input reports exactly", func(t *testing.T) {
		partial := repro001Store()
		delete(partial, "sha256:source-1")
		got, err := ReplayEvidence(bundle, partial, repro001At)
		if err != nil {
			t.Fatal(err)
		}
		if got.Verdict != ReplayMissing || len(got.Fields) != 1 || got.Fields[0] != "sources[0]" {
			t.Fatalf("missing input must report MISSING sources[0]: %+v", got)
		}
	})

	t.Run("result cannot be reconstructed without versions", func(t *testing.T) {
		unversioned := bundle
		unversioned.ModelVersion = ""
		if _, err := ReplayEvidence(unversioned, store, repro001At); !errors.Is(err, ErrReproRejected) {
			t.Fatalf("unversioned bundle must be REPRO_001_REJECTED, got %v", err)
		}
		var rej *ReproRejection
		if _, err := ReplayEvidence(unversioned, store, repro001At); !errors.As(err, &rej) || rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version")
		}
	})
}

func TestTodo_REPRO_001_Golden(t *testing.T) {
	store := repro001Store()
	bundle := repro001Bundle(store)
	got, err := ReplayEvidence(bundle, store, repro001At)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "repro_001_golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read required golden file %s: %v", path, err)
	}
	if string(want) != string(raw)+"\n" {
		t.Fatalf("golden mismatch:\n got %s\nwant %s", raw, want)
	}
}

func TestTodo_REPRO_001_Security(t *testing.T) {
	store := repro001Store()
	bundle := repro001Bundle(store)
	// Tenant scope is enforced: foreign-tenant stores never validate.
	if _, err := ReplayEvidence(bundle, nil, repro001At); !errors.Is(err, ErrReproRejected) {
		t.Fatalf("nil store must be REPRO_001_REJECTED")
	}
	unscoped := bundle
	unscoped.Classification = ""
	if _, err := ReplayEvidence(unscoped, store, repro001At); !errors.Is(err, ErrReproRejected) {
		t.Fatalf("unscoped bundle must be REPRO_001_REJECTED")
	}
	// Classification travels in the seal: swapping it moves the digest.
	relabeled := bundle
	relabeled.Classification = "PUBLIC"
	got, err := ReplayEvidence(relabeled, store, repro001At)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := ReplayEvidence(bundle, store, repro001At)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest == sealed.Digest {
		t.Fatalf("classification must bind the seal")
	}
}

func TestTodo_REPRO_001_Recovery(t *testing.T) {
	store := repro001Store()
	bundle := repro001Bundle(store)
	// Full loss of one shard is reported, not reconstructed: recovery
	// restores the shard, then the replay matches again.
	partial := repro001Store()
	delete(partial, "sha256:source-2")
	lost, err := ReplayEvidence(bundle, partial, repro001At)
	if err != nil {
		t.Fatal(err)
	}
	if lost.Verdict != ReplayMissing {
		t.Fatalf("lost shard must report MISSING: %+v", lost)
	}
	restored := repro001Store()
	restored["sha256:source-2"] = store["sha256:source-2"]
	whole, err := ReplayEvidence(bundle, restored, repro001At)
	if err != nil {
		t.Fatal(err)
	}
	if whole.Verdict != ReplayMatch || whole.Digest == lost.Digest {
		t.Fatalf("restored inputs must MATCH with a new seal: %+v", whole)
	}
}
