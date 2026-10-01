package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/personacompfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type nativeCompensationDirectoryFake struct {
	roles    []string
	subjects []agentgate.Subject
	fields   []authz.FieldID
	err      error
	calls    int
}

func (f *nativeCompensationDirectoryFake) CurrentRoles(context.Context, values.TenantId, string) ([]string, error) {
	f.calls++
	return f.roles, f.err
}
func (f *nativeCompensationDirectoryFake) CurrentOrganizationScopes(context.Context, values.TenantId, string, []string) ([]string, error) {
	return []string{"org"}, f.err
}
func (f *nativeCompensationDirectoryFake) CurrentSubjects(context.Context, values.TenantId, string, string, []string, []string) ([]agentgate.Subject, error) {
	return f.subjects, f.err
}
func (f *nativeCompensationDirectoryFake) CurrentFields(context.Context, *trust.Principal, string, []string, []string, []agentgate.Subject) ([]authz.FieldID, error) {
	return f.fields, f.err
}

type nativeCompensationFactsFake struct {
	snapshot personacompfacts.Snapshot
	calls    int
	err      error
	asOf     values.Instant
}

func (f *nativeCompensationFactsFake) CompensationAt(_ context.Context, _ values.EntityRef, asOf values.Instant) (personacompfacts.Snapshot, error) {
	f.calls++
	f.asOf = asOf
	return f.snapshot, f.err
}

type nativeCompensationWorkersFake struct {
	set   people.FactSet
	calls int
	query people.FactQuery
}

func (f *nativeCompensationWorkersFake) WorkerFactsAt(_ context.Context, query people.FactQuery) (people.FactSet, error) {
	f.calls++
	f.query = query
	return f.set, nil
}

type nativeCompensationBandsFake struct {
	record rewards.BandRecord
	calls  int
	query  rewards.BandQuery
	err    error
}

func (f *nativeCompensationBandsFake) LookupBand(_ context.Context, query rewards.BandQuery) (rewards.BandRecord, error) {
	f.calls++
	f.query = query
	return f.record, f.err
}

type nativeCompensationBeginFake struct{}

func (nativeCompensationBeginFake) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("unexpected database access")
}

func nativeCompensationFixture(t *testing.T) (*NativePersonaCompensationSource, context.Context, values.EntityRef) {
	t.Helper()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	worker := values.EntityRef{Tenant: "tenant-a", Kind: people.KindWorker, Id: "00000000-0000-0000-0000-000000000100"}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: worker.Tenant, Subject: "invoker", SubjectKind: trust.SubjectKindHuman, Roles: []string{"old-token-role"}, Purposes: []string{authz.PurposeCompensationReview}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "verified-credential"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewOpaqueRevision("compensation-package", []byte("persisted-revision"))
	if err != nil {
		t.Fatal(err)
	}
	from := values.NewInstant(now.AddDate(0, -1, 0))
	known, err := values.NewKnownAt(from)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(from)
	if err != nil {
		t.Fatal(err)
	}
	instantInterval, err := values.NewOpenInstantInterval(from)
	if err != nil {
		t.Fatal(err)
	}
	date, err := personaCompensationDate(from)
	if err != nil {
		t.Fatal(err)
	}
	dateInterval, err := values.NewOpenLocalDateInterval(date, values.CalendarRef{Ref: "calendar", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	authority := evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcmnext.compensation", PolicyRef: "source-authority/v1"}
	provenance := evidence.Provenance{Source: authority.System, EvidenceRef: "persisted-row-digest", RecordedAt: recorded}
	money := func(amount string, scale int32) values.Money {
		t.Helper()
		result, err := values.NewMoney(amount, "USD", scale, values.RoundingExactRequired)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	facts := &nativeCompensationFactsFake{snapshot: personacompfacts.Snapshot{Worker: worker, Revision: revision, Components: []personacompfacts.Component{{ID: "base", Kind: "BASE_PAY", Amount: money("8000.0000", 4), Frequency: "MONTHLY", Effective: instantInterval, KnownAt: known, Authority: authority, Provenance: provenance}}}}
	workers := &nativeCompensationWorkersFake{set: people.FactSet{Worker: worker, Exists: true, Watermark: revision}}
	for field, text := range map[people.FieldID]string{people.FieldJobCode: "ENG", people.FieldGrade: "G7", people.FieldPayZone: "US", people.FieldFTE: "1.0000"} {
		workers.set.Facts = append(workers.set.Facts, people.Fact{Field: field, Value: values.Value(text), Effective: dateInterval, KnownAt: known, Revision: revision, Authority: authority, Provenance: provenance})
	}
	bands := &nativeCompensationBandsFake{record: rewards.BandRecord{Band: payband.Band{ID: "stored-band", Version: "catalog/v1", Scope: payband.Scope{JobCode: "ENG", Grade: "G7", PayZone: "US"}, Minimum: money("80000.00", 2), Midpoint: money("100000.00", 2), Maximum: money("120000.00", 2)}, CatalogVersion: "catalog/v1", Authority: authority, Provenance: provenance}}
	directory := &nativeCompensationDirectoryFake{roles: []string{"manager"}, subjects: []agentgate.Subject{{Ref: worker, Organization: authz.OrgUnitRef{Tenant: worker.Tenant, ID: "org"}}}, fields: []authz.FieldID{authz.FieldBaseSalary}}
	return &NativePersonaCompensationSource{facts: facts, workers: workers, bands: bands, directory: directory, annualization: rewards.DefaultAnnualization(), now: func() time.Time { return now }}, trust.WithPrincipal(context.Background(), principal), worker
}

func TestTodo_AGENTP_023_CompensationNative_CanonicalSalaryAndFTE(t *testing.T) {
	source, ctx, worker := nativeCompensationFixture(t)
	asOf := values.NewInstant(source.now())
	result, err := source.PayBandPosition(ctx, worker, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if result.Position.Amount.Amount().String() != "96000.00" || result.Position.CompaRatio.String() != "0.9600" || result.Position.BandID != "stored-band" || result.CatalogVersion != "catalog/v1" || result.InputsDigest == "" || result.ResultDigest == "" || !result.Effects.IsZero() {
		t.Fatalf("canonical evaluation = %#v", result)
	}
	workers := source.workers.(*nativeCompensationWorkersFake)
	if workers.query.AsOf.KnownAt.Instant() != asOf || source.facts.(*nativeCompensationFactsFake).asOf != asOf {
		t.Fatal("read coordinates diverged")
	}
	for i := range workers.set.Facts {
		if workers.set.Facts[i].Field == people.FieldFTE {
			workers.set.Facts[i].Value = values.Value("0.5000")
		}
	}
	partTime, err := source.PayBandPosition(ctx, worker, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if partTime.Position.Amount.Amount().String() != "48000.00" || partTime.Position.CompaRatio.String() != "0.4800" {
		t.Fatalf("canonical FTE did not govern: %#v", partTime)
	}
	if source.directory.(*nativeCompensationDirectoryFake).calls != 2 {
		t.Fatal("current authority was cached")
	}
}

func TestTodo_AGENTP_023_CompensationNative_SecurityBeforeFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*NativePersonaCompensationSource, context.Context, *values.EntityRef) context.Context
	}{
		{"missing_principal", func(_ *NativePersonaCompensationSource, _ context.Context, _ *values.EntityRef) context.Context {
			return context.Background()
		}},
		{"unauthorized_purpose", func(s *NativePersonaCompensationSource, _ context.Context, worker *values.EntityRef) context.Context {
			return nativeCompensationPrincipalContext(t, s, worker.Tenant, trust.SubjectKindHuman, []string{authz.PurposeSelfService})
		}},
		{"agent_without_human_invoker", func(s *NativePersonaCompensationSource, _ context.Context, worker *values.EntityRef) context.Context {
			return nativeCompensationPrincipalContext(t, s, worker.Tenant, trust.SubjectKindAgent, []string{authz.PurposeCompensationReview})
		}},
		{"cross_tenant", func(_ *NativePersonaCompensationSource, ctx context.Context, worker *values.EntityRef) context.Context {
			worker.Tenant = "tenant-b"
			return ctx
		}},
		{"wrong_subject_kind", func(_ *NativePersonaCompensationSource, ctx context.Context, worker *values.EntityRef) context.Context {
			worker.Kind = "position"
			return ctx
		}},
		{"revoked_role", func(s *NativePersonaCompensationSource, ctx context.Context, _ *values.EntityRef) context.Context {
			s.directory.(*nativeCompensationDirectoryFake).roles = nil
			return ctx
		}},
		{"revoked_target", func(s *NativePersonaCompensationSource, ctx context.Context, _ *values.EntityRef) context.Context {
			s.directory.(*nativeCompensationDirectoryFake).subjects = nil
			return ctx
		}},
		{"withheld_compensation", func(s *NativePersonaCompensationSource, ctx context.Context, _ *values.EntityRef) context.Context {
			s.directory.(*nativeCompensationDirectoryFake).fields = nil
			return ctx
		}},
		{"directory_failed", func(s *NativePersonaCompensationSource, ctx context.Context, _ *values.EntityRef) context.Context {
			s.directory.(*nativeCompensationDirectoryFake).err = errors.New("directory down")
			return ctx
		}},
		{"expired_principal", func(s *NativePersonaCompensationSource, ctx context.Context, _ *values.EntityRef) context.Context {
			original := s.now()
			s.now = func() time.Time { return original.Add(2 * time.Hour) }
			return ctx
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, ctx, worker := nativeCompensationFixture(t)
			ctx = test.mutate(source, ctx, &worker)
			if _, err := source.PayBandPosition(ctx, worker, values.NewInstant(source.now())); !errors.Is(err, ErrPersonaCompensationUnavailable) {
				t.Fatalf("denial = %v", err)
			}
			if source.facts.(*nativeCompensationFactsFake).calls != 0 || source.workers.(*nativeCompensationWorkersFake).calls != 0 || source.bands.(*nativeCompensationBandsFake).calls != 0 {
				t.Fatal("protected facts read before current authority")
			}
		})
	}
}

func nativeCompensationPrincipalContext(t *testing.T, source *NativePersonaCompensationSource, tenant values.TenantId, kind trust.SubjectKind, purposes []string) context.Context {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: "invoker", SubjectKind: kind, Roles: []string{"comp_admin"}, Purposes: purposes, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: source.now().Add(-time.Hour), ExpiresAt: source.now().Add(time.Hour), CredentialDigest: "verified-credential"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

func TestTodo_AGENTP_023_CompensationNative_RejectsWrongOrFutureFacts(t *testing.T) {
	for _, name := range []string{"wrong_worker", "unknown_salary", "future_salary", "future_placement", "future_band", "duplicate_base", "wrong_band", "unknown_frequency", "future_coordinate"} {
		t.Run(name, func(t *testing.T) {
			s, ctx, worker := nativeCompensationFixture(t)
			asOf := values.NewInstant(s.now())
			facts, workers, bands := s.facts.(*nativeCompensationFactsFake), s.workers.(*nativeCompensationWorkersFake), s.bands.(*nativeCompensationBandsFake)
			future := values.NewInstant(s.now().Add(time.Hour))
			switch name {
			case "wrong_worker":
				facts.snapshot.Worker.Id = "wrong-worker"
			case "unknown_salary":
				facts.snapshot.Components = nil
			case "future_salary":
				facts.snapshot.Components[0].KnownAt, _ = values.NewKnownAt(future)
			case "future_placement":
				workers.set.Facts[0].KnownAt, _ = values.NewKnownAt(future)
			case "future_band":
				bands.record.Provenance.RecordedAt, _ = values.NewRecordedAt(future)
			case "duplicate_base":
				facts.snapshot.Components = append(facts.snapshot.Components, facts.snapshot.Components[0])
			case "wrong_band":
				bands.record.Band.Scope.Grade = "other-grade"
			case "unknown_frequency":
				facts.snapshot.Components[0].Frequency = "WEEKLY"
			case "future_coordinate":
				asOf = future
			}
			if _, err := s.PayBandPosition(ctx, worker, asOf); !errors.Is(err, ErrPersonaCompensationUnavailable) {
				t.Fatalf("bad facts error = %v", err)
			}
		})
	}
}

func TestTodo_AGENTP_023_CompensationNative_ScenarioUsesAuthoritativeBaseline(t *testing.T) {
	source, ctx, worker := nativeCompensationFixture(t)
	facts := source.facts.(*nativeCompensationFactsFake)
	bonus := facts.snapshot.Components[0]
	bonus.ID, bonus.Kind, bonus.Frequency = "bonus", "BONUS_TARGET", "ANNUAL"
	bonus.Amount, _ = values.NewMoney("4800.0000", "USD", 4, values.RoundingExactRequired)
	facts.snapshot.Components = append(facts.snapshot.Components, bonus)
	effective, _ := values.NewLocalDate(2026, 9, 2)
	proposalMoney, _ := values.NewMoney("9000.00", "USD", 2, values.RoundingExactRequired)
	input := rewards.SimulateCompensationInput{Tenant: worker.Tenant, Subject: worker, EffectiveDate: effective, Proposed: rewards.CompensationSnapshot{Base: values.Value(proposalMoney), PayBasis: rewards.PayBasisMonthlySalary, BonusTargetPercent: values.Absent[values.Percentage](), EffectiveDate: effective, Watermark: facts.snapshot.Revision, Complete: true}, Band: &rewards.BandQuery{Tenant: "forged-tenant"}}
	// Current and Annualization are deliberately invalid model input. The
	// native source must replace them before domain validation and evaluation.
	result, err := source.SimulateCompensation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Current.AnnualizedBase.Amount().String() != "96000.00" || result.Current.AnnualizedBonusTarget.Amount().String() != "4800.00" || result.Proposed.AnnualizedBase.Amount().String() != "108000.00" || result.Band.Evaluation.Query.Tenant != worker.Tenant || result.Band.Evaluation.Query.AsOf != effective || !result.Effects.IsZero() || string(result.CurrentSnapshotMark.Canonical()) != string(facts.snapshot.Revision.Canonical()) {
		t.Fatalf("scenario trusted supplied current facts: %#v", result)
	}
	if source.bands.(*nativeCompensationBandsFake).calls != 1 {
		t.Fatal("scenario did not pin one native band read")
	}
	baseDelta, present := result.Delta.BaseAmount.Get()
	if !present || result.Current.Base.Amount().String() != "8000.0000" || result.Proposed.Base.Amount().String() != "9000.0000" || baseDelta.Amount().String() != "1000.0000" {
		t.Fatalf("basis amounts were not normalized exactly: current=%s proposed=%s delta=%s", result.Current.Base, result.Proposed.Base, baseDelta)
	}
	input.FX = &rewards.PinnedFXConversion{}
	if _, err := source.SimulateCompensation(ctx, input); !errors.Is(err, ErrPersonaCompensationUnavailable) {
		t.Fatalf("caller FX error = %v", err)
	}
	input.FX = nil
	facts.snapshot.Components[1].Kind = "ALLOWANCE"
	if _, err := source.SimulateCompensation(ctx, input); !errors.Is(err, ErrPersonaCompensationUnavailable) {
		t.Fatalf("unsupported material component error = %v", err)
	}
}

func TestTodo_AGENTP_023_CompensationNative_ScenarioPreservesHourlyPrecision(t *testing.T) {
	source, ctx, worker := nativeCompensationFixture(t)
	facts := source.facts.(*nativeCompensationFactsFake)
	facts.snapshot.Components[0].Amount, _ = values.NewMoney("25.1234", "USD", 4, values.RoundingExactRequired)
	facts.snapshot.Components[0].Frequency = "HOURLY"
	effective, _ := values.NewLocalDate(2026, 9, 2)
	proposed, _ := values.NewMoney("25.13", "USD", 2, values.RoundingExactRequired)
	input := rewards.SimulateCompensationInput{Tenant: worker.Tenant, Subject: worker, EffectiveDate: effective, Proposed: rewards.CompensationSnapshot{Base: values.Value(proposed), PayBasis: rewards.PayBasisHourly, BonusTargetPercent: values.Absent[values.Percentage](), EffectiveDate: effective, Watermark: facts.snapshot.Revision, Complete: true}}
	result, err := source.SimulateCompensation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	baseDelta, present := result.Delta.BaseAmount.Get()
	if !present || result.Current.Base.Amount().String() != "25.1234" || result.Proposed.Base.Amount().String() != "25.1300" || baseDelta.Amount().String() != "0.0066" || result.Current.AnnualizedBase.Amount().String() != "52256.67" || result.Proposed.AnnualizedBase.Amount().String() != "52270.40" || !result.Effects.IsZero() {
		t.Fatalf("hourly precision changed: current=%#v proposed=%#v delta=%s", result.Current, result.Proposed, baseDelta)
	}
}

func TestTodo_AGENTP_023_CompensationNative_BandAndConstructor(t *testing.T) {
	source, ctx, worker := nativeCompensationFixture(t)
	date, _ := personaCompensationDate(values.NewInstant(source.now()))
	query := rewards.BandQuery{Tenant: worker.Tenant, JobCode: "ENG", Grade: "G7", PayZone: "US", Currency: "USD", AsOf: date}
	band, err := source.LookupBand(ctx, query)
	if err != nil || band.Band.ID != "stored-band" {
		t.Fatalf("band = %#v, %v", band, err)
	}
	if _, err := source.LookupBand(context.Background(), query); !errors.Is(err, ErrPersonaCompensationUnavailable) {
		t.Fatalf("unauthenticated band = %v", err)
	}
	if _, err := NewNativePersonaCompensationSource(NativePersonaCompensationConfig{}); !errors.Is(err, ErrPersonaCompensationUnavailable) {
		t.Fatalf("missing source = %v", err)
	}
	config := NativePersonaCompensationConfig{DB: nativeCompensationBeginFake{}, TenantUUID: func(values.TenantId) uuid.UUID { return uuid.New() }, WorkerFacts: source.workers, CatalogVersion: "production-band-version", Annualization: rewards.DefaultAnnualization()}
	configured, err := NewNativePersonaCompensationSource(config)
	if err != nil || configured == nil || configured.now == nil {
		t.Fatalf("native constructor = %#v, %v", configured, err)
	}
}
