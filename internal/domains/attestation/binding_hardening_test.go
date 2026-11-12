package attestation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func bindingFixture() (Binding, AttestationStatement, ContextDigest, []EvidenceRef) {
	tenant := values.TenantId("tenant-a")
	ref := values.EntityRef{Tenant: tenant, Kind: "person", Id: "00000000-0000-4000-8000-000000000001"}
	stmt := AttestationStatement{
		ID: "statement-1", Version: 1, Kind: StatementKindConsent,
		SubjectRef: ref,
		Attester:   AttesterPrincipal{PrincipalRef: ref, IdentityAssuranceRef: IdentityAssuranceRef{ID: "assurance-1", Kind: "CREDENTIAL"}},
		TextDigest: "sha256:text", EvidenceRefs: []EvidenceRef{{ID: "e1", Digest: "sha256:e1"}, {ID: "e2", Digest: "sha256:e2"}},
		ValidityWindow:  ValidityWindow{StartsAt: values.NewInstant(time.Unix(1, 0)), ExpiresAt: values.NewInstant(time.Unix(2, 0))},
		JurisdictionRef: values.EntityRef{Tenant: tenant, Kind: "jurisdiction", Id: "00000000-0000-4000-8000-000000000002"},
	}
	ctx := ContextDigest{SubjectAsOf: "sha256:subject", Jurisdiction: "US", Locale: "en-US", RenderedTextHash: "sha256:text"}
	b := Binding{StatementID: stmt.ID, StatementVersion: stmt.Version, ContextDigest: ctx,
		EvidenceBindings: []EvidenceBinding{{EvidenceID: "e2", EvidenceHash: "sha256:e2"}, {EvidenceID: "e1", EvidenceHash: "sha256:e1"}},
		BindingVersion:   1, BoundAt: values.NewInstant(time.Unix(1, 0)), Signer: ref}
	return b, stmt, ctx, stmt.EvidenceRefs
}

func TestContextDigest_Validate(t *testing.T) {
	base := ContextDigest{SubjectAsOf: "subject", Jurisdiction: "US", Locale: "en-US", RenderedTextHash: "text"}
	cases := []struct {
		name string
		edit func(*ContextDigest)
	}{
		{"subject", func(c *ContextDigest) { c.SubjectAsOf = "" }},
		{"jurisdiction", func(c *ContextDigest) { c.Jurisdiction = "" }},
		{"locale", func(c *ContextDigest) { c.Locale = "" }},
		{"rendered text", func(c *ContextDigest) { c.RenderedTextHash = "" }},
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			tc.edit(&bad)
			if err := bad.Validate(); err == nil {
				t.Fatal("invalid context was accepted")
			}
		})
	}
}

func TestEvidenceBinding_Validate(t *testing.T) {
	base := EvidenceBinding{EvidenceID: "e1", EvidenceHash: "sha256:e1"}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*EvidenceBinding)
	}{
		{"id", func(e *EvidenceBinding) { e.EvidenceID = "" }},
		{"hash", func(e *EvidenceBinding) { e.EvidenceHash = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			tc.edit(&bad)
			if err := bad.Validate(); err == nil {
				t.Fatal("invalid evidence binding was accepted")
			}
		})
	}
}

func TestBinding_ValidateAndCanonical(t *testing.T) {
	base, _, _, _ := bindingFixture()
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(base.Canonical()) == 0 || base.Digest() == "" {
		t.Fatal("valid binding has no canonical representation")
	}
	reordered := base
	reordered.EvidenceBindings = []EvidenceBinding{base.EvidenceBindings[1], base.EvidenceBindings[0]}
	if base.Digest() != reordered.Digest() {
		t.Fatal("evidence order changed canonical digest")
	}
	for _, tc := range []struct {
		name string
		edit func(*Binding)
		want error
	}{
		{"statement id", func(b *Binding) { b.StatementID = "" }, ErrBindingStatementEmpty},
		{"statement version", func(b *Binding) { b.StatementVersion = 0 }, ErrBindingStatementVersion},
		{"binding version", func(b *Binding) { b.BindingVersion = 0 }, ErrBindingVersionZero},
		{"bound at", func(b *Binding) { b.BoundAt = values.Instant{} }, ErrBindingBoundAtEmpty},
		{"signer", func(b *Binding) { b.Signer = values.EntityRef{} }, ErrBindingSignerEmpty},
		{"evidence list", func(b *Binding) { b.EvidenceBindings = nil }, ErrBindingEvidenceEmpty},
		{"context", func(b *Binding) { b.ContextDigest.Locale = "" }, ErrBindingContextInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			bad.EvidenceBindings = append([]EvidenceBinding(nil), base.EvidenceBindings...)
			tc.edit(&bad)
			if err := bad.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want errors.Is(%v)", err, tc.want)
			}
		})
	}
	badEvidence := base
	badEvidence.EvidenceBindings = []EvidenceBinding{{EvidenceID: "", EvidenceHash: "hash"}}
	if err := badEvidence.Validate(); err == nil {
		t.Fatal("invalid nested evidence was accepted")
	}
}

func TestBinding_VerifyReportsStateAndDetail(t *testing.T) {
	binding, stmt, ctx, refs := bindingFixture()
	cases := []struct {
		name   string
		mutate func(*AttestationStatement, *ContextDigest, *[]EvidenceRef)
		want   VerifyDriftReason
	}{
		{"valid", func(*AttestationStatement, *ContextDigest, *[]EvidenceRef) {}, DriftNone},
		{"statement", func(s *AttestationStatement, _ *ContextDigest, _ *[]EvidenceRef) { s.Version++ }, DriftStatementModified},
		{"subject", func(_ *AttestationStatement, c *ContextDigest, _ *[]EvidenceRef) { c.SubjectAsOf = "changed" }, DriftSubjectAsOf},
		{"jurisdiction", func(_ *AttestationStatement, c *ContextDigest, _ *[]EvidenceRef) { c.Jurisdiction = "CA" }, DriftJurisdiction},
		{"locale", func(_ *AttestationStatement, c *ContextDigest, _ *[]EvidenceRef) { c.Locale = "fr" }, DriftLocale},
		{"text", func(_ *AttestationStatement, c *ContextDigest, _ *[]EvidenceRef) { c.RenderedTextHash = "changed" }, DriftRenderedText},
		{"removed", func(_ *AttestationStatement, _ *ContextDigest, r *[]EvidenceRef) { *r = (*r)[:1] }, DriftEvidenceRemoved},
		{"added", func(_ *AttestationStatement, _ *ContextDigest, r *[]EvidenceRef) {
			*r = append(*r, EvidenceRef{ID: "e3", Digest: "sha256:e3"})
		}, DriftEvidenceAdded},
		{"modified", func(_ *AttestationStatement, _ *ContextDigest, r *[]EvidenceRef) { (*r)[0].Digest = "changed" }, DriftEvidenceModified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := stmt
			c := ctx
			r := append([]EvidenceRef(nil), refs...)
			tc.mutate(&s, &c, &r)
			got := binding.Verify(s, c, r)
			if got.Valid != (tc.want == DriftNone) || got.Reason != tc.want {
				t.Fatalf("result = %+v, want reason %s", got, tc.want)
			}
			if tc.want == DriftNone && got.Detail != "" {
				t.Fatalf("valid result detail = %q", got.Detail)
			}
			if tc.want != DriftNone && got.Detail == "" {
				t.Fatal("drift result omitted detail")
			}
		})
	}
}

func TestMemoryStore_BindingCancellationAndCopies(t *testing.T) {
	store := NewMemoryStore()
	binding, stmt, _, _ := bindingFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.PutStatement(ctx, "tenant-a", stmt); !errors.Is(err, context.Canceled) {
		t.Fatalf("PutStatement cancellation = %v", err)
	}
	if err := store.AppendBinding(ctx, "tenant-a", binding); !errors.Is(err, context.Canceled) {
		t.Fatalf("AppendBinding cancellation = %v", err)
	}
	if _, err := store.GetStatement(ctx, "tenant-a", stmt.ID, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetStatement cancellation = %v", err)
	}
	if _, err := store.ListStatementVersions(ctx, "tenant-a", stmt.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListStatementVersions cancellation = %v", err)
	}
	if _, err := store.ListBindings(ctx, "tenant-a", stmt.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListBindings cancellation = %v", err)
	}

	ctx = context.Background()
	if err := store.PutStatement(ctx, "tenant-a", stmt); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetStatement(ctx, "tenant-a", stmt.ID, stmt.Version)
	if err != nil {
		t.Fatal(err)
	}
	loaded.EvidenceRefs[0].Digest = "tampered"
	again, err := store.GetStatement(ctx, "tenant-a", stmt.ID, stmt.Version)
	if err != nil || again.EvidenceRefs[0].Digest == "tampered" {
		t.Fatalf("store returned mutable statement: %+v, %v", again, err)
	}
	if _, err := store.GetStatement(ctx, "tenant-b", stmt.ID, stmt.Version); !errors.Is(err, ErrStatementNotFound) {
		t.Fatalf("cross-tenant read = %v", err)
	}
	if err := store.AppendBinding(ctx, "tenant-a", binding); err != nil {
		t.Fatal(err)
	}
	bindings, err := store.ListBindings(ctx, "tenant-a", binding.StatementID)
	if err != nil || len(bindings) != 1 {
		t.Fatalf("bindings = %#v, %v", bindings, err)
	}
	bindings[0].EvidenceBindings[0].EvidenceHash = "tampered"
	bindingsAgain, err := store.ListBindings(ctx, "tenant-a", binding.StatementID)
	if err != nil || bindingsAgain[0].EvidenceBindings[0].EvidenceHash == "tampered" {
		t.Fatalf("store returned mutable binding: %#v, %v", bindingsAgain, err)
	}
}

// TestTodo_ATTEST_002_DuplicateEvidenceBinding proves that a binding cannot
// carry two different digests for the same evidence ID: such a binding would
// bind the statement to an ambiguous evidence set.
func TestTodo_ATTEST_002_DuplicateEvidenceBinding(t *testing.T) {
	base, _, _, _ := bindingFixture()
	dup := base
	dup.EvidenceBindings = []EvidenceBinding{
		{EvidenceID: "e1", EvidenceHash: "sha256:e1"},
		{EvidenceID: "e1", EvidenceHash: "sha256:e1-other"},
		{EvidenceID: "e2", EvidenceHash: "sha256:e2"},
	}
	if err := dup.Validate(); !errors.Is(err, ErrBindingEvidenceDuplicate) {
		t.Fatalf("duplicate evidence id error = %v, want errors.Is(%v)", err, ErrBindingEvidenceDuplicate)
	}
}
