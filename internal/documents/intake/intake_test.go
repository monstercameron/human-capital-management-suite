package intake

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
)

type scanner struct{ state documentsecurity.State }

func (s scanner) Scan(context.Context, documentsecurity.ScanInput, documentsecurity.Limits) (documentsecurity.Verdict, error) {
	return documentsecurity.Verdict{State: s.state, Scanner: "test-av", ScannerVersion: "1", Derivative: []byte("%PDF-1.7\nsafe derivative")}, nil
}

func fixture(t *testing.T) (*documentsecurity.Registry, documentsecurity.Upload, documentsecurity.Limits) {
	t.Helper()
	r := documentsecurity.NewRegistry()
	u := documentsecurity.Upload{ID: "upload-1", Name: "medical.pdf", ContentType: "application/pdf", Bytes: []byte("bytes")}
	l := documentsecurity.Limits{MaxBytes: 100, MaxDerivativeBytes: 100, MaxCompressionRatio: 10}
	return r, u, l
}

func TestTodo_DOC_INTAKE_001(t *testing.T) {
	r, u, l := fixture(t)
	if _, err := r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe}); err != nil {
		t.Fatal(err)
	}
	p, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "leave.review", SubjectID: "worker-1", RequestedBy: "specialist", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Evidence.Check(); err != nil {
		t.Fatal(err)
	}
	if p.Evidence.Classification != MedicalSensitive || p.Evidence.Access.Download {
		t.Fatalf("unsafe governance: %#v", p.Evidence)
	}
	if p.DocumentType != "PDF" {
		t.Fatalf("document type was not derived from the scanned derivative: %q", p.DocumentType)
	}
}

func TestTodo_DOC_INTAKE_001_Golden(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
	p, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "leave.review", SubjectID: "worker-1", RequestedBy: "specialist", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if p.Evidence.ID != "evidence:upload-1-1" || p.DocumentType != "PDF" {
		t.Fatalf("unexpected golden package: %#v", p)
	}
}

func TestTodo_DOC_INTAKE_001_Security(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Unsafe})
	if _, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "x", SubjectID: "w", RequestedBy: "s"}); !errors.Is(err, ErrNotReleasable) {
		t.Fatalf("err=%v", err)
	}
	// A caller cannot select a less restrictive classification.
}

func TestTodo_DOC_INTAKE_001_Integration(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
	p, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "review", SubjectID: "w", RequestedBy: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Evidence.Usable() {
		t.Fatal("safe evidence is not usable")
	}
	r.Revoke(u.ID, "rescan unsafe")
	if p.Evidence.Usable() {
		t.Fatal("revoked evidence remains usable")
	}
}

func TestTodo_DOC_INTAKE_001_Mutation(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
	p, _ := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "review", SubjectID: "w", RequestedBy: "s"})
	p.Evidence.Access.Download = true
	if p.Evidence.Validate() == nil {
		t.Fatal("mutable caller fields must not weaken validation")
	}
}

func TestTodo_DOC_INTAKE_001_ReferenceTampering(t *testing.T) {
	r, u, l := fixture(t)
	if _, err := r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe}); err != nil {
		t.Fatal(err)
	}
	p, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "leave.review", SubjectID: "worker-1", RequestedBy: "specialist", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*EvidenceRef)
	}{
		{"authority", func(ref *EvidenceRef) { ref.Authority = "caller-selected" }},
		{"provenance", func(ref *EvidenceRef) { ref.Provenance = "scanner:forged@1" }},
		{"purpose", func(ref *EvidenceRef) { ref.Purpose = "payroll.publish"; ref.Access.Purpose = "payroll.publish" }},
		{"document type", func(ref *EvidenceRef) { ref.DocumentType = "PDF_WITH_APPROVAL" }},
		{"retention", func(ref *EvidenceRef) { ref.Retention.PolicyRef = "records.no-expiry/v1" }},
		{"issue time", func(ref *EvidenceRef) { ref.IssuedAt = ref.IssuedAt.Add(time.Hour) }},
		{"digest", func(ref *EvidenceRef) {
			ref.ArtifactDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forged := p.Evidence
			tc.edit(&forged)
			if err := forged.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("tampered reference validated: %v", err)
			}
			if forged.Usable() {
				t.Fatal("tampered reference remained usable")
			}
		})
	}
}

func TestTodo_DOC_INTAKE_001_ServerDerivedDocumentType(t *testing.T) {
	r, u, l := fixture(t)
	u.Name = "misleading.png"
	u.ContentType = "image/png"
	if _, err := r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe}); err != nil {
		t.Fatal(err)
	}
	p, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "leave.review", SubjectID: "worker-1", RequestedBy: "specialist"})
	if err != nil {
		t.Fatal(err)
	}
	if p.DocumentType != "PDF" || p.Evidence.DocumentType != "PDF" {
		t.Fatalf("caller metadata selected document type: package=%q evidence=%q", p.DocumentType, p.Evidence.DocumentType)
	}
}

func TestTodo_DOC_INTAKE_001_Race(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
	s := NewService(r)
	type result struct {
		pkg Package
		err error
	}
	done := make(chan result, 20)
	for i := 0; i < 20; i++ {
		go func() {
			pkg, err := s.Release(Request{ArtifactID: u.ID, Purpose: "review", SubjectID: "w", RequestedBy: "s"})
			done <- result{pkg: pkg, err: err}
		}()
	}
	ids := make(map[string]struct{}, 20)
	for i := 0; i < 20; i++ {
		got := <-done
		if got.err != nil {
			t.Fatalf("concurrent release failed: %v", got.err)
		}
		if err := got.pkg.Evidence.Check(); err != nil {
			t.Fatalf("concurrent evidence ref invalid: %v", err)
		}
		if _, exists := ids[got.pkg.ID]; exists {
			t.Fatalf("duplicate concurrent evidence package id %q", got.pkg.ID)
		}
		ids[got.pkg.ID] = struct{}{}
	}
}

func FuzzTodo_DOC_INTAKE_001(f *testing.F) {
	f.Add("review", "worker")
	f.Fuzz(func(t *testing.T, purpose, subject string) {
		r, u, l := fixture(t)
		_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
		pkg, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: purpose, SubjectID: subject, RequestedBy: "specialist"})
		if strings.TrimSpace(purpose) == "" || strings.TrimSpace(subject) == "" {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("blank caller context returned %v, want ErrInvalid", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("nonblank context refused: %v", err)
		}
		if err := pkg.Evidence.Check(); err != nil {
			t.Fatalf("fuzzed reference failed check: %v", err)
		}
	})
}
