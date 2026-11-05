// TestTodo_WF_DATA_002 proves a tenant publishes extension definitions
// bound to core HR and organizational entity kinds; extension records are
// effective-dated revisions keyed by the core entity reference and stored
// as custom-object events, never as core-table columns.
package custom_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/custom"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/refdata"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var wfd2FixedTime = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

func wfd2Classification(domain string) custom.FieldClassification {
	return custom.FieldClassification{
		AuthZDomain: domain, Classification: "CONFIDENTIAL",
		ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL",
	}
}

func wfd2Def() custom.CustomObjectDefinition {
	return custom.CustomObjectDefinition{
		Kind: "WorkerExtension", Namespace: "acme", Version: 1,
		Fields: map[string]custom.FieldDefinition{
			"nickname": {Type: "string", Classification: wfd2Classification("tenant.profile")},
			"badge_no": {
				Type: "string", Classification: wfd2Classification("tenant.identity"),
				Kind: custom.FieldKindSequence, Sequence: &custom.SequenceConfig{Format: "ACME-{seq:6}"},
			},
		},
	}
}

func wfd2Entity(kind, tag string) values.EntityRef {
	return values.EntityRef{Tenant: "acme", Kind: values.Kind(kind), Id: wfd2UUID(kind + "/" + tag)}
}

func wfd2ForeignEntity(tenant, kind, tag string) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind(kind), Id: wfd2UUID(kind + "/" + tag)}
}

// wfd2UUID derives a deterministic canonical UUID for one entity tag so
// fixtures satisfy entity-id validation without fixtures files.
func wfd2UUID(tag string) string {
	sum := sha256.Sum256([]byte("wfd2:" + tag))
	return fmt.Sprintf("11111111-1111-4111-8111-%x", sum[:6])
}

func wfd2Intervals() map[values.EntityRef]custom.EntityInterval {
	return map[values.EntityRef]custom.EntityInterval{
		wfd2Entity("worker", "w-1"): {
			Start: time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC), Found: true,
		},
		wfd2Entity("employment", "e-1"): {
			Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), Found: true,
		},
	}
}

func wfd2Provider(intervals map[values.EntityRef]custom.EntityInterval) custom.IntervalProvider {
	return func(_ context.Context, entity values.EntityRef) (custom.EntityInterval, error) {
		if iv, ok := intervals[entity]; ok {
			return iv, nil
		}
		return custom.EntityInterval{}, nil
	}
}

func wfd2Records(intervals map[values.EntityRef]custom.EntityInterval) (*custom.ExtensionRecords, *custom.EventStore) {
	store := custom.NewEventStore()
	records := custom.NewExtensionRecords(custom.ExtensionDeps{
		Store: store, Access: &custom.AccessDeps{Platform: custom.AllowPlatformPolicy()},
		Clock:     func() time.Time { return wfd2FixedTime },
		Intervals: wfd2Provider(intervals), Values: custom.FieldValueDeps{Sequences: custom.NewSequences()},
	})
	return records, store
}

func wfd2Span(t *testing.T, start, end time.Time) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewInstantInterval(values.NewInstant(start), values.NewInstant(end))
	if err != nil {
		t.Fatalf("span: %v", err)
	}
	return iv
}

func wfd2OpenSpan(t *testing.T, start time.Time) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewOpenInstantInterval(values.NewInstant(start))
	if err != nil {
		t.Fatalf("open span: %v", err)
	}
	return iv
}

func wfd2Write(span values.EffectiveInterval, key string) custom.WriteOpts {
	return custom.WriteOpts{
		Effective: span, Actor: "alice", Purpose: "hr.admin",
		EvidenceDigest: "evidence-" + key, IdempotencyKey: key,
		Principal: "alice", Scopes: []string{"hr.admin"},
	}
}

func wfd2Fields(pairs ...string) map[string]custom.TypedValue {
	out := map[string]custom.TypedValue{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i]] = custom.TypedValue{FieldName: pairs[i], Type: "string", Value: pairs[i+1]}
	}
	return out
}

func wfd2Digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func wfd2Release(t *testing.T) refdata.Release {
	t.Helper()
	member := func(code, label string, retired bool, from, to time.Time) refdata.Member {
		t.Helper()
		m, err := refdata.NewMember(code, label, false, retired, from, to, nil)
		if err != nil {
			t.Fatalf("member %s: %v", code, err)
		}
		return m
	}
	draft, err := refdata.NewRelease(refdata.Release{
		DatasetID: "work_site", Version: "v2",
		Source: refdata.SourceEvidence{
			SourceRef: "hr.sites", SourceVersion: "2025-01", SourceDigest: wfd2Digest("source"),
			SignatureRef: "sig:hr", LicenseRef: "license:hr", Coverage: "global",
			RetrievedAt: wfd2FixedTime,
		},
		SchemaDigest: wfd2Digest("schema"),
		Members: []refdata.Member{
			member("SF", "San Francisco", false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}),
			member("NYC", "New York", false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}),
			member("XX", "Retired", true, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}),
		},
		Applicability: []string{"hr"}, ConsumerRefs: []string{"custom.worker_extension"},
		AffectedIntents: []string{"intent:hr"}, EffectiveFrom: wfd2FixedTime.AddDate(-1, 0, 0),
		KnownAt: wfd2FixedTime,
	})
	if err != nil {
		t.Fatalf("seal release: %v", err)
	}
	validation, err := refdata.ValidateRelease(draft, wfd2FixedTime)
	if err != nil {
		t.Fatalf("validate release: %v", err)
	}
	validated, err := refdata.MarkValidated(draft, validation)
	if err != nil {
		t.Fatalf("mark validated: %v", err)
	}
	published, err := refdata.PublishRelease(validated, validation, wfd2FixedTime)
	if err != nil {
		t.Fatalf("publish release: %v", err)
	}
	return published
}

type wfd2Scanner struct {
	verdict documentsecurity.State
	err     error
}

func (s wfd2Scanner) Scan(_ context.Context, in documentsecurity.ScanInput, _ documentsecurity.Limits) (documentsecurity.Verdict, error) {
	if s.err != nil {
		return documentsecurity.Verdict{}, s.err
	}
	return documentsecurity.Verdict{
		State: s.verdict, Scanner: "stub-av", ScannerVersion: "v1",
		Reason: "fixture", Derivative: append([]byte(nil), in.Bytes...),
	}, nil
}

func wfd2Artifacts(t *testing.T) *documentsecurity.Registry {
	t.Helper()
	registry := documentsecurity.NewRegistry()
	limits := documentsecurity.Limits{MaxBytes: 1 << 20, MaxDerivativeBytes: 1 << 20, MaxCompressionRatio: 10}
	scan := func(id string, content string, verdict documentsecurity.State) {
		t.Helper()
		if _, err := registry.Scan(context.Background(), documentsecurity.Upload{
			ID: id, Name: id + ".pdf", ContentType: "application/pdf", Bytes: []byte(content),
		}, limits, wfd2Scanner{verdict: verdict}); err != nil {
			t.Fatalf("scan %s: %v", id, err)
		}
	}
	scan("inv-safe", "%PDF-1.4 safe invoice bytes", documentsecurity.Safe)
	scan("inv-virus", "%PDF-1.4 infected bytes", documentsecurity.Unsafe)
	return registry
}

func TestTodo_WF_DATA_002(t *testing.T) {
	ctx := context.Background()

	publish := func(t *testing.T, def custom.CustomObjectDefinition, kind custom.EntityKind) custom.ExtensionDefinition {
		t.Helper()
		published, err := custom.PublishExtension(def, kind, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		return published
	}

	t.Run("publish_binds_entity_kind", func(t *testing.T) {
		published := publish(t, wfd2Def(), custom.EntityWorker)
		if published.EntityKind != custom.EntityWorker {
			t.Fatalf("entity kind = %q, want worker", published.EntityKind)
		}
		if len(published.Policy.Fields) != 2 {
			t.Fatalf("policy fields = %d, want 2", len(published.Policy.Fields))
		}
	})

	t.Run("publish_refuses_unknown_kind", func(t *testing.T) {
		if _, err := custom.PublishExtension(wfd2Def(), custom.EntityKind("contractor"), custom.FieldValueDeps{}); !errors.Is(err, custom.ErrExtensionDefinition) {
			t.Fatalf("unknown kind err = %v, want ErrExtensionDefinition", err)
		}
	})

	t.Run("publish_refuses_shadowed_names", func(t *testing.T) {
		def := wfd2Def()
		def.Namespace = "employment"
		if _, err := custom.PublishExtension(def, custom.EntityWorker, custom.FieldValueDeps{}); !errors.Is(err, custom.ErrExtensionDefinition) {
			t.Fatalf("shadowed namespace err = %v, want ErrExtensionDefinition", err)
		}
		def = wfd2Def()
		def.Namespace = "Employment"
		if _, err := custom.PublishExtension(def, custom.EntityWorker, custom.FieldValueDeps{}); !errors.Is(err, custom.ErrExtensionDefinition) {
			t.Fatalf("mixed-case shadowed namespace err = %v, want ErrExtensionDefinition", err)
		}
		def = wfd2Def()
		def.Kind = "worker"
		if _, err := custom.PublishExtension(def, custom.EntityWorker, custom.FieldValueDeps{}); !errors.Is(err, custom.ErrExtensionDefinition) {
			t.Fatalf("core kind name err = %v, want ErrExtensionDefinition", err)
		}
		def = wfd2Def()
		def.Fields["employment.hire_date"] = custom.FieldDefinition{Type: "string", Classification: wfd2Classification("tenant.profile")}
		if _, err := custom.PublishExtension(def, custom.EntityWorker, custom.FieldValueDeps{}); !errors.Is(err, custom.ErrExtensionDefinition) {
			t.Fatalf("shadowed field err = %v, want ErrExtensionDefinition", err)
		}
	})

	t.Run("publish_refuses_every_platform_field", func(t *testing.T) {
		for _, field := range people.AllFields() {
			def := wfd2Def()
			def.Fields[string(field)] = custom.FieldDefinition{Type: "string", Classification: wfd2Classification("tenant.profile")}
			if _, err := custom.PublishExtension(def, custom.EntityWorker, custom.FieldValueDeps{}); !errors.Is(err, custom.ErrExtensionDefinition) {
				t.Fatalf("platform field %q must be refused, err = %v", field, err)
			}
		}
		if len(people.AllFields()) == 0 {
			t.Fatal("people.AllFields is empty; the shadow coupling proves nothing")
		}
	})

	t.Run("publish_refuses_unclassified", func(t *testing.T) {
		def := wfd2Def()
		def.Fields["nickname"] = custom.FieldDefinition{Type: "string"}
		if _, err := custom.PublishExtension(def, custom.EntityWorker, custom.FieldValueDeps{}); !errors.Is(err, custom.ErrDanglingField) {
			t.Fatalf("unclassified err = %v, want ErrDanglingField", err)
		}
	})

	t.Run("publish_requires_kind_resolvers", func(t *testing.T) {
		release := wfd2Release(t)
		def := wfd2Def()
		def.Fields["site"] = custom.FieldDefinition{
			Type: "string", Classification: wfd2Classification("tenant.sites"),
			Kind: custom.FieldKindReference, Reference: &custom.ReferenceConfig{
				DatasetID: release.DatasetID, Version: release.Version, ReleaseDigest: release.Digest,
			},
		}
		if _, err := custom.PublishExtension(def, custom.EntityWorker, custom.FieldValueDeps{}); !errors.Is(err, custom.ErrInvalidFieldKind) {
			t.Fatalf("reference without resolver err = %v, want ErrInvalidFieldKind", err)
		}
		def = wfd2Def()
		def.Fields["photo"] = custom.FieldDefinition{
			Type: "string", Classification: wfd2Classification("tenant.media"),
			Kind: custom.FieldKindAttachment, Attachment: &custom.AttachmentConfig{},
		}
		if _, err := custom.PublishExtension(def, custom.EntityWorker, custom.FieldValueDeps{}); !errors.Is(err, custom.ErrInvalidFieldKind) {
			t.Fatalf("attachment without registry err = %v, want ErrInvalidFieldKind", err)
		}
	})

	t.Run("create_change_read_roundtrip", func(t *testing.T) {
		records, _ := wfd2Records(wfd2Intervals())
		published := publish(t, wfd2Def(), custom.EntityWorker)
		entity := wfd2Entity("worker", "w-1")
		span := wfd2Span(t,
			time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC))

		created, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span, "create-1"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.Revision != 1 || created.Receipt.Event.Type != custom.EventObjectCreated {
			t.Fatalf("created = rev %d type %s, want rev 1 CUSTOM_OBJECT_CREATED",
				created.Revision, created.Receipt.Event.Type)
		}
		if got := created.Fields["badge_no"].Value; got != "ACME-000001" {
			t.Fatalf("badge_no = %v, want ACME-000001", got)
		}
		if created.Entity != entity {
			t.Fatalf("entity = %+v, want %+v", created.Entity, entity)
		}

		changed, err := records.Change(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Alice", "badge_no", "ACME-000001"), func() custom.WriteOpts {
				opts := wfd2Write(span, "change-1")
				opts.ExpectedRevision = 1
				return opts
			}())
		if err != nil {
			t.Fatalf("change: %v", err)
		}
		if changed.Revision != 2 || changed.Receipt.Event.Type != custom.EventObjectChanged {
			t.Fatalf("changed = rev %d type %s, want rev 2 CUSTOM_OBJECT_CHANGED",
				changed.Revision, changed.Receipt.Event.Type)
		}

		read, err := records.Read(ctx, "acme", published, entity, custom.ReadOpts{
			Principal: "alice", Purpose: "hr.admin",
			Grants: map[string]bool{"tenant.profile": true, "tenant.identity": true},
		})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if read.Revision != 2 || read.Fields["nickname"].Value != "Alice" {
			t.Fatalf("read = rev %d nickname %v, want rev 2 Alice", read.Revision, read.Fields["nickname"].Value)
		}
		if read.Receipt.Event.EventID != "" {
			t.Fatal("reads carry no receipt")
		}
	})

	t.Run("governed_writes_resolve", func(t *testing.T) {
		release := wfd2Release(t)
		resolver := func(dataset, version string) (refdata.Release, error) { return release, nil }
		store := custom.NewEventStore()
		records := custom.NewExtensionRecords(custom.ExtensionDeps{
			Store: store, Access: &custom.AccessDeps{Platform: custom.AllowPlatformPolicy()},
			Clock: func() time.Time { return wfd2FixedTime }, Intervals: wfd2Provider(wfd2Intervals()),
			Values: custom.FieldValueDeps{Sequences: custom.NewSequences(), RefReleases: resolver, Artifacts: wfd2Artifacts(t)},
		})
		def := wfd2Def()
		def.Fields["site"] = custom.FieldDefinition{
			Type: "string", Classification: wfd2Classification("tenant.sites"),
			Kind: custom.FieldKindReference, Reference: &custom.ReferenceConfig{
				DatasetID: release.DatasetID, Version: release.Version, ReleaseDigest: release.Digest,
			},
		}
		def.Fields["photo"] = custom.FieldDefinition{
			Type: "string", Classification: wfd2Classification("tenant.media"),
			Kind: custom.FieldKindAttachment, Attachment: &custom.AttachmentConfig{
				AllowedContentTypes: []string{"application/pdf"},
			},
		}
		published, err := custom.PublishExtension(def, custom.EntityWorker, custom.FieldValueDeps{
			Sequences: custom.NewSequences(), RefReleases: resolver, Artifacts: wfd2Artifacts(t),
		})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		entity := wfd2Entity("worker", "w-1")
		span := wfd2OpenSpan(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))
		created, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", "", "site", "SF", "photo", "inv-safe"),
			wfd2Write(span, "create-gov"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if got := created.Fields["site"].Value; got != "SF" {
			t.Fatalf("site = %v, want SF", got)
		}
		pin, ok := created.Fields["photo"].Value.(string)
		if !ok || !strings.HasPrefix(pin, "inv-safe@sha256:") {
			t.Fatalf("photo = %v, want inv-safe@sha256:...", created.Fields["photo"].Value)
		}
	})

	t.Run("events_live_in_custom_store_only", func(t *testing.T) {
		records, store := wfd2Records(wfd2Intervals())
		published := publish(t, wfd2Def(), custom.EntityWorker)
		entity := wfd2Entity("worker", "w-1")
		span := wfd2OpenSpan(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))
		if _, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span, "create-1")); err != nil {
			t.Fatalf("create: %v", err)
		}
		opts := wfd2Write(span, "change-1")
		opts.ExpectedRevision = 1
		if _, err := records.Change(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Alice", "badge_no", "ACME-000001"), opts); err != nil {
			t.Fatalf("change: %v", err)
		}
		events := store.Events("acme", "WorkerExtension", entity.String())
		if len(events) != 2 || events[0].Revision != 1 || events[1].Revision != 2 {
			t.Fatalf("extension stream holds %d events, want contiguous revisions 1..2", len(events))
		}
		if events[1].PreviousEventID != events[0].EventID {
			t.Fatal("extension stream must chain")
		}
		outbox := store.Outbox()
		if len(outbox) != 2 || outbox[0].Effect != "custom-object.create" || outbox[1].Effect != "custom-object.change" {
			t.Fatalf("outbox = %+v, want create then change intents", outbox)
		}
		for _, entry := range outbox {
			if !strings.Contains(entry.OrderingKey, "WorkerExtension") {
				t.Fatalf("outbox ordering key %q escapes the custom stream", entry.OrderingKey)
			}
		}
	})
}

func TestTodo_WF_DATA_002_Property(t *testing.T) {
	ctx := context.Background()
	at := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}

	t.Run("record_never_escapes_entity_interval", func(t *testing.T) {
		rows := []struct {
			name   string
			entity values.EntityRef
			start  time.Time
			end    time.Time
			open   bool
			want   bool
		}{
			{"bounded_inside", wfd2Entity("employment", "e-1"), at(2024, 6, 1), at(2025, 6, 1), false, true},
			{"exact_equal", wfd2Entity("employment", "e-1"), at(2024, 1, 1), at(2026, 12, 31), false, true},
			{"touch_start", wfd2Entity("employment", "e-1"), at(2024, 1, 1), at(2025, 1, 1), false, true},
			{"touch_end", wfd2Entity("employment", "e-1"), at(2025, 1, 1), at(2026, 12, 31), false, true},
			{"predates_by_second", wfd2Entity("employment", "e-1"), at(2023, 12, 31).Add(23*time.Hour + 59*time.Minute + 59*time.Second), at(2025, 1, 1), false, false},
			{"outlives_by_day", wfd2Entity("employment", "e-1"), at(2025, 1, 1), at(2027, 1, 1), false, false},
			{"open_escapes_bounded_end", wfd2Entity("employment", "e-1"), at(2025, 1, 1), time.Time{}, true, false},
			{"bounded_inside_open", wfd2Entity("worker", "w-1"), at(2024, 1, 1), at(2025, 1, 1), false, true},
			{"open_equal_start", wfd2Entity("worker", "w-1"), at(2023, 6, 1), time.Time{}, true, true},
			{"open_inside_open", wfd2Entity("worker", "w-1"), at(2025, 1, 1), time.Time{}, true, true},
			{"predates_open_entity", wfd2Entity("worker", "w-1"), at(2020, 1, 1), at(2024, 1, 1), false, false},
			{"bounded_ends_after_open", wfd2Entity("worker", "w-1"), at(2023, 6, 1), at(2030, 1, 1), false, true},
		}
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) {
				records, _ := wfd2Records(wfd2Intervals())
				def := wfd2Def()
				if row.entity.Kind == "employment" {
					def.Kind = "EmploymentExtension"
				}
				kind := custom.EntityWorker
				if row.entity.Kind == "employment" {
					kind = custom.EntityEmployment
				}
				published, err := custom.PublishExtension(def, kind, custom.FieldValueDeps{Sequences: custom.NewSequences()})
				if err != nil {
					t.Fatalf("publish: %v", err)
				}
				var span values.EffectiveInterval
				if row.open {
					span = wfd2OpenSpan(t, row.start)
				} else {
					span = wfd2Span(t, row.start, row.end)
				}
				created, err := records.Create(ctx, "acme", published, row.entity,
					wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span, "row"))
				if row.want {
					if err != nil {
						t.Fatalf("contained span must commit: %v", err)
					}
					if created.Revision != 1 {
						t.Fatalf("revision = %d, want 1", created.Revision)
					}
					return
				}
				if !errors.Is(err, custom.ErrIntervalViolation) {
					t.Fatalf("escaping span err = %v, want ErrIntervalViolation", err)
				}
			})
		}
	})

	t.Run("kinds_are_independent_streams", func(t *testing.T) {
		intervals := wfd2Intervals()
		intervals[wfd2Entity("worker", "w-9")] = custom.EntityInterval{Start: at(2024, 1, 1), Found: true}
		intervals[wfd2Entity("employment", "e-9")] = custom.EntityInterval{
			Start: at(2024, 1, 1), End: at(2026, 1, 1), Found: true,
		}
		records, store := wfd2Records(intervals)
		workerDef, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish worker: %v", err)
		}
		employmentDef := wfd2Def()
		employmentDef.Kind = "EmploymentExtension"
		employmentPublished, err := custom.PublishExtension(employmentDef, custom.EntityEmployment, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish employment: %v", err)
		}
		span := wfd2Span(t, at(2024, 6, 1), at(2025, 6, 1))
		worker, employment := wfd2Entity("worker", "w-9"), wfd2Entity("employment", "e-9")
		if _, err := records.Create(ctx, "acme", workerDef, worker,
			wfd2Fields("nickname", "W", "badge_no", ""), wfd2Write(span, "w")); err != nil {
			t.Fatalf("create worker: %v", err)
		}
		if _, err := records.Create(ctx, "acme", employmentPublished, employment,
			wfd2Fields("nickname", "E", "badge_no", ""), wfd2Write(span, "e")); err != nil {
			t.Fatalf("create employment: %v", err)
		}
		opts := wfd2Write(span, "w2")
		opts.ExpectedRevision = 1
		if _, err := records.Change(ctx, "acme", workerDef, worker,
			wfd2Fields("nickname", "W2", "badge_no", "ACME-000001"), opts); err != nil {
			t.Fatalf("change worker: %v", err)
		}
		if got := len(store.Events("acme", "WorkerExtension", worker.String())); got != 2 {
			t.Fatalf("worker stream = %d events, want 2", got)
		}
		if got := len(store.Events("acme", "EmploymentExtension", employment.String())); got != 1 {
			t.Fatalf("employment stream = %d events, want 1", got)
		}
	})

	t.Run("tenants_are_independent_streams", func(t *testing.T) {
		intervals := wfd2Intervals()
		globex := wfd2ForeignEntity("globex", "worker", "w-1")
		intervals[globex] = custom.EntityInterval{Start: at(2023, 6, 1), Found: true}
		records, _ := wfd2Records(intervals)
		published, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		span := wfd2OpenSpan(t, at(2024, 3, 1))
		if _, err := records.Create(ctx, "acme", published, wfd2Entity("worker", "w-1"),
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span, "a")); err != nil {
			t.Fatalf("create acme: %v", err)
		}
		if _, err := records.Read(ctx, "globex", published, globex, custom.ReadOpts{
			Principal: "mallory", Purpose: "hr.admin",
			Grants: map[string]bool{"tenant.profile": true, "tenant.identity": true},
		}); !errors.Is(err, custom.ErrNotFound) {
			t.Fatalf("globex read before write err = %v, want ErrNotFound", err)
		}
		created, err := records.Create(ctx, "globex", published, globex,
			wfd2Fields("nickname", "Gx", "badge_no", ""), wfd2Write(span, "g"))
		if err != nil {
			t.Fatalf("create globex: %v", err)
		}
		if created.Revision != 1 {
			t.Fatalf("globex revision = %d, want 1", created.Revision)
		}
		if got := created.Fields["badge_no"].Value; got != "ACME-000001" {
			t.Fatalf("globex badge_no = %v, want ACME-000001 (tenant-scoped sequences)", got)
		}
	})

	t.Run("sequence_numbers_never_duplicate", func(t *testing.T) {
		intervals := wfd2Intervals()
		ids := []string{"w-20", "w-21", "w-22", "w-23", "w-24", "w-25", "w-26", "w-27"}
		for _, id := range ids {
			intervals[wfd2Entity("worker", id)] = custom.EntityInterval{Start: at(2024, 1, 1), Found: true}
		}
		records, _ := wfd2Records(intervals)
		published, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		span := wfd2OpenSpan(t, at(2024, 3, 1))
		seen := map[string]bool{}
		for _, id := range ids {
			created, err := records.Create(ctx, "acme", published, wfd2Entity("worker", id),
				wfd2Fields("nickname", id, "badge_no", ""), wfd2Write(span, "seq-"+id))
			if err != nil {
				t.Fatalf("create %s: %v", id, err)
			}
			number, _ := created.Fields["badge_no"].Value.(string)
			if number == "" || seen[number] {
				t.Fatalf("badge_no %q for %s is empty or duplicated", number, id)
			}
			seen[number] = true
		}
		if len(seen) != len(ids) {
			t.Fatalf("distinct numbers = %d, want %d", len(seen), len(ids))
		}
	})
}

func TestTodo_WF_DATA_002_Security(t *testing.T) {
	ctx := context.Background()

	fixture := func(t *testing.T) (*custom.ExtensionRecords, custom.ExtensionDefinition, values.EntityRef) {
		t.Helper()
		records, _ := wfd2Records(wfd2Intervals())
		published, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		entity := wfd2Entity("worker", "w-1")
		span := wfd2OpenSpan(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))
		if _, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span, "sec")); err != nil {
			t.Fatalf("create: %v", err)
		}
		return records, published, entity
	}

	t.Run("fields_default_to_hidden", func(t *testing.T) {
		records, published, entity := fixture(t)
		read, err := records.Read(ctx, "acme", published, entity, custom.ReadOpts{
			Principal: "mallory", Purpose: "hr.admin",
		})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(read.Fields) != 0 {
			t.Fatalf("ungranted read exposes %d fields, want 0", len(read.Fields))
		}
		if read.Revision != 1 {
			t.Fatalf("revision = %d, want 1", read.Revision)
		}
	})

	t.Run("partial_grants_show_granted_fields_only", func(t *testing.T) {
		records, published, entity := fixture(t)
		read, err := records.Read(ctx, "acme", published, entity, custom.ReadOpts{
			Principal: "bob", Purpose: "hr.admin", Grants: map[string]bool{"tenant.profile": true},
		})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if read.Fields["nickname"].Value != "Al" {
			t.Fatalf("nickname = %v, want Al", read.Fields["nickname"].Value)
		}
		if _, ok := read.Fields["badge_no"]; ok {
			t.Fatalf("badge_no is present without its grant: %+v", read.Fields)
		}
	})

	t.Run("explicit_ungranted_field_is_denied", func(t *testing.T) {
		records, published, entity := fixture(t)
		if _, err := records.Read(ctx, "acme", published, entity, custom.ReadOpts{
			Principal: "bob", Purpose: "hr.admin",
			Grants: map[string]bool{"tenant.profile": true}, Fields: []string{"badge_no"},
		}); !errors.Is(err, custom.ErrCapabilityDenied) {
			t.Fatalf("ungranted field err = %v, want ErrCapabilityDenied", err)
		}
	})

	t.Run("cross_tenant_reference_is_refused", func(t *testing.T) {
		records, published, _ := fixture(t)
		foreign := wfd2ForeignEntity("globex", "worker", "w-1")
		if _, err := records.Read(ctx, "acme", published, foreign, custom.ReadOpts{
			Principal: "mallory", Purpose: "hr.admin",
			Grants: map[string]bool{"tenant.profile": true, "tenant.identity": true},
		}); !errors.Is(err, custom.ErrUnknownEntity) {
			t.Fatalf("cross-tenant err = %v, want ErrUnknownEntity", err)
		}
	})

	t.Run("wrong_kind_reference_is_refused", func(t *testing.T) {
		records, published, _ := fixture(t)
		other := wfd2Entity("employment", "e-1")
		if _, err := records.Read(ctx, "acme", published, other, custom.ReadOpts{
			Principal: "mallory", Purpose: "hr.admin",
			Grants: map[string]bool{"tenant.profile": true, "tenant.identity": true},
		}); !errors.Is(err, custom.ErrUnknownEntity) {
			t.Fatalf("wrong-kind err = %v, want ErrUnknownEntity", err)
		}
	})
}

func TestTodo_WF_DATA_002_Fault(t *testing.T) {
	ctx := context.Background()
	span := func(t *testing.T) values.EffectiveInterval {
		t.Helper()
		return wfd2OpenSpan(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))
	}
	fixture := func(t *testing.T) (*custom.ExtensionRecords, custom.ExtensionDefinition) {
		t.Helper()
		records, _ := wfd2Records(wfd2Intervals())
		published, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		return records, published
	}

	t.Run("unknown_entity_is_refused", func(t *testing.T) {
		records, published := fixture(t)
		if _, err := records.Create(ctx, "acme", published, wfd2Entity("worker", "w-ghost"),
			wfd2Fields("nickname", "G", "badge_no", ""), wfd2Write(span(t), "ghost")); !errors.Is(err, custom.ErrUnknownEntity) {
			t.Fatalf("ghost err = %v, want ErrUnknownEntity", err)
		}
	})

	t.Run("provider_errors_pass_through", func(t *testing.T) {
		raw := errors.New("core aggregates offline")
		store := custom.NewEventStore()
		records := custom.NewExtensionRecords(custom.ExtensionDeps{
			Store: store, Access: &custom.AccessDeps{Platform: custom.AllowPlatformPolicy()},
			Clock: func() time.Time { return wfd2FixedTime },
			Intervals: func(_ context.Context, _ values.EntityRef) (custom.EntityInterval, error) {
				return custom.EntityInterval{}, raw
			},
			Values: custom.FieldValueDeps{Sequences: custom.NewSequences()},
		})
		published, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		_, err = records.Create(ctx, "acme", published, wfd2Entity("worker", "w-1"),
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "outage"))
		if !errors.Is(err, raw) {
			t.Fatalf("provider err = %v, want the raw provider error", err)
		}
	})

	t.Run("stale_revision_is_refused", func(t *testing.T) {
		records, published := fixture(t)
		entity := wfd2Entity("worker", "w-1")
		if _, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "stale")); err != nil {
			t.Fatalf("create: %v", err)
		}
		opts := wfd2Write(span(t), "stale-change")
		opts.ExpectedRevision = 7
		if _, err := records.Change(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Alice", "badge_no", "ACME-000001"), opts); !errors.Is(err, custom.ErrStaleRevision) {
			t.Fatalf("stale err = %v, want ErrStaleRevision", err)
		}
	})

	t.Run("duplicate_create_conflicts_on_head", func(t *testing.T) {
		records, published := fixture(t)
		entity := wfd2Entity("worker", "w-1")
		if _, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "dup-1")); err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "dup-2")); !errors.Is(err, custom.ErrStaleHead) {
			t.Fatalf("duplicate err = %v, want ErrStaleHead", err)
		}
	})

	t.Run("write_preconditions_are_refused", func(t *testing.T) {
		records, published := fixture(t)
		entity := wfd2Entity("worker", "w-1")
		fields := wfd2Fields("nickname", "Al", "badge_no", "")
		opts := wfd2Write(span(t), "")
		if _, err := records.Create(ctx, "acme", published, entity, fields, opts); !errors.Is(err, custom.ErrInvalidMutation) {
			t.Fatalf("missing key err = %v, want ErrInvalidMutation", err)
		}
		opts = wfd2Write(span(t), "rev")
		opts.ExpectedRevision = 1
		if _, err := records.Create(ctx, "acme", published, entity, fields, opts); !errors.Is(err, custom.ErrInvalidMutation) {
			t.Fatalf("create with revision err = %v, want ErrInvalidMutation", err)
		}
		if _, err := records.Change(ctx, "acme", published, entity, fields, wfd2Write(span(t), "no-rev")); !errors.Is(err, custom.ErrInvalidMutation) {
			t.Fatalf("change without revision err = %v, want ErrInvalidMutation", err)
		}
		opts = wfd2Write(span(t), "no-actor")
		opts.Actor = ""
		if _, err := records.Create(ctx, "acme", published, entity, fields, opts); !errors.Is(err, custom.ErrInvalidMutation) {
			t.Fatalf("missing actor err = %v, want ErrInvalidMutation", err)
		}
		opts = wfd2Write(values.EffectiveInterval{}, "no-span")
		if _, err := records.Create(ctx, "acme", published, entity, fields, opts); !errors.Is(err, custom.ErrInvalidMutation) {
			t.Fatalf("missing effective err = %v, want ErrInvalidMutation", err)
		}
	})

	t.Run("sequence_rewrite_is_refused", func(t *testing.T) {
		records, published := fixture(t)
		entity := wfd2Entity("worker", "w-1")
		if _, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "im")); err != nil {
			t.Fatalf("create: %v", err)
		}
		opts := wfd2Write(span(t), "im-change")
		opts.ExpectedRevision = 1
		if _, err := records.Change(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Alice", "badge_no", "ACME-999999"), opts); !errors.Is(err, custom.ErrSequenceImmutable) {
			t.Fatalf("rewrite err = %v, want ErrSequenceImmutable", err)
		}
	})

	t.Run("canceled_context_refuses_fresh_writes", func(t *testing.T) {
		records, published := fixture(t)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := records.Create(canceled, "acme", published, wfd2Entity("worker", "w-1"),
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "cancel")); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled err = %v, want context.Canceled", err)
		}
	})
}

func TestTodo_WF_DATA_002_Recovery(t *testing.T) {
	ctx := context.Background()
	span := func(t *testing.T) values.EffectiveInterval {
		t.Helper()
		return wfd2OpenSpan(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))
	}

	t.Run("retried_create_replays_without_reallocating", func(t *testing.T) {
		records, store := wfd2Records(wfd2Intervals())
		published, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		entity := wfd2Entity("worker", "w-1")
		fields := wfd2Fields("nickname", "Al", "badge_no", "")
		first, err := records.Create(ctx, "acme", published, entity, fields, wfd2Write(span(t), "retry"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		replayed, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "retry"))
		if err != nil {
			t.Fatalf("retry: %v", err)
		}
		if !replayed.Replayed || !replayed.Receipt.Replayed {
			t.Fatal("retry must replay")
		}
		if replayed.Receipt.Event.EventID != first.Receipt.Event.EventID {
			t.Fatal("retry must return the identical receipt")
		}
		if replayed.Fields["badge_no"].Value != "ACME-000001" {
			t.Fatalf("badge_no = %v, want ACME-000001 (no reallocation)", replayed.Fields["badge_no"].Value)
		}
		if got := len(store.Events("acme", "WorkerExtension", entity.String())); got != 1 {
			t.Fatalf("stream holds %d events after retry, want 1", got)
		}
	})

	t.Run("retried_change_replays", func(t *testing.T) {
		records, store := wfd2Records(wfd2Intervals())
		published, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		entity := wfd2Entity("worker", "w-1")
		if _, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "rc")); err != nil {
			t.Fatalf("create: %v", err)
		}
		change := func() custom.WriteOpts {
			opts := wfd2Write(span(t), "rc-change")
			opts.ExpectedRevision = 1
			return opts
		}
		fields := wfd2Fields("nickname", "Alice", "badge_no", "ACME-000001")
		first, err := records.Change(ctx, "acme", published, entity, fields, change())
		if err != nil {
			t.Fatalf("change: %v", err)
		}
		replayed, err := records.Change(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Alice", "badge_no", "ACME-000001"), change())
		if err != nil {
			t.Fatalf("retry: %v", err)
		}
		if !replayed.Replayed || replayed.Receipt.Event.EventID != first.Receipt.Event.EventID {
			t.Fatal("change retry must replay the identical receipt")
		}
		if got := len(store.Events("acme", "WorkerExtension", entity.String())); got != 2 {
			t.Fatalf("stream holds %d events after change retry, want 2", got)
		}
	})

	t.Run("journal_keys_scope_by_operation", func(t *testing.T) {
		records, _ := wfd2Records(wfd2Intervals())
		published, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		entity := wfd2Entity("worker", "w-1")
		if _, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "shared")); err != nil {
			t.Fatalf("create: %v", err)
		}
		opts := wfd2Write(span(t), "shared")
		opts.ExpectedRevision = 1
		changed, err := records.Change(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Alice", "badge_no", "ACME-000001"), opts)
		if err != nil {
			t.Fatalf("change reusing the create key must commit, not replay: %v", err)
		}
		if changed.Revision != 2 || changed.Replayed {
			t.Fatalf("changed = rev %d replayed %v, want rev 2 fresh", changed.Revision, changed.Replayed)
		}
	})

	t.Run("rebuild_verifies_projection", func(t *testing.T) {
		records, store := wfd2Records(wfd2Intervals())
		published, err := custom.PublishExtension(wfd2Def(), custom.EntityWorker, custom.FieldValueDeps{Sequences: custom.NewSequences()})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		entity := wfd2Entity("worker", "w-1")
		if _, err := records.Create(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Al", "badge_no", ""), wfd2Write(span(t), "rb")); err != nil {
			t.Fatalf("create: %v", err)
		}
		opts := wfd2Write(span(t), "rb-change")
		opts.ExpectedRevision = 1
		if _, err := records.Change(ctx, "acme", published, entity,
			wfd2Fields("nickname", "Alice", "badge_no", "ACME-000001"), opts); err != nil {
			t.Fatalf("change: %v", err)
		}
		report, err := store.Rebuild(ctx, "acme", "WorkerExtension", entity.String())
		if err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		if report.EventCount != 2 || report.Retired {
			t.Fatalf("rebuild = %+v, want 2 live events", report)
		}
		live := store.Events("acme", "WorkerExtension", entity.String())
		if report.Digest != live[len(live)-1].Digest {
			t.Fatal("rebuild digest must match the live projection")
		}
	})
}
