// REPRO-001: package reproducible search and analytics evidence.
//
// ReplayEvidence replays one bundled search or analytics result from its
// pinned digests: query, definition, population, sources, policy, model
// and timezone versions. The replay matches the output hash exactly, or
// reports the changed or missing input — it never silently adjusts.
// Bundles stay tenant and classification scoped. The function is
// kernel-pure; large inputs travel as content-addressed references.
package search

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// ReproVersion is the rejection version for evidence replay.
const ReproVersion = "search-repro/v1"

var (
	// ErrReproRejected is the REPRO-001 sentinel for unreproducible or
	// unscoped bundles.
	ErrReproRejected = errors.New("REPRO_001_REJECTED")
)

// ReproRejection is the stable REPRO-001 failure shape.
type ReproRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *ReproRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrReproRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the REPRO_001_REJECTED sentinel to errors.Is.
func (r *ReproRejection) Unwrap() error { return ErrReproRejected }

func reproReject(field, state, reason string) error {
	return &ReproRejection{Field: field, State: state, Version: ReproVersion, Reason: reason}
}

// ReproBundle is the isolated evidence package for one result. Every
// input is a versioned digest; large inputs are content-addressed
// references resolved through InputStore.
type ReproBundle struct {
	Tenant            string
	Classification    string
	QueryDigest       string
	QueryVersion      string
	DefinitionDigest  string
	DefinitionVersion string
	PopulationDigest  string
	SourceDigests     []string
	PolicyVersion     string
	ModelVersion      string
	Timezone          string
	OutputHash        string
}

// InputStore resolves content-addressed input digests.
type InputStore interface {
	Resolve(digest string) ([]byte, bool)
}

// MapInputStore is an in-memory InputStore for tests and harnesses.
type MapInputStore map[string][]byte

// Resolve returns the bytes for one digest.
func (m MapInputStore) Resolve(digest string) ([]byte, bool) {
	raw, ok := m[digest]
	return raw, ok
}

// ReplayVerdict is the closed REPRO-001 replay vocabulary.
type ReplayVerdict string

const (
	ReplayMatch   ReplayVerdict = "MATCH"
	ReplayChanged ReplayVerdict = "CHANGED"
	ReplayMissing ReplayVerdict = "MISSING"
)

// ReplayResult is the deterministic replay outcome.
type ReplayResult struct {
	Verdict ReplayVerdict
	Fields  []string
	Digest  string
}

func (r ReplayResult) computedDigest(bundle ReproBundle) string {
	sources := append([]string(nil), bundle.SourceDigests...)
	sort.Strings(sources)
	w := canonicalbytes.New("hcmnext.engines.search.ReplayResult", 1).
		String("tenant", bundle.Tenant).
		String("classification", bundle.Classification).
		String("query", bundle.QueryDigest).
		String("query_version", bundle.QueryVersion).
		String("definition", bundle.DefinitionDigest).
		String("definition_version", bundle.DefinitionVersion).
		String("population", bundle.PopulationDigest).
		SortedStrings("sources", sources).
		String("policy", bundle.PolicyVersion).
		String("model", bundle.ModelVersion).
		String("timezone", bundle.Timezone).
		String("output", bundle.OutputHash).
		String("verdict", string(r.Verdict)).
		SortedStrings("fields", append([]string(nil), r.Fields...))
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// ReplayEvidence replays one bundle in isolation against the store.
func ReplayEvidence(bundle ReproBundle, store InputStore, now time.Time) (ReplayResult, error) {
	if strings.TrimSpace(bundle.Tenant) == "" {
		return ReplayResult{}, reproReject("repro.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(bundle.Classification) == "" {
		return ReplayResult{}, reproReject("repro.classification", "MISSING", "classification scope is required")
	}
	for name, digest := range map[string]string{
		"query": bundle.QueryDigest, "definition": bundle.DefinitionDigest,
		"population": bundle.PopulationDigest, "output": bundle.OutputHash,
	} {
		if strings.TrimSpace(digest) == "" {
			return ReplayResult{}, reproReject("repro."+name, "MISSING", "every pinned digest is required")
		}
	}
	if strings.TrimSpace(bundle.QueryVersion) == "" || strings.TrimSpace(bundle.DefinitionVersion) == "" ||
		strings.TrimSpace(bundle.PolicyVersion) == "" || strings.TrimSpace(bundle.ModelVersion) == "" ||
		strings.TrimSpace(bundle.Timezone) == "" {
		return ReplayResult{}, reproReject("repro.versions", "MISSING", "query, definition, policy, model and timezone versions are required")
	}
	if len(bundle.SourceDigests) == 0 {
		return ReplayResult{}, reproReject("repro.sources", "MISSING", "at least one source digest is required")
	}
	if store == nil {
		return ReplayResult{}, reproReject("repro.store", "MISSING", "input store is required")
	}
	if now.IsZero() {
		return ReplayResult{}, reproReject("repro.now", "MISSING", "replay instant is required")
	}
	res := ReplayResult{Verdict: ReplayMatch}
	inputs := map[string]string{
		"query": bundle.QueryDigest, "definition": bundle.DefinitionDigest,
		"population": bundle.PopulationDigest,
	}
	for i, digest := range bundle.SourceDigests {
		inputs[fmt.Sprintf("sources[%d]", i)] = digest
	}
	for field, digest := range inputs {
		raw, ok := store.Resolve(digest)
		if !ok || len(raw) == 0 {
			res.Verdict = ReplayMissing
			res.Fields = append(res.Fields, field)
		}
	}
	if res.Verdict == ReplayMissing {
		sort.Strings(res.Fields)
		res.Digest = res.computedDigest(bundle)
		return res, nil
	}
	recomputed := recomputeOutput(bundle, store)
	switch {
	case recomputed == "":
		res.Verdict = ReplayMissing
		res.Fields = []string{"output"}
	case recomputed != bundle.OutputHash:
		res.Verdict = ReplayChanged
		res.Fields = []string{"output"}
	}
	sort.Strings(res.Fields)
	res.Digest = res.computedDigest(bundle)
	return res, nil
}

// recomputeOutput replays the pure combination the bundle records: the
// output hash is sealed over the ordered input bytes. Any changed input
// moves the recomputation, which is exactly the changed-input report.
func recomputeOutput(bundle ReproBundle, store InputStore) string {
	digests := []string{bundle.QueryDigest, bundle.DefinitionDigest, bundle.PopulationDigest}
	sources := append([]string(nil), bundle.SourceDigests...)
	sort.Strings(sources)
	digests = append(digests, sources...)
	w := canonicalbytes.New("hcmnext.engines.search.ReproOutput", 1).
		String("query_version", bundle.QueryVersion).
		String("definition_version", bundle.DefinitionVersion).
		String("policy", bundle.PolicyVersion).
		String("model", bundle.ModelVersion).
		String("timezone", bundle.Timezone)
	for i, digest := range digests {
		raw, ok := store.Resolve(digest)
		if !ok {
			return ""
		}
		w.Field(fmt.Sprintf("input_%d", i), raw)
	}
	out, err := w.Digest()
	if err != nil {
		return ""
	}
	return out
}
