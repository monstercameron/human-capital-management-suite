package diagnosticsession_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	ds "github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/diagnosticsession"
)

func scope() ds.Scope {
	now := time.Unix(100, 0).UTC()
	scope := ds.Scope{TenantID: "tenant", SubjectID: "operator", CaseID: "case-1", Purpose: ds.PurposeSupport, Resources: []string{"incident:123"}, Actions: []ds.UIAction{ds.ActionViewSummary, ds.ActionViewEvidence}, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute)}
	scope.Approval = ds.MintCustomerApproval(scope, "approval-1", "customer-approver")
	return scope
}

func acceptTestApproval() ds.ApprovalVerifier {
	return ds.ApprovalVerifierFunc(func(ds.Scope, ds.CustomerApproval) error { return nil })
}

func TestADMIN006RegistryExact(t *testing.T) {
	r := ds.Registry()
	if r.ID != "ADMIN-006" || r.Version != "hcmnext.admincenter.diagnostic-session/1" || r.Purpose != ds.PurposeSupport || r.MaxTTL != 15*time.Minute || !r.RedactionRequired {
		t.Fatalf("registry = %+v", r)
	}
	want := []ds.UIAction{ds.ActionViewSummary, ds.ActionViewTimeline, ds.ActionViewEvidence, ds.ActionCopyRedactedReference}
	if !reflect.DeepEqual(r.Actions, want) {
		t.Fatalf("actions = %v, want %v", r.Actions, want)
	}
}

func TestADMIN006JITScopeAndExpiry(t *testing.T) {
	s, err := ds.New(scope(), acceptTestApproval())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(time.Unix(101, 0)); err != nil {
		t.Fatal(err)
	}
	if !s.AllowsResource("incident:123", time.Unix(101, 0)) || s.AllowsResource("incident:999", time.Unix(101, 0)) || !s.AllowsAction(ds.ActionViewSummary, time.Unix(101, 0)) {
		t.Fatal("scope widened or denied unexpectedly")
	}
	if !s.Expired(time.Unix(400, 0)) {
		t.Fatal("expiry not enforced")
	}
	if s.Validate(time.Unix(99, 0)) != ds.ErrNotYetValid || s.AllowsResource("incident:123", time.Unix(99, 0)) || s.AllowsAction(ds.ActionViewSummary, time.Unix(99, 0)) {
		t.Fatal("session was usable before its issued-at time")
	}
	if err := s.Validate(time.Unix(400, 0)); !errors.Is(err, ds.ErrExpired) {
		t.Fatalf("Validate = %v", err)
	}
	bad := scope()
	bad.ExpiresAt = bad.IssuedAt.Add(16 * time.Minute)
	bad.Approval = ds.MintCustomerApproval(bad, "approval-1", "customer-approver")
	if _, err := ds.New(bad, acceptTestApproval()); !errors.Is(err, ds.ErrInvalidScope) {
		t.Fatalf("long TTL = %v", err)
	}
	bad = scope()
	bad.Actions = []ds.UIAction{"delete_user"}
	bad.Approval = ds.MintCustomerApproval(bad, "approval-1", "customer-approver")
	if _, err := ds.New(bad, acceptTestApproval()); !errors.Is(err, ds.ErrActionDenied) {
		t.Fatalf("unsafe action = %v", err)
	}
}

func TestADMIN006RedactsEvidenceWithoutMutating(t *testing.T) {
	p := map[string]any{"email": "person@example.com", "nested": map[string]any{"token": "abc", "status": "ok"}, "count": 3}
	e := ds.PrepareEvidence(ds.Evidence{ID: "ev-1", Payload: p})
	got := e.Payload.(map[string]any)
	if got["email"] != ds.RedactedValue || got["nested"].(map[string]any)["token"] != ds.RedactedValue || got["nested"].(map[string]any)["status"] != "ok" {
		t.Fatalf("payload = %#v", got)
	}
	if !e.Redacted || p["email"] != "person@example.com" {
		t.Fatal("evidence was not safely copied")
	}
}

func TestADMIN006JITScopeRequiredFieldsMatrix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ds.Scope)
	}{
		{"tenant", func(s *ds.Scope) { s.TenantID = " " }},
		{"subject", func(s *ds.Scope) { s.SubjectID = "" }},
		{"case", func(s *ds.Scope) { s.CaseID = "\t" }},
		{"purpose", func(s *ds.Scope) { s.Purpose = "other" }},
		{"resources", func(s *ds.Scope) { s.Resources = nil }},
		{"actions", func(s *ds.Scope) { s.Actions = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scope()
			tc.mutate(&got)
			if _, err := ds.New(got, acceptTestApproval()); !errors.Is(err, ds.ErrInvalidScope) {
				t.Fatalf("New(%s) = %v, want ErrInvalidScope", tc.name, err)
			}
		})
	}
}

func TestADMIN006TTLAndExpiryBoundaries(t *testing.T) {
	base := scope()
	base.ExpiresAt = base.IssuedAt.Add(ds.MaxTTL)
	base.Approval = ds.MintCustomerApproval(base, "approval-1", "customer-approver")
	if _, err := ds.New(base, acceptTestApproval()); err != nil {
		t.Fatalf("maximum TTL rejected: %v", err)
	}

	s, err := ds.New(scope(), acceptTestApproval())
	if err != nil {
		t.Fatal(err)
	}
	if s.Expired(s.Scope().ExpiresAt.Add(-time.Nanosecond)) {
		t.Fatal("session expired before its deadline")
	}
	if !s.Expired(s.Scope().ExpiresAt) {
		t.Fatal("session remained valid at its deadline")
	}
}

func TestADMIN006ScopeAndAllowlistIsolation(t *testing.T) {
	in := scope()
	s, err := ds.New(in, acceptTestApproval())
	if err != nil {
		t.Fatal(err)
	}
	// New must detach caller-owned slices, and Scope must return fresh copies.
	in.Resources[0] = "tenant-b:incident:123"
	in.Actions[0] = ds.ActionViewTimeline
	got := s.Scope()
	validAt := in.IssuedAt.Add(time.Minute)
	if !s.AllowsResource("incident:123", validAt) || s.AllowsResource("tenant-b:incident:123", validAt) || !s.AllowsAction(ds.ActionViewSummary, validAt) {
		t.Fatalf("caller mutation widened or changed session: %+v", got)
	}
	got.Resources[0] = "tenant-c:incident:123"
	got.Actions[0] = ds.ActionViewTimeline
	if s.AllowsResource("tenant-c:incident:123", validAt) || s.AllowsAction(ds.ActionViewTimeline, validAt) {
		t.Fatal("Scope returned aliased mutable slices")
	}
	if s.AllowsResource("incident:123", s.Scope().ExpiresAt) || s.AllowsAction(ds.ActionViewSummary, s.Scope().ExpiresAt) {
		t.Fatal("expired session allowed resource or action access")
	}
}

func TestADMIN006UIActionAndSensitiveKeyBoundaries(t *testing.T) {
	for _, action := range []ds.UIAction{ds.ActionViewSummary, ds.ActionViewTimeline, ds.ActionViewEvidence, ds.ActionCopyRedactedReference} {
		if !ds.IsSafeUIAction(action) {
			t.Fatalf("safe action %q rejected", action)
		}
	}
	if ds.IsSafeUIAction("delete") {
		t.Fatal("unsafe action accepted")
	}
	for _, key := range []string{" PASSWORD ", "secret_value", "TOKEN", "authorization", "cookie", "email", "phone", "ssn", "salary", "access_key", "private_key"} {
		if !ds.SensitiveKey(key) {
			t.Fatalf("sensitive key %q was not recognized", key)
		}
	}
	if ds.SensitiveKey("status") {
		t.Fatal("ordinary key classified as sensitive")
	}
	input := []any{map[string]any{"status": "ok", "secret": "hidden"}, "plain"}
	got := ds.Redact(input).([]any)
	if got[0].(map[string]any)["secret"] != ds.RedactedValue || got[0].(map[string]any)["status"] != "ok" || got[1] != "plain" {
		t.Fatalf("redacted array=%#v", got)
	}
}

func TestADMIN006EvidenceDigestBoundIsEnforced(t *testing.T) {
	e := ds.PrepareEvidence(ds.Evidence{ID: "large", Payload: map[string]any{"blob": strings.Repeat("x", ds.MaxEvidenceBytes)}})
	if !e.Redacted || e.Digest != "" {
		t.Fatalf("oversized evidence controls=%+v", e)
	}
	if got := ds.PrepareEvidence(ds.Evidence{ID: "scalar", Payload: "safe"}); got.Digest == "" || !got.Redacted {
		t.Fatalf("scalar evidence=%+v", got)
	}
}

func TestADMIN006NewDefaultsAndBoundaryErrors(t *testing.T) {
	bare := ds.Scope{TenantID: "tenant", SubjectID: "subject", CaseID: "case", Purpose: ds.PurposeSupport, Resources: []string{"incident:1"}, Actions: []ds.UIAction{ds.ActionViewSummary}}
	bare.Approval = ds.MintCustomerApproval(bare, "approval-1", "customer-approver")
	s, err := ds.New(bare, acceptTestApproval())
	if err != nil {
		t.Fatal(err)
	}
	if s.Scope().IssuedAt.IsZero() || !s.Scope().ExpiresAt.After(s.Scope().IssuedAt) {
		t.Fatalf("defaults not applied: %+v", s.Scope())
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ds.Scope)
	}{
		{"empty resource", func(s *ds.Scope) { s.Resources = []string{" "} }},
		{"equal expiry", func(s *ds.Scope) { s.ExpiresAt = s.IssuedAt }},
		{"expired at now", func(s *ds.Scope) { s.ExpiresAt = s.IssuedAt.Add(-time.Nanosecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := scope()
			tc.mutate(&in)
			in.Approval = ds.MintCustomerApproval(in, "approval-1", "customer-approver")
			if _, err := ds.New(in, acceptTestApproval()); !errors.Is(err, ds.ErrInvalidScope) {
				t.Fatalf("New=%v", err)
			}
		})
	}
}

type admin006Inner struct {
	Token  string `json:"token"`
	Status string `json:"status"`
}

type admin006Outer struct {
	Name     string         `json:"name"`
	Password string         `json:"password"`
	Inner    *admin006Inner `json:"inner"`
	Tags     []string       `json:"tags"`
}

func TestADMIN006RedactsTypedNestedPayloads(t *testing.T) {
	payload := map[string]any{
		"user":   map[string]string{"email": "person@example.com", "name": "Ada"},
		"scores": map[string]int{"salary": 90000, "level": 5},
		"tags":   []string{"support", "ok"},
		"matrix": [2]string{"a", "b"},
	}
	got, ok := ds.Redact(payload).(map[string]any)
	if !ok {
		t.Fatalf("Redact = %#v, want map[string]any", ds.Redact(payload))
	}
	user, ok := got["user"].(map[string]any)
	if !ok || user["email"] != ds.RedactedValue || user["name"] != "Ada" {
		t.Fatalf("typed nested map = %#v", got["user"])
	}
	scores, ok := got["scores"].(map[string]any)
	if !ok || scores["salary"] != ds.RedactedValue {
		t.Fatalf("typed salary map = %#v", got["scores"])
	}
	if level, ok := scores["level"].(json.Number); !ok || level.String() != "5" {
		t.Fatalf("level = %#v, want json.Number 5", scores["level"])
	}
	tags, ok := got["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "support" || tags[1] != "ok" {
		t.Fatalf("typed slice = %#v", got["tags"])
	}
	matrix, ok := got["matrix"].([]any)
	if !ok || len(matrix) != 2 || matrix[0] != "a" || matrix[1] != "b" {
		t.Fatalf("array = %#v", got["matrix"])
	}
	raw, err := json.Marshal(got)
	if err != nil || strings.Contains(string(raw), "person@example.com") {
		t.Fatalf("redacted payload leaks or is unmarshalable: %s, err=%v", raw, err)
	}
}

func TestADMIN006RedactsStructPointerPayloads(t *testing.T) {
	in := &admin006Outer{Name: "ops", Password: "super-secret", Inner: &admin006Inner{Token: "abc", Status: "ok"}, Tags: []string{"a"}}
	got, ok := ds.Redact(in).(map[string]any)
	if !ok {
		t.Fatalf("Redact = %#v, want map[string]any", ds.Redact(in))
	}
	if got["name"] != "ops" || got["password"] != ds.RedactedValue {
		t.Fatalf("struct fields = %#v", got)
	}
	inner, ok := got["inner"].(map[string]any)
	if !ok || inner["token"] != ds.RedactedValue || inner["status"] != "ok" {
		t.Fatalf("nested pointer = %#v", got["inner"])
	}
	raw, err := json.Marshal(got)
	if err != nil || strings.Contains(string(raw), "super-secret") || strings.Contains(string(raw), `"abc"`) {
		t.Fatalf("struct payload leaks: %s, err=%v", raw, err)
	}
	if in.Password != "super-secret" || in.Inner.Token != "abc" {
		t.Fatal("struct input was mutated")
	}
}

func TestADMIN006RedactNormalizesNumbers(t *testing.T) {
	got, ok := ds.Redact(map[string]any{"count": 3, "ratio": 1.5, "nested": map[string]any{"total": 42}}).(map[string]any)
	if !ok {
		t.Fatal("Redact did not return map[string]any")
	}
	if n, ok := got["count"].(json.Number); !ok || n.String() != "3" {
		t.Fatalf("count = %#v, want json.Number 3", got["count"])
	}
	if n, ok := got["ratio"].(json.Number); !ok || n.String() != "1.5" {
		t.Fatalf("ratio = %#v, want json.Number 1.5", got["ratio"])
	}
	nested, ok := got["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested = %#v", got["nested"])
	}
	if n, ok := nested["total"].(json.Number); !ok || n.String() != "42" {
		t.Fatalf("total = %#v, want json.Number 42", nested["total"])
	}
	preserved, ok := ds.Redact(map[string]any{"id": json.Number("123")}).(map[string]any)
	if !ok {
		t.Fatal("Redact did not return map[string]any")
	}
	if n, ok := preserved["id"].(json.Number); !ok || n.String() != "123" {
		t.Fatalf("id = %#v, want json.Number 123", preserved["id"])
	}
}

func TestADMIN006RedactFailsClosedOnCycle(t *testing.T) {
	cyclic := map[string]any{"status": "ok"}
	cyclic["self"] = cyclic
	if got := ds.Redact(cyclic); got != ds.RedactedValue {
		t.Fatalf("cyclic Redact = %#v, want %q", got, ds.RedactedValue)
	}
	e := ds.PrepareEvidence(ds.Evidence{ID: "cycle", Digest: "sha256:stale", Payload: cyclic})
	if !e.Redacted || e.Payload != ds.RedactedValue || e.Digest == "sha256:stale" {
		t.Fatalf("cyclic evidence = %+v", e)
	}
}

func TestADMIN006RedactFailsClosedOnUnsupported(t *testing.T) {
	cases := map[string]any{"chan": make(chan int), "func": func() {}, "complex": complex(1, 2)}
	for name, v := range cases {
		if got := ds.Redact(v); got != ds.RedactedValue {
			t.Fatalf("%s: Redact = %#v, want %q", name, got, ds.RedactedValue)
		}
	}
	nested := map[string]any{"token": "secret", "ch": make(chan int)}
	e := ds.PrepareEvidence(ds.Evidence{ID: "unsupported", Digest: "sha256:stale", Payload: nested})
	if !e.Redacted || e.Payload != ds.RedactedValue || e.Digest == "sha256:stale" {
		t.Fatalf("unsupported evidence = %+v", e)
	}
	raw, err := json.Marshal(e.Payload)
	if err != nil || strings.Contains(string(raw), "secret") {
		t.Fatalf("unsupported evidence leaks: %s, err=%v", raw, err)
	}
}

func TestADMIN006RedactDoesNotMutateInput(t *testing.T) {
	nested := map[string]any{"token": "abc", "status": "ok"}
	tags := []any{"a", "b"}
	p := map[string]any{"email": "person@example.com", "nested": nested, "tags": tags}
	typed := map[string]string{"email": "typed@example.com", "name": "Ada"}
	got, ok := ds.Redact(p).(map[string]any)
	if !ok {
		t.Fatal("Redact did not return map[string]any")
	}
	gotTyped, ok := ds.Redact(typed).(map[string]any)
	if !ok {
		t.Fatal("Redact did not normalize typed map to map[string]any")
	}
	got["email"] = "changed"
	got["nested"].(map[string]any)["status"] = "changed"
	got["tags"].([]any)[0] = "changed"
	gotTyped["name"] = "changed"
	if p["email"] != "person@example.com" || nested["token"] != "abc" || nested["status"] != "ok" || tags[0] != "a" {
		t.Fatalf("caller input was mutated: %#v %#v %#v", p, nested, tags)
	}
	if typed["email"] != "typed@example.com" || typed["name"] != "Ada" {
		t.Fatalf("typed caller input was mutated: %#v", typed)
	}
}

func TestADMIN006PrepareEvidenceClearsStaleDigest(t *testing.T) {
	big := ds.PrepareEvidence(ds.Evidence{ID: "big", Digest: "sha256:stale", Payload: map[string]any{"blob": strings.Repeat("x", ds.MaxEvidenceBytes)}})
	if !big.Redacted || big.Digest != "" {
		t.Fatalf("oversized evidence kept a digest: %+v", big)
	}
	if got := ds.PrepareEvidence(ds.Evidence{ID: "small", Digest: "sha256:stale", Payload: map[string]any{"status": "ok"}}); !got.Redacted || got.Digest == "" || got.Digest == "sha256:stale" {
		t.Fatalf("small evidence digest = %+v", got)
	}
}

func TestADMIN006RedactedEvidenceIsDeterministic(t *testing.T) {
	payload := func() any {
		return map[string]any{
			"email":  "person@example.com",
			"token":  "secret",
			"counts": map[string]int{"a": 1, "b": 2},
			"items":  []any{"x", map[string]string{"phone": "555-0100", "note": "hi"}},
		}
	}
	first := ds.PrepareEvidence(ds.Evidence{ID: "ev", Payload: payload()})
	second := ds.PrepareEvidence(ds.Evidence{ID: "ev", Payload: payload()})
	rawFirst, errFirst := json.Marshal(first.Payload)
	rawSecond, errSecond := json.Marshal(second.Payload)
	if errFirst != nil || errSecond != nil || string(rawFirst) != string(rawSecond) {
		t.Fatalf("redaction is not deterministic: %s vs %s (%v, %v)", rawFirst, rawSecond, errFirst, errSecond)
	}
	if !first.Redacted || first.Digest == "" || first.Digest != second.Digest {
		t.Fatalf("evidence lacks stable provenance: %+v vs %+v", first, second)
	}
	if want := fmt.Sprintf("sha256:%x", sha256.Sum256(rawFirst)); first.Digest != want {
		t.Fatalf("digest = %q, want %q", first.Digest, want)
	}
	for _, leaked := range []string{"person@example.com", "secret", "555-0100"} {
		if strings.Contains(string(rawFirst), leaked) {
			t.Fatalf("redacted evidence leaks %q: %s", leaked, rawFirst)
		}
	}
	if !strings.Contains(string(rawFirst), ds.RedactedValue) {
		t.Fatalf("no redaction marker in %s", rawFirst)
	}
}
