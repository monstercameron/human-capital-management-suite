package agentrollout

import (
	"errors"
	"sync"
	"testing"
)

func versionRequest() VersionRequest {
	return VersionRequest{ID: "plan", TenantID: "tenant", AgentID: "agent", Version: 2, ProfileDigest: "digest", EvaluationRef: "eval", ReviewRef: "review", BatchLimit: 1, CanaryIDs: []string{"i1"}, Candidates: []VersionCandidate{{InstallationID: "i2", ConversationID: "c2", Version: 1, Revision: 2, RevocationEpoch: 1, AuthorityRevision: 3, PolicyDigest: "policy"}, {InstallationID: "i1", ConversationID: "c1", Version: 1, Revision: 1, RevocationEpoch: 1, AuthorityRevision: 3, PolicyDigest: "policy"}}}
}
func TestTodo_AGENT_044_VersionPlan(t *testing.T) {
	p, err := PreviewVersion(versionRequest())
	if err != nil || p.Candidates[0].InstallationID != "i1" || p.Digest == "" || p.CanaryCount != 1 {
		t.Fatalf("preview=%+v err=%v", p, err)
	}
	if err = p.Verify(); err != nil {
		t.Fatal(err)
	}
	p.Candidates[0].RevocationEpoch++
	if !errors.Is(p.Verify(), ErrPreviewStale) {
		t.Fatal("tampered revoked installation accepted")
	}
}
func TestTodo_AGENT_044_Security_VersionPlan(t *testing.T) {
	for _, mutate := range []func(*VersionRequest){func(r *VersionRequest) { r.CanaryIDs = []string{"unknown"} }, func(r *VersionRequest) { r.Candidates = append(r.Candidates, r.Candidates[0]) }, func(r *VersionRequest) { r.EvaluationRef = "" }, func(r *VersionRequest) { r.Candidates[0].AuthorityRevision = 0 }, func(r *VersionRequest) { r.TenantID = "" }} {
		r := versionRequest()
		mutate(&r)
		if _, err := PreviewVersion(r); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid request accepted: %v", err)
		}
	}
}
func TestTodo_AGENT_044_Golden_VersionPlan(t *testing.T) {
	a, _ := PreviewVersion(versionRequest())
	r := versionRequest()
	r.Candidates[0], r.Candidates[1] = r.Candidates[1], r.Candidates[0]
	b, _ := PreviewVersion(r)
	if a.Digest != b.Digest {
		t.Fatal("input order changes digest")
	}
	r.Version = 1
	c, _ := PreviewVersion(r)
	if c.Digest == a.Digest {
		t.Fatal("rollback target not digest bound")
	}
}
func TestTodo_AGENT_044_Race_VersionPlan(t *testing.T) {
	r := versionRequest()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p, e := PreviewVersion(r); e != nil || p.Verify() != nil {
				t.Error("concurrent preview failed")
			}
		}()
	}
	wg.Wait()
	if r.Candidates[0].InstallationID != "i2" {
		t.Fatal("preview mutated request")
	}
}
