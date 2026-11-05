// TestTodo_WF_DATA_031 proves a tenant field may draw record numbers from a
// formatted sequence, choose from a governed reference-data release, or pin
// a scanned document artifact; sequences never duplicate under concurrency.
package custom_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/custom"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/refdata"
)

var wfd31FixedTime = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func wfd31Digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func wfd31Classification() custom.FieldClassification {
	return custom.FieldClassification{
		AuthZDomain: "worker.core", Classification: "CONFIDENTIAL",
		ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL",
	}
}

func wfd31Def(release refdata.Release) custom.CustomObjectDefinition {
	return custom.CustomObjectDefinition{
		Kind: "PurchaseOrder", Namespace: "procurement", Version: 1,
		Fields: map[string]custom.FieldDefinition{
			"supplier": {Type: "string", Classification: wfd31Classification()},
			"po_number": {
				Type: "string", Classification: wfd31Classification(),
				Kind: custom.FieldKindSequence, Sequence: &custom.SequenceConfig{Format: "PO-{seq:6}"},
			},
			"country": {
				Type: "string", Classification: wfd31Classification(),
				Kind: custom.FieldKindReference, Reference: &custom.ReferenceConfig{
					DatasetID: release.DatasetID, Version: release.Version, ReleaseDigest: release.Digest,
				},
			},
			"invoice": {
				Type: "string", Classification: wfd31Classification(),
				Kind: custom.FieldKindAttachment, Attachment: &custom.AttachmentConfig{
					AllowedContentTypes: []string{"application/pdf"},
				},
			},
		},
	}
}

func wfd31Member(t *testing.T, code, label string, retired bool, from, to time.Time) refdata.Member {
	t.Helper()
	m, err := refdata.NewMember(code, label, false, retired, from, to, nil)
	if err != nil {
		t.Fatalf("member %s: %v", code, err)
	}
	return m
}

func wfd31Release(t *testing.T, dataset, version string, members []refdata.Member) refdata.Release {
	t.Helper()
	draft, err := refdata.NewRelease(refdata.Release{
		DatasetID: dataset, Version: version,
		Source: refdata.SourceEvidence{
			SourceRef: "iso.country", SourceVersion: "2026-01", SourceDigest: wfd31Digest("source"),
			SignatureRef: "sig:iso", LicenseRef: "license:iso", Coverage: "global",
			RetrievedAt: wfd31FixedTime,
		},
		SchemaDigest: wfd31Digest("schema"), Members: members,
		Applicability: []string{"procurement"}, ConsumerRefs: []string{"custom.purchase_order"},
		AffectedIntents: []string{"intent:procurement"}, EffectiveFrom: wfd31FixedTime.AddDate(-1, 0, 0),
		KnownAt: wfd31FixedTime,
	})
	if err != nil {
		t.Fatalf("seal release: %v", err)
	}
	validation, err := refdata.ValidateRelease(draft, wfd31FixedTime)
	if err != nil {
		t.Fatalf("validate release: %v", err)
	}
	validated, err := refdata.MarkValidated(draft, validation)
	if err != nil {
		t.Fatalf("mark validated: %v", err)
	}
	published, err := refdata.PublishRelease(validated, validation, wfd31FixedTime)
	if err != nil {
		t.Fatalf("publish release: %v", err)
	}
	return published
}

func wfd31CountryRelease(t *testing.T) refdata.Release {
	t.Helper()
	return wfd31Release(t, "country", "v3", []refdata.Member{
		wfd31Member(t, "US", "United States", false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}),
		wfd31Member(t, "DE", "Germany", false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}),
		wfd31Member(t, "XX", "Retired", true, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}),
		wfd31Member(t, "FU", "Future", false, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}),
		wfd31Member(t, "EX", "Expired", false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
	})
}

type wfd31Scanner struct {
	verdict documentsecurity.State
	err     error
}

func (s wfd31Scanner) Scan(_ context.Context, in documentsecurity.ScanInput, _ documentsecurity.Limits) (documentsecurity.Verdict, error) {
	if s.err != nil {
		return documentsecurity.Verdict{}, s.err
	}
	return documentsecurity.Verdict{
		State: s.verdict, Scanner: "stub-av", ScannerVersion: "v1",
		Reason: "fixture", Derivative: append([]byte(nil), in.Bytes...),
	}, nil
}

func wfd31Limits() documentsecurity.Limits {
	return documentsecurity.Limits{MaxBytes: 1 << 20, MaxDerivativeBytes: 1 << 20, MaxCompressionRatio: 10}
}

func wfd31Scan(t *testing.T, registry *documentsecurity.Registry, id string, content []byte, verdict documentsecurity.State) {
	t.Helper()
	if _, err := registry.Scan(context.Background(), documentsecurity.Upload{
		ID: id, Name: id + ".pdf", ContentType: "application/pdf", Bytes: content,
	}, wfd31Limits(), wfd31Scanner{verdict: verdict}); err != nil {
		t.Fatalf("scan %s: %v", id, err)
	}
}

func wfd31Artifacts(t *testing.T) *documentsecurity.Registry {
	t.Helper()
	registry := documentsecurity.NewRegistry()
	wfd31Scan(t, registry, "inv-safe", []byte("%PDF-1.4 safe invoice bytes"), documentsecurity.Safe)
	wfd31Scan(t, registry, "inv-virus", []byte("%PDF-1.4 infected bytes"), documentsecurity.Unsafe)
	wfd31Scan(t, registry, "inv-text", []byte("plain text, not a pdf"), documentsecurity.Safe)
	if _, err := registry.Scan(context.Background(), documentsecurity.Upload{
		ID: "inv-broken", Name: "broken.pdf", ContentType: "application/pdf", Bytes: []byte("%PDF-1.4 broken"),
	}, wfd31Limits(), wfd31Scanner{err: errors.New("scanner offline")}); err != nil {
		t.Fatalf("scan broken: %v", err)
	}
	return registry
}

func wfd31Deps(store *custom.EventStore, resolver custom.RefReleaseResolver, artifacts *documentsecurity.Registry) custom.PublishDeps {
	return custom.PublishDeps{
		Store: store, Access: &custom.AccessDeps{Platform: custom.AllowPlatformPolicy()},
		Clock:     func() time.Time { return wfd31FixedTime },
		Sequences: custom.NewSequences(), RefReleases: resolver, Artifacts: artifacts,
	}
}

func wfd31Payload(tenant, object string, fields map[string]string) custom.RecordPayload {
	return custom.RecordPayload{
		Tenant: tenant, Principal: "alice", ObjectID: object, Fields: fields,
		IdempotencyKey: "key-" + object, Actor: "alice",
		Purpose: "procurement.operations", EvidenceDigest: "evidence-" + object,
	}
}

func wfd31OrderFields(number, country, invoice string) map[string]string {
	return map[string]string{
		"supplier": "acme-parts", "po_number": number, "country": country, "invoice": invoice,
	}
}

func TestTodo_WF_DATA_031(t *testing.T) {
	ctx := context.Background()
	release := wfd31CountryRelease(t)
	resolver := func(dataset, version string) (refdata.Release, error) { return release, nil }

	newRegistration := func(t *testing.T, def custom.CustomObjectDefinition) *custom.Registration {
		t.Helper()
		published, err := custom.RegisterGeneratedCapabilities(
			capability.NewRegistry(), "acme", def, "procurement.operations",
			wfd31Deps(custom.NewEventStore(), resolver, wfd31Artifacts(t)))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		return published
	}

	t.Run("sequence_assigns_formatted_numbers", func(t *testing.T) {
		published := newRegistration(t, wfd31Def(release))
		first, err := published.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("acme", "po-1", wfd31OrderFields("", "US", "inv-safe")))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if got := first.Fields["po_number"].Value; got != "PO-000001" {
			t.Fatalf("first number = %v, want PO-000001", got)
		}
		second, err := published.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("acme", "po-2", wfd31OrderFields("", "DE", "inv-safe")))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if got := second.Fields["po_number"].Value; got != "PO-000002" {
			t.Fatalf("second number = %v, want PO-000002", got)
		}
		explicit, err := published.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("acme", "po-legacy", wfd31OrderFields("PO-19990101", "US", "inv-safe")))
		if err != nil {
			t.Fatalf("explicit number must pass through for backfill: %v", err)
		}
		if got := explicit.Fields["po_number"].Value; got != "PO-19990101" {
			t.Fatalf("explicit number = %v", got)
		}
	})

	t.Run("sequence_counters_are_tenant_scoped", func(t *testing.T) {
		def := wfd31Def(release)
		shared := custom.NewSequences()
		acmeDeps := wfd31Deps(custom.NewEventStore(), resolver, wfd31Artifacts(t))
		acmeDeps.Sequences = shared
		globexDeps := wfd31Deps(custom.NewEventStore(), resolver, wfd31Artifacts(t))
		globexDeps.Sequences = shared
		published, err := custom.RegisterGeneratedCapabilities(
			capability.NewRegistry(), "acme", def, "procurement.operations", acmeDeps)
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		other, err := custom.RegisterGeneratedCapabilities(
			capability.NewRegistry(), "globex", def, "procurement.operations", globexDeps)
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		a, err := published.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("acme", "po-1", wfd31OrderFields("", "US", "inv-safe")))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		b, err := other.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("globex", "po-1", wfd31OrderFields("", "US", "inv-safe")))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if got := a.Fields["po_number"].Value; got != "PO-000001" {
			t.Fatalf("acme number = %v", got)
		}
		if got := b.Fields["po_number"].Value; got != "PO-000001" {
			t.Fatalf("globex shares the counter: %v", got)
		}
	})

	t.Run("reference_values_follow_the_governed_release", func(t *testing.T) {
		published := newRegistration(t, wfd31Def(release))
		if _, err := published.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("acme", "po-1", wfd31OrderFields("", "US", "inv-safe"))); err != nil {
			t.Fatalf("member value must be accepted: %v", err)
		}
		for code, want := range map[string]error{
			"ZZ": custom.ErrUnknownReferenceValue,
			"XX": custom.ErrUnknownReferenceValue,
			"FU": custom.ErrUnknownReferenceValue,
			"EX": custom.ErrUnknownReferenceValue,
			"":   custom.ErrUnknownReferenceValue,
		} {
			_, err := published.ExecuteRecord(ctx, custom.OperationCreate,
				wfd31Payload("acme", "po-"+code, wfd31OrderFields("", code, "inv-safe")))
			if !errors.Is(err, want) {
				t.Fatalf("code %q error = %v, want %v", code, err, want)
			}
		}
		withdrawn, err := refdata.WithdrawRelease(release, wfd31FixedTime)
		if err != nil {
			t.Fatalf("withdraw: %v", err)
		}
		resolverReturns := func(r refdata.Release) func(string, string) (refdata.Release, error) {
			return func(string, string) (refdata.Release, error) { return r, nil }
		}
		replaced, err := custom.RegisterGeneratedCapabilities(
			capability.NewRegistry(), "acme", wfd31Def(release), "procurement.operations",
			wfd31Deps(custom.NewEventStore(), resolverReturns(withdrawn), wfd31Artifacts(t)))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		if _, err := replaced.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("acme", "po-1", wfd31OrderFields("", "US", "inv-safe"))); !errors.Is(err, custom.ErrReferenceNotGoverned) {
			t.Fatalf("withdrawn release error = %v", err)
		}
		tampered := release
		tampered.Digest = wfd31Digest("tampered")
		spoofed, err := custom.RegisterGeneratedCapabilities(
			capability.NewRegistry(), "acme", wfd31Def(release), "procurement.operations",
			wfd31Deps(custom.NewEventStore(), resolverReturns(tampered), wfd31Artifacts(t)))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		if _, err := spoofed.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("acme", "po-1", wfd31OrderFields("", "US", "inv-safe"))); !errors.Is(err, custom.ErrReferenceNotGoverned) {
			t.Fatalf("digest mismatch error = %v", err)
		}
	})

	t.Run("attachments_pin_safe_scanned_derivatives", func(t *testing.T) {
		published := newRegistration(t, wfd31Def(release))
		created, err := published.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("acme", "po-1", wfd31OrderFields("", "US", "inv-safe")))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		pinned, ok := created.Fields["invoice"].Value.(string)
		if !ok || !strings.HasPrefix(pinned, "inv-safe@sha256:") {
			t.Fatalf("attachment value = %v, want a pinned derivative", created.Fields["invoice"].Value)
		}
		if _, err := custom.ParseAttachmentPin(pinned); err != nil {
			t.Fatalf("pinned value does not parse: %v", err)
		}
		for id, want := range map[string]error{
			"inv-virus":  custom.ErrAttachmentNotSafe,
			"inv-broken": custom.ErrAttachmentNotSafe,
			"inv-text":   custom.ErrAttachmentNotSafe,
			"inv-ghost":  custom.ErrAttachmentNotSafe,
			"":           custom.ErrAttachmentNotSafe,
		} {
			_, err := published.ExecuteRecord(ctx, custom.OperationCreate,
				wfd31Payload("acme", "po-"+id, wfd31OrderFields("", "US", id)))
			if !errors.Is(err, want) {
				t.Fatalf("artifact %q error = %v, want %v", id, err, want)
			}
		}
		readBack, err := published.ExecuteRead(ctx, wfd31Payload("acme", "po-1", nil))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if readBack.Fields["invoice"].Value != pinned {
			t.Fatalf("attachment pin did not round-trip: %v", readBack.Fields["invoice"].Value)
		}
	})

	t.Run("sequence_values_are_immutable_and_refs_may_move", func(t *testing.T) {
		published := newRegistration(t, wfd31Def(release))
		created, err := published.ExecuteRecord(ctx, custom.OperationCreate,
			wfd31Payload("acme", "po-1", wfd31OrderFields("", "US", "inv-safe")))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		number := created.Fields["po_number"].Value.(string)
		renumber := wfd31Payload("acme", "po-1", wfd31OrderFields("PO-999999", "US", "inv-safe"))
		renumber.IdempotencyKey = "key-po-1-renumber"
		renumber.ExpectedRevision = 1
		if _, err := published.ExecuteRecord(ctx, custom.OperationChange, renumber); !errors.Is(err, custom.ErrSequenceImmutable) {
			t.Fatalf("renumber error = %v", err)
		}
		move := wfd31Payload("acme", "po-1", wfd31OrderFields(number, "DE", "inv-safe"))
		move.IdempotencyKey = "key-po-1-move"
		move.ExpectedRevision = 1
		moved, err := published.ExecuteRecord(ctx, custom.OperationChange, move)
		if err != nil {
			t.Fatalf("reference may move to another member: %v", err)
		}
		if moved.Fields["country"].Value != "DE" || moved.Fields["po_number"].Value != number {
			t.Fatalf("change result is wrong: %+v", moved.Fields)
		}
	})

	t.Run("kind_configurations_are_validated_at_publish", func(t *testing.T) {
		base := wfd31Def(release)
		cases := map[string]func(*custom.CustomObjectDefinition){
			"unknown kind": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["supplier"]
				f.Kind = "teleport"
				d.Fields["supplier"] = f
			},
			"kind without string type": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["supplier"]
				f.Type = "bool"
				f.Kind = custom.FieldKindSequence
				f.Sequence = &custom.SequenceConfig{Format: "S-{seq}"}
				d.Fields["supplier"] = f
			},
			"sequence without config": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["supplier"]
				f.Kind = custom.FieldKindSequence
				d.Fields["supplier"] = f
			},
			"sequence with foreign config": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["po_number"]
				f.Reference = &custom.ReferenceConfig{DatasetID: "x", Version: "v", ReleaseDigest: wfd31Digest("x")}
				d.Fields["po_number"] = f
			},
			"plain with config": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["supplier"]
				f.Sequence = &custom.SequenceConfig{Format: "S-{seq}"}
				d.Fields["supplier"] = f
			},
			"format without counter": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["po_number"]
				f.Sequence = &custom.SequenceConfig{Format: "PO-NOCTR"}
				d.Fields["po_number"] = f
			},
			"format with two counters": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["po_number"]
				f.Sequence = &custom.SequenceConfig{Format: "{seq}-{seq}"}
				d.Fields["po_number"] = f
			},
			"format with bad width": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["po_number"]
				f.Sequence = &custom.SequenceConfig{Format: "PO-{seq:0}"}
				d.Fields["po_number"] = f
			},
			"format with unknown directive": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["po_number"]
				f.Sequence = &custom.SequenceConfig{Format: "PO-{date}"}
				d.Fields["po_number"] = f
			},
			"reference without pin": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["country"]
				f.Reference = &custom.ReferenceConfig{}
				d.Fields["country"] = f
			},
			"reference with malformed digest": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["country"]
				f.Reference = &custom.ReferenceConfig{DatasetID: "country", Version: "v3", ReleaseDigest: "nope"}
				d.Fields["country"] = f
			},
			"attachment without lattice label": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["invoice"]
				f.Classification.Classification = "SENSITIVE"
				d.Fields["invoice"] = f
			},
			"attachment with blank allowlist entry": func(d *custom.CustomObjectDefinition) {
				f := d.Fields["invoice"]
				f.Attachment = &custom.AttachmentConfig{AllowedContentTypes: []string{"  "}}
				d.Fields["invoice"] = f
			},
		}
		for name, mutate := range cases {
			def := base
			def.Fields = map[string]custom.FieldDefinition{}
			for fieldName, fieldDef := range base.Fields {
				def.Fields[fieldName] = fieldDef
			}
			mutate(&def)
			if _, err := custom.RegisterGeneratedCapabilities(
				capability.NewRegistry(), "acme", def, "procurement.operations",
				wfd31Deps(custom.NewEventStore(), resolver, wfd31Artifacts(t))); err == nil {
				t.Fatalf("%s: publish must be refused", name)
			}
		}
		withoutKinds := base
		withoutKinds.Fields = map[string]custom.FieldDefinition{
			"supplier": {Type: "string", Classification: wfd31Classification()},
		}
		if base.Digest() == withoutKinds.Digest() {
			t.Fatal("governed kinds must move the definition digest")
		}
		if wfd31Def(release).Digest() != base.Digest() {
			t.Fatal("identical definitions must digest identically")
		}
	})

	t.Run("external_kinds_require_their_resolvers", func(t *testing.T) {
		deps := wfd31Deps(custom.NewEventStore(), nil, wfd31Artifacts(t))
		if _, err := custom.RegisterGeneratedCapabilities(
			capability.NewRegistry(), "acme", wfd31Def(release), "procurement.operations", deps); err == nil {
			t.Fatal("reference fields without a resolver must be refused")
		}
		deps = wfd31Deps(custom.NewEventStore(), resolver, nil)
		if _, err := custom.RegisterGeneratedCapabilities(
			capability.NewRegistry(), "acme", wfd31Def(release), "procurement.operations", deps); err == nil {
			t.Fatal("attachment fields without a registry must be refused")
		}
		bare := wfd31Deps(custom.NewEventStore(), resolver, wfd31Artifacts(t))
		bare.Sequences = nil
		if _, err := custom.RegisterGeneratedCapabilities(
			capability.NewRegistry(), "acme", wfd31Def(release), "procurement.operations", bare); err != nil {
			t.Fatalf("sequences default to a fresh allocator: %v", err)
		}
	})
}

// TestTodo_WF_DATA_031_Property proves governed values behave: formats
// render deterministically with distinct values per number, reference
// validation matches membership exactly, pins round-trip, and malformed
// formats are rejected.
func TestTodo_WF_DATA_031_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(31))
	formats := []string{"PO-{seq}", "PO-{seq:6}", "{tenant}-REQ-{seq:3}", "{tenant}/REQ/{seq}"}
	tenants := []string{"acme", "globex", "initech"}

	t.Run("formats_render_deterministically", func(t *testing.T) {
		for i := 0; i < 300; i++ {
			format := formats[rng.Intn(len(formats))]
			tenant := tenants[rng.Intn(len(tenants))]
			start := uint64(1 + rng.Intn(99999))
			cfg := custom.SequenceConfig{Format: format, StartAt: start}
			first, err := custom.NewSequences().Next(tenant, "procurement", "PurchaseOrder", "po_number", cfg)
			if err != nil {
				t.Fatalf("case %d: %v", i, err)
			}
			again, err := custom.NewSequences().Next(tenant, "procurement", "PurchaseOrder", "po_number", cfg)
			if err != nil || again != first {
				t.Fatalf("case %d: render is not deterministic: %q vs %q", i, first, again)
			}
			shared := custom.NewSequences()
			a, err := shared.Next(tenant, "procurement", "PurchaseOrder", "po_number", cfg)
			if err != nil {
				t.Fatalf("case %d: %v", i, err)
			}
			b, err := shared.Next(tenant, "procurement", "PurchaseOrder", "po_number", cfg)
			if err != nil {
				t.Fatalf("case %d: %v", i, err)
			}
			if a == b {
				t.Fatalf("case %d: successive numbers collide: %q", i, a)
			}
			if strings.Contains(format, "{tenant}") && !strings.Contains(first, tenant) {
				t.Fatalf("case %d: tenant missing from %q", i, first)
			}
		}
	})

	t.Run("malformed_formats_are_rejected", func(t *testing.T) {
		for _, format := range []string{
			"", "PO-NOCTR", "{seq}-{seq}", "PO-{seq:0}", "PO-{seq:21}", "PO-{seq:x}",
			"PO-{date}", "PO-{seq", "PO-seq}", "PO-{se{q}}", "{tenant}",
		} {
			cfg := custom.SequenceConfig{Format: format}
			if _, err := custom.NewSequences().Next("acme", "ns", "kind", "field", cfg); err == nil {
				t.Fatalf("format %q must be refused", format)
			}
		}
	})

	t.Run("reference_validation_matches_membership", func(t *testing.T) {
		release := wfd31CountryRelease(t)
		pin := custom.ReferenceConfig{DatasetID: release.DatasetID, Version: release.Version, ReleaseDigest: release.Digest}
		members := map[string]bool{}
		for _, m := range release.Members {
			members[m.Code] = !m.Retired && !wfd31FixedTime.Before(m.ValidFrom) &&
				(m.ValidTo.IsZero() || wfd31FixedTime.Before(m.ValidTo))
		}
		codes := []string{"US", "DE", "XX", "FU", "EX", "ZZ", ""}
		for i := 0; i < 120; i++ {
			code := codes[rng.Intn(len(codes))]
			err := custom.ValidateReferenceValue(release, pin, code, wfd31FixedTime)
			if members[code] && err != nil {
				t.Fatalf("case %d: member %q refused: %v", i, code, err)
			}
			if !members[code] && !errors.Is(err, custom.ErrUnknownReferenceValue) {
				t.Fatalf("case %d: code %q error = %v", i, code, err)
			}
		}
		withdrawn, err := refdata.WithdrawRelease(release, wfd31FixedTime)
		if err != nil {
			t.Fatalf("withdraw: %v", err)
		}
		if err := custom.ValidateReferenceValue(withdrawn, pin, "US", wfd31FixedTime); !errors.Is(err, custom.ErrReferenceNotGoverned) {
			t.Fatalf("withdrawn release error = %v", err)
		}
	})

	t.Run("pins_round_trip", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			pin := custom.AttachmentPin{
				ArtifactID:       fmt.Sprintf("doc-%d", rng.Intn(1000)),
				DerivativeDigest: "sha256:" + wfd31Digest(fmt.Sprintf("bytes-%d", i)),
				Classification:   "CONFIDENTIAL",
			}
			parsed, err := custom.ParseAttachmentPin(pin.String())
			if err != nil {
				t.Fatalf("case %d: %v", i, err)
			}
			if parsed.ArtifactID != pin.ArtifactID || parsed.DerivativeDigest != pin.DerivativeDigest {
				t.Fatalf("case %d: round trip lost data: %+v", i, parsed)
			}
		}
		for _, text := range []string{"", "no-at-sign", "@sha256:" + wfd31Digest("x"), "id@nope", "id@"} {
			if _, err := custom.ParseAttachmentPin(text); err == nil {
				t.Fatalf("pin %q must be refused", text)
			}
		}
	})
}

// TestTodo_WF_DATA_031_Race proves sequence values are never duplicated
// under concurrency, both at the allocator and through record creates.
func TestTodo_WF_DATA_031_Race(t *testing.T) {
	ctx := context.Background()

	t.Run("allocator_never_duplicates", func(t *testing.T) {
		const goroutines, perGoroutine = 16, 250
		sequences := custom.NewSequences()
		cfg := custom.SequenceConfig{Format: "P-{seq:6}"}
		values := make(chan string, goroutines*perGoroutine)
		errs := make(chan error, goroutines)
		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < perGoroutine; i++ {
					number, err := sequences.Next("acme", "procurement", "PurchaseOrder", "po_number", cfg)
					if err != nil {
						errs <- err
						return
					}
					values <- number
				}
			}()
		}
		wg.Wait()
		close(values)
		close(errs)
		for err := range errs {
			t.Fatalf("allocation: %v", err)
		}
		seen := map[string]bool{}
		for number := range values {
			if seen[number] {
				t.Fatalf("duplicate sequence value %q", number)
			}
			seen[number] = true
		}
		if len(seen) != goroutines*perGoroutine {
			t.Fatalf("allocated %d values, want %d", len(seen), goroutines*perGoroutine)
		}
	})

	t.Run("concurrent_creates_keep_unique_numbers", func(t *testing.T) {
		release := wfd31CountryRelease(t)
		resolver := func(string, string) (refdata.Release, error) { return release, nil }
		published, err := custom.RegisterGeneratedCapabilities(
			capability.NewRegistry(), "acme", wfd31Def(release), "procurement.operations",
			wfd31Deps(custom.NewEventStore(), resolver, wfd31Artifacts(t)))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		const goroutines, perGoroutine = 8, 25
		values := make(chan string, goroutines*perGoroutine)
		errs := make(chan error, goroutines*perGoroutine)
		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for i := 0; i < perGoroutine; i++ {
					object := fmt.Sprintf("po-%d-%d", g, i)
					payload := wfd31Payload("acme", object, wfd31OrderFields("", "US", "inv-safe"))
					result, err := published.ExecuteRecord(ctx, custom.OperationCreate, payload)
					if err != nil {
						errs <- err
						return
					}
					number, ok := result.Fields["po_number"].Value.(string)
					if !ok {
						errs <- fmt.Errorf("number is not text: %v", result.Fields["po_number"].Value)
						return
					}
					values <- number
				}
			}(g)
		}
		wg.Wait()
		close(values)
		close(errs)
		for err := range errs {
			t.Fatalf("create: %v", err)
		}
		seen := map[string]bool{}
		for number := range values {
			if seen[number] {
				t.Fatalf("duplicate record number %q", number)
			}
			seen[number] = true
		}
		if len(seen) != goroutines*perGoroutine {
			t.Fatalf("created %d numbers, want %d", len(seen), goroutines*perGoroutine)
		}
	})
}
