package agentstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_021_CanonicalRoutePayload(t *testing.T) {
	first, firstDigest, err := CanonicalPersonaModelRoutePayload([]byte(`{"z":2,"model_digest":"sha256:` + strings.Repeat("a", 64) + `","a":{"b":1,"a":true}}`))
	if err != nil {
		t.Fatalf("canonicalize route: %v", err)
	}
	second, secondDigest, err := CanonicalPersonaModelRoutePayload([]byte(`{"a":{"a":true,"b":1},"model_digest":"sha256:` + strings.Repeat("a", 64) + `","z":2}`))
	if err != nil {
		t.Fatalf("canonicalize reordered route: %v", err)
	}
	if string(first) != string(second) || firstDigest != secondDigest {
		t.Fatalf("equivalent objects differ: %s / %s; %s / %s", first, second, firstDigest, secondDigest)
	}
	expected := sha256.Sum256(first)
	if firstDigest != "sha256:"+hex.EncodeToString(expected[:]) {
		t.Fatalf("digest %q does not hash canonical bytes", firstDigest)
	}
	for _, invalid := range []string{`[]`, `{"a":1,"a":2}`, `{"n":1.0}`, `{"n":1e2}`, `{} {}`, `not-json`} {
		if _, _, err := CanonicalPersonaModelRoutePayload([]byte(invalid)); !errors.Is(err, ErrPersonaRoutePayload) {
			t.Errorf("canonicalize %q error=%v, want ErrPersonaRoutePayload", invalid, err)
		}
	}
}

func TestTodo_AGENTP_021_PublisherRequiresTrustedQualification(t *testing.T) {
	if _, err := NewPersonaModelRoutePublisher(nil, func(values.TenantId) uuid.UUID { return uuid.New() }, qualificationFixture{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil database error=%v, want ErrInvalidConfig", err)
	}
	if _, err := NewPersonaModelRoutePublisher(emptyBeginner{}, nil, qualificationFixture{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil tenant mapper error=%v, want ErrInvalidConfig", err)
	}
	if _, err := NewPersonaModelRoutePublisher(emptyBeginner{}, func(values.TenantId) uuid.UUID { return uuid.New() }, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil qualification authority error=%v, want ErrInvalidConfig", err)
	}
	if _, _, err := CanonicalPersonaModelRoutePayload([]byte(`{"model_digest":"sha256:` + strings.Repeat("a", 64) + `"}`)); err != nil {
		t.Fatalf("valid model digest object should be canonicalizable: %v", err)
	}
}

func TestTodo_AGENTP_021_PublishTenantScopedQualifiedImmutablePolicy(t *testing.T) {
	tenantID := uuid.New()
	modelDigest := "sha256:" + strings.Repeat("a", 64)
	raw := publicationRoutePayload(modelDigest)
	qualification := PersonaRouteQualification{RunID: "eval-run", TenantID: "tenant-a", PersonaID: "policy-helper", PersonaVersion: 1,
		ProfileDigest: "sha256:" + strings.Repeat("b", 64), ModelDigest: modelDigest, SuiteDigest: "sha256:" + strings.Repeat("c", 64),
		RunDigest: "sha256:" + strings.Repeat("d", 64), Passed: true, Fresh: true, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	beginner := &publisherFixtureBeginner{tx: &publisherFixtureTx{maxRevision: 0}}
	qualified := qualificationFixture{value: qualification}
	publisher, err := NewPersonaModelRoutePublisher(beginner, func(values.TenantId) uuid.UUID { return tenantID }, qualified)
	if err != nil {
		t.Fatalf("construct publisher: %v", err)
	}
	policyPayload := []byte(`{"id":"model-routing","version":1,"schema_version":1,"purpose":"persona.reply","limits":{"cost_micros":250}}`)
	_, policyDigest, err := CanonicalPersonaModelRoutePayload(policyPayload)
	if err != nil {
		t.Fatalf("canonicalize publication payload: %v", err)
	}
	in := PersonaModelRoutePublication{Tenant: "tenant-a", LegalEntityID: "entity-a", PolicyID: "model-routing", PolicyVersion: 1, SchemaVersion: 1,
		PolicyDigest: policyDigest, Revision: 1, EffectiveFrom: time.Now().UTC().Truncate(time.Second), PersonaID: "policy-helper",
		PersonaVersion: 1, ProfileDigest: qualification.ProfileDigest, EvaluationRunID: "eval-run", RoutePayload: raw, PolicyPayload: policyPayload}
	got, err := publisher.PublishPersonaModelRoutePolicy(context.Background(), in)
	if err != nil {
		t.Fatalf("publish qualified route policy: %v", err)
	}
	_, want, err := CanonicalPersonaModelRoutePayload(raw)
	if err != nil || got != want {
		t.Fatalf("published digest=%q, want %q (canonicalization error %v)", got, want, err)
	}
	if !beginner.tx.committed || beginner.tx.insertCount != 1 || beginner.tx.tenantSet != tenantID.String() {
		t.Fatalf("publish effects: committed=%v inserts=%d tenant=%q", beginner.tx.committed, beginner.tx.insertCount, beginner.tx.tenantSet)
	}
	if beginner.tx.insertDigest != got || beginner.tx.insertRun != in.EvaluationRunID || beginner.tx.insertModelDigest != modelDigest {
		t.Fatalf("insert evidence: payload=%q eval=%q model=%q", beginner.tx.insertDigest, beginner.tx.insertRun, beginner.tx.insertModelDigest)
	}
	if got == policyDigest || beginner.tx.insertPolicyDigest != policyDigest || beginner.tx.insertPolicyPayload == "" {
		t.Fatalf("semantic and route integrity bindings lost: policy=%s route=%s payload=%s", beginner.tx.insertPolicyDigest, got, beginner.tx.insertPolicyPayload)
	}
	if beginner.tx.insertUntil == nil || !beginner.tx.insertUntil.Equal(qualification.ExpiresAt) {
		t.Fatalf("route qualification expiry lost: until=%v evidence=%v", beginner.tx.insertUntil, qualification.ExpiresAt)
	}
	outlives := in
	tooLate := qualification.ExpiresAt.Add(time.Second)
	outlives.EffectiveUntil = &tooLate
	if _, err := publisher.PublishPersonaModelRoutePolicy(context.Background(), outlives); !errors.Is(err, ErrPersonaRouteQualificationRequired) {
		t.Fatalf("route outlives qualification: %v", err)
	}
	for _, bad := range []struct {
		name    string
		payload []byte
		digest  string
	}{
		{"changed semantics", []byte(`{"id":"model-routing","version":1,"schema_version":1,"purpose":"other"}`), policyDigest},
		{"wrong identity", []byte(`{"id":"other","version":1,"schema_version":1}`), ""},
		{"missing policy", nil, policyDigest},
	} {
		t.Run(bad.name, func(t *testing.T) {
			altered := in
			altered.PolicyPayload = bad.payload
			altered.PolicyDigest = bad.digest
			if altered.PolicyDigest == "" {
				_, altered.PolicyDigest, _ = CanonicalPersonaModelRoutePayload(bad.payload)
			}
			if _, err := publisher.PublishPersonaModelRoutePolicy(context.Background(), altered); !errors.Is(err, ErrPersonaRoutePayload) {
				t.Fatalf("altered policy accepted: %v", err)
			}
		})
	}
}

func TestTodo_AGENTP_021_PublisherRejectsUnqualifiedAndOverlappingIntervals(t *testing.T) {
	for _, tc := range []struct {
		name          string
		qualification PersonaRouteQualification
		overlap       bool
		want          error
	}{
		{name: "missing pass", qualification: PersonaRouteQualification{Passed: false, Fresh: true}, want: ErrPersonaRouteQualificationRequired},
		{name: "stale evidence", qualification: PersonaRouteQualification{Passed: true, Fresh: false}, want: ErrPersonaRouteQualificationRequired},
		{name: "overlap", qualification: PersonaRouteQualification{Passed: true, Fresh: true, ExpiresAt: time.Now().UTC().Add(time.Hour)}, overlap: true, want: ErrPersonaModelRouteAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenantID := uuid.New()
			modelDigest := "sha256:" + strings.Repeat("a", 64)
			q := tc.qualification
			q.RunID, q.TenantID, q.PersonaID, q.PersonaVersion = "eval-run", "tenant-a", "policy-helper", 1
			q.ProfileDigest, q.ModelDigest = "sha256:"+strings.Repeat("b", 64), modelDigest
			q.SuiteDigest, q.RunDigest = "sha256:"+strings.Repeat("c", 64), "sha256:"+strings.Repeat("d", 64)
			if q.ExpiresAt.IsZero() {
				q.ExpiresAt = time.Now().UTC().Add(time.Hour)
			}
			tx := &publisherFixtureTx{maxRevision: 0, overlap: tc.overlap}
			publisher, err := NewPersonaModelRoutePublisher(&publisherFixtureBeginner{tx: tx}, func(values.TenantId) uuid.UUID { return tenantID }, qualificationFixture{value: q})
			if err != nil {
				t.Fatalf("construct publisher: %v", err)
			}
			in := validPublication(q.ProfileDigest, modelDigest)
			_, err = publisher.PublishPersonaModelRoutePolicy(context.Background(), in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("publish error=%v, want %v", err, tc.want)
			}
			if tx.insertCount != 0 || tx.committed {
				t.Fatalf("rejected policy persisted: inserts=%d committed=%v", tx.insertCount, tx.committed)
			}
		})
	}
}

func TestTodo_AGENTP_021_RoutePublisherRoleIsRestricted(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if err := Migrate(context.Background(), db.SQL); err != nil {
		t.Fatalf("migrate agent database: %v", err)
	}
	var login, bypass, createDB, createRole bool
	if err := db.SQL.QueryRow(`SELECT rolcanlogin,rolbypassrls,rolcreatedb,rolcreaterole FROM pg_roles WHERE rolname='hcmnext_persona_model_route_authority'`).Scan(&login, &bypass, &createDB, &createRole); err != nil {
		t.Fatalf("read publisher role: %v", err)
	}
	if login || bypass || createDB || createRole {
		t.Fatalf("publisher role overprivileged: login=%v bypass=%v createdb=%v createrole=%v", login, bypass, createDB, createRole)
	}
	var appInsert, publisherSelect, publisherInsert, publisherUpdate, publisherDelete bool
	if err := db.SQL.QueryRow(`SELECT has_table_privilege('hcmnext_agent_app','persona_model_route_policy','INSERT'),
		has_table_privilege('hcmnext_persona_model_route_authority','persona_model_route_policy','SELECT'),
		has_table_privilege('hcmnext_persona_model_route_authority','persona_model_route_policy','INSERT'),
		has_table_privilege('hcmnext_persona_model_route_authority','persona_model_route_policy','UPDATE'),
		has_table_privilege('hcmnext_persona_model_route_authority','persona_model_route_policy','DELETE')`).Scan(&appInsert, &publisherSelect, &publisherInsert, &publisherUpdate, &publisherDelete); err != nil {
		t.Fatalf("read publisher table privileges: %v", err)
	}
	if appInsert || !publisherSelect || !publisherInsert || publisherUpdate || publisherDelete {
		t.Fatalf("unexpected policy privileges app_insert=%v publisher=(select %v, insert %v, update %v, delete %v)", appInsert, publisherSelect, publisherInsert, publisherUpdate, publisherDelete)
	}
	var rlsEnabled, rlsForced bool
	if err := db.SQL.QueryRow(`SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='persona_model_route_policy'::regclass`).Scan(&rlsEnabled, &rlsForced); err != nil {
		t.Fatalf("read route policy RLS: %v", err)
	}
	if !rlsEnabled || !rlsForced {
		t.Fatalf("publisher table RLS enabled=%v forced=%v", rlsEnabled, rlsForced)
	}
}

func validPublication(profileDigest, modelDigest string) PersonaModelRoutePublication {
	payload := publicationRoutePayload(modelDigest)
	policy := []byte(`{"id":"model-routing","version":1,"schema_version":1,"purpose":"persona.reply"}`)
	_, digest, _ := CanonicalPersonaModelRoutePayload(policy)
	return PersonaModelRoutePublication{Tenant: "tenant-a", LegalEntityID: "entity-a", PolicyID: "model-routing", PolicyVersion: 1, SchemaVersion: 1,
		PolicyDigest: digest, Revision: 1, EffectiveFrom: time.Now().UTC().Truncate(time.Second), PersonaID: "policy-helper",
		PersonaVersion: 1, ProfileDigest: profileDigest, EvaluationRunID: "eval-run", RoutePayload: payload, PolicyPayload: policy}
}

func publicationRoutePayload(modelDigest string) []byte {
	manifestDigest := "sha256:" + strings.Repeat("e", 64)
	return []byte(`{"model_digest":"` + modelDigest + `","route":{"Pin":{"AgentVersionDigest":"` + manifestDigest + `"},"Task":{"AgentVersionDigest":"` + manifestDigest + `"}}}`)
}

type qualificationFixture struct {
	value PersonaRouteQualification
	err   error
}

func (f qualificationFixture) ResolvePersonaRouteQualification(context.Context, dbport.Tx, values.TenantId, string, string, int64, string) (PersonaRouteQualification, error) {
	return f.value, f.err
}

type emptyBeginner struct{}

func (emptyBeginner) Begin(context.Context) (dbport.Tx, error) {
	return nil, fmt.Errorf("no fixture transaction")
}

type publisherFixtureBeginner struct{ tx *publisherFixtureTx }

func (b *publisherFixtureBeginner) Begin(context.Context) (dbport.Tx, error) { return b.tx, nil }

type publisherFixtureTx struct {
	committed                                  bool
	maxRevision                                int64
	overlap                                    bool
	tenantSet                                  string
	insertCount                                int
	insertDigest, insertRun, insertModelDigest string
	insertPolicyDigest, insertPolicyPayload    string
	insertUntil                                *time.Time
}

func (tx *publisherFixtureTx) Exec(_ context.Context, query string, args ...any) (int64, error) {
	switch {
	case strings.Contains(query, "set_config"):
		tx.tenantSet = args[1].(string)
	case strings.Contains(query, "pg_advisory_xact_lock"):
		if strings.ContainsRune(args[0].(string), '\x00') {
			return 0, errors.New("PostgreSQL text disallows NUL lock domains")
		}
	case strings.Contains(query, "INSERT INTO persona_model_route_deployment"):
		tx.insertCount++
		tx.insertDigest = args[10].(string)
		tx.insertRun = args[11].(string)
		tx.insertModelDigest = args[12].(string)
		tx.insertPolicyDigest = args[5].(string)
		tx.insertPolicyPayload = args[13].(string)
		tx.insertUntil = args[8].(*time.Time)
	}
	return 1, nil
}
func (tx *publisherFixtureTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (tx *publisherFixtureTx) QueryRow(_ context.Context, query string, _ ...any) dbport.Row {
	if strings.Contains(query, "COALESCE(MAX(revision)") {
		return publisherFixtureRow{value: tx.maxRevision}
	}
	if strings.Contains(query, "SELECT EXISTS") {
		return publisherFixtureRow{value: tx.overlap}
	}
	return publisherFixtureRow{err: errors.New("unexpected query row")}
}
func (tx *publisherFixtureTx) Commit(context.Context) error   { tx.committed = true; return nil }
func (tx *publisherFixtureTx) Rollback(context.Context) error { return nil }

type publisherFixtureRow struct {
	value any
	err   error
}

func (r publisherFixtureRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch target := dest[0].(type) {
	case *int64:
		*target = r.value.(int64)
	case *bool:
		*target = r.value.(bool)
	default:
		return fmt.Errorf("unsupported scan target %T", dest[0])
	}
	return nil
}
