package filing

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var filing001At = time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

func filing001Input() FilingPackageInput {
	return FilingPackageInput{
		Tenant: "acme", ReportDefinition: "941-employer-quarterly", ReportVersion: "v2026.01",
		SchemaVersion: "irs-941/2026", PeriodRef: "2026-Q1", Jurisdiction: "US-FED",
		SourceValueDigest: "sha256:source-values", SourceWatermark: "watermark:payroll-77",
		RenderedHash: "sha256:rendered", SubmissionHash: "sha256:submission",
		SignerRef:   "officer:controller",
		ApprovalRef: "approval:board-12", IdempotencyKey: "idem-941-q1",
	}
}

type filing001Sender struct {
	state DispatchState
	err   error
	calls int
}

func (s *filing001Sender) Send(pkg FilingPackage) (DispatchOutcome, error) {
	s.calls++
	if s.err != nil {
		return DispatchOutcome{}, s.err
	}
	return DispatchOutcome{State: s.state, Detail: "gateway accepted batch"}, nil
}

// TestTodo_FILING_001 is the PRIMARY contract: the package binds report
// definition and version, source values, rendered and submission hashes,
// signer and idempotency; a provider timeout remains AMBIGUOUS; and a
// duplicate filing never dispatches twice.
func TestTodo_FILING_001(t *testing.T) {
	pkg, err := BuildFilingPackage(filing001Input())
	if err != nil {
		t.Fatalf("BuildFilingPackage: %v", err)
	}
	if pkg.Digest == "" {
		t.Fatalf("package must seal a digest")
	}

	sender := &filing001Sender{state: DispatchAccepted}
	outcome, recorded, err := SubmitFiling(pkg, sender, map[string]DispatchOutcome{}, filing001At)
	if err != nil {
		t.Fatalf("SubmitFiling: %v", err)
	}
	if outcome.State != DispatchAccepted || outcome.PackageDigest != pkg.Digest {
		t.Fatalf("accepted dispatch must bind the package: %+v", outcome)
	}

	t.Run("duplicate filing replays, never redispatches", func(t *testing.T) {
		again, _, err := SubmitFiling(pkg, sender, recorded, filing001At.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if again.State != DispatchAccepted || sender.calls != 1 {
			t.Fatalf("duplicate key must replay without redispatch: %+v (calls=%d)", again, sender.calls)
		}
		other := filing001Input()
		other.ReportVersion = "v2026.02"
		otherPkg, err := BuildFilingPackage(other)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := SubmitFiling(otherPkg, sender, recorded, filing001At); !errors.Is(err, ErrPackageRejected) {
			t.Fatalf("reused key with another package must be FILING_001_REJECTED, got %v", err)
		}
	})

	t.Run("provider timeout remains ambiguous", func(t *testing.T) {
		timeout := &filing001Sender{err: errors.New("gateway timeout after 30s")}
		outcome, recorded, err := SubmitFiling(pkg, timeout, map[string]DispatchOutcome{}, filing001At)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.State != DispatchAmbiguous {
			t.Fatalf("timeout must be AMBIGUOUS: %+v", outcome)
		}
		if recorded[pkg.IdempotencyKey].State != DispatchAmbiguous {
			t.Fatalf("ambiguity must be recorded under the key: %+v", recorded)
		}
	})

	t.Run("wrong identity or missing seals never build", func(t *testing.T) {
		for name, mutate := range map[string]func(*FilingPackageInput){
			"period":       func(in *FilingPackageInput) { in.PeriodRef = "" },
			"jurisdiction": func(in *FilingPackageInput) { in.Jurisdiction = "" },
			"schema":       func(in *FilingPackageInput) { in.SchemaVersion = "" },
			"signature":    func(in *FilingPackageInput) { in.SignerRef = "" },
			"watermark":    func(in *FilingPackageInput) { in.SourceWatermark = "" },
			"approval":     func(in *FilingPackageInput) { in.ApprovalRef = "" },
		} {
			in := filing001Input()
			mutate(&in)
			if _, err := BuildFilingPackage(in); !errors.Is(err, ErrPackageRejected) {
				t.Fatalf("bad %s must be FILING_001_REJECTED", name)
			}
		}
		tampered := pkg
		tampered.ReportVersion = "v2026.02"
		if _, _, err := SubmitFiling(tampered, sender, map[string]DispatchOutcome{}, filing001At); !errors.Is(err, ErrPackageRejected) {
			t.Fatalf("unsealed package must be FILING_001_REJECTED")
		}
	})
}

func TestTodo_FILING_001_Golden(t *testing.T) {
	pkg, err := BuildFilingPackage(filing001Input())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "filing_001_golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read required golden file %s: %v", path, err)
	}
	if string(want) != string(raw)+"\n" {
		t.Fatalf("golden mismatch:\n got %s\nwant %s", raw, want)
	}
}

func TestTodo_FILING_001_Race(t *testing.T) {
	pkg, err := BuildFilingPackage(filing001Input())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sender := &filing001Sender{state: DispatchAccepted}
			outcome, _, err := SubmitFiling(pkg, sender, map[string]DispatchOutcome{}, filing001At)
			if err != nil {
				t.Error(err)
				return
			}
			if outcome.State != DispatchAccepted || sender.calls != 1 {
				t.Errorf("concurrent submit diverged: %+v", outcome)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_FILING_001_Integration(t *testing.T) {
	// The sealed package composes with the evidence chain: payload,
	// submission and acknowledgment digests bind the same package.
	pkg, err := BuildFilingPackage(filing001Input())
	if err != nil {
		t.Fatal(err)
	}
	chain, err := EvidenceChain{}.Append(EvidenceRecord{
		Kind: PayloadEvidence, At: filing001At, ProfileDigest: "profile:941",
		PayloadDigest: pkg.SubmissionHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	chain, err = chain.Append(EvidenceRecord{
		Kind: SubmissionEvidence, At: filing001At, ProfileDigest: "profile:941",
		PayloadDigest: pkg.SubmissionHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	chain, err = chain.Append(EvidenceRecord{
		Kind: AcknowledgmentEvidence, At: filing001At, ProfileDigest: "profile:941",
		PayloadDigest: pkg.SubmissionHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.Verify(); err != nil {
		t.Fatalf("package-bound chain must verify: %v", err)
	}
}

func TestTodo_FILING_001_Fault(t *testing.T) {
	pkg, err := BuildFilingPackage(filing001Input())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SubmitFiling(pkg, nil, map[string]DispatchOutcome{}, filing001At); !errors.Is(err, ErrPackageRejected) {
		t.Fatalf("sender-free submit must be FILING_001_REJECTED")
	}
	if _, _, err := SubmitFiling(pkg, &filing001Sender{state: DispatchAccepted}, map[string]DispatchOutcome{}, time.Time{}); !errors.Is(err, ErrPackageRejected) {
		t.Fatalf("instant-free submit must be FILING_001_REJECTED")
	}
	weird := &filing001Sender{state: "MAYBE"}
	if _, _, err := SubmitFiling(pkg, weird, map[string]DispatchOutcome{}, filing001At); !errors.Is(err, ErrPackageRejected) {
		t.Fatalf("undeclared sender state must be FILING_001_REJECTED")
	}
}

func TestTodo_FILING_001_Security(t *testing.T) {
	pkg, err := BuildFilingPackage(filing001Input())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(pkg)
	for _, secret := range []string{"acct:", "routing:", "ssn:", "password"} {
		if len(raw) > 0 && containsFilingSecret(string(raw), secret) {
			t.Fatalf("package must not carry %s payload", secret)
		}
	}
	// Only digest references cross the boundary: source values travel
	// by digest, never inline.
	if pkg.SourceValueDigest == "" {
		t.Fatalf("source values must bind by digest")
	}
}

func containsFilingSecret(rendered, secret string) bool {
	for i := 0; i+len(secret) <= len(rendered); i++ {
		if rendered[i:i+len(secret)] == secret {
			return true
		}
	}
	return false
}

func TestTodo_FILING_001_Mutation(t *testing.T) {
	a, err := BuildFilingPackage(filing001Input())
	if err != nil {
		t.Fatal(err)
	}
	for field, mutate := range map[string]func(*FilingPackageInput){
		"report":      func(in *FilingPackageInput) { in.ReportVersion = "v2026.02" },
		"period":      func(in *FilingPackageInput) { in.PeriodRef = "2026-Q2" },
		"rendered":    func(in *FilingPackageInput) { in.RenderedHash = "sha256:other" },
		"signer":      func(in *FilingPackageInput) { in.SignerRef = "officer:other" },
		"idempotency": func(in *FilingPackageInput) { in.IdempotencyKey = "idem-other" },
	} {
		in := filing001Input()
		mutate(&in)
		b, err := BuildFilingPackage(in)
		if err != nil {
			t.Fatalf("mutation %s must build: %v", field, err)
		}
		if b.Digest == a.Digest {
			t.Fatalf("mutation %s must move the digest", field)
		}
	}
}
