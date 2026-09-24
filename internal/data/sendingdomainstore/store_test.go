package sendingdomainstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/sendingdomainstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type dnsRecordsResolver struct {
	records delivery.DNSRecords
	err     error
}

func (r dnsRecordsResolver) Resolve(context.Context, delivery.DomainProfile) (delivery.DNSRecords, error) {
	return r.records, r.err
}

func TestTodo_REV_058_01_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local','DNS auth test','ACTIVE',now())`, tenant, "rev058-01-"+tenant.String())
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema, "role": tenancy.AppRole})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := sendingdomainstore.New(pool, tenant)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = store.ActivateFromDNS(context.Background(), "mail.example.test", delivery.DNSRecords{SPF: "v=spf1 -all", DKIMSelectors: map[string]string{"s1": "v=DKIM1; p=key"}, DMARC: "v=DMARC1; p=reject"}, "mail-operations", now)
	if err != nil {
		t.Fatal(err)
	}
	domains, err := store.ListActive(context.Background())
	if err != nil || len(domains) != 1 || domains[0].Owner != "mail-operations" || domains[0].Profile.DKIMSelectors[0] != "s1" {
		t.Fatalf("active domains=%+v err=%v", domains, err)
	}
	if err := store.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); err != nil {
		t.Fatalf("verified domain refused: %v", err)
	}
	initial := domains[0].Profile
	verifier := delivery.DomainReverifier{
		Profiles: store,
		DNS: dnsRecordsResolver{records: delivery.DNSRecords{
			SPF: "v=spf1 -all", DKIMSelectors: map[string]string{"s1": "v=DKIM1; p=rotated-key"}, DMARC: "v=DMARC1; p=reject",
		}},
		Alerts: store,
		Now:    func() time.Time { return now },
	}
	if err := verifier.RunOnce(context.Background()); err != nil {
		t.Fatalf("scheduled reverification pass: %v", err)
	}
	domains, err = store.ListActive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rotated := domains[0].Profile
	if !rotated.Verified || rotated.Version != initial.Version+1 || rotated.PreviousDigest != initial.Digest || rotated.RecordDigest == initial.RecordDigest {
		t.Fatalf("live store did not retain DNS-authenticated rotation: before=%+v after=%+v", initial, rotated)
	}
	if err := store.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); err != nil {
		t.Fatalf("successfully reverified domain refused: %v", err)
	}
	staleTx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), staleTx, tenant); err != nil {
		_ = staleTx.Rollback(context.Background())
		t.Fatal(err)
	}
	if _, err := staleTx.Exec(context.Background(), `UPDATE sending_domain_profile SET last_checked_at=$2 WHERE tenant_id=$1 AND domain='mail.example.test'`, tenant, now.Add(-24*time.Hour)); err != nil {
		_ = staleTx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := staleTx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); !errors.Is(err, delivery.ErrUnverifiedDomain) {
		t.Fatalf("stale DNS evidence passed send gate: %v", err)
	}
	futureTx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), futureTx, tenant); err != nil {
		_ = futureTx.Rollback(context.Background())
		t.Fatal(err)
	}
	if _, err := futureTx.Exec(context.Background(), `UPDATE sending_domain_profile SET last_checked_at=$2 WHERE tenant_id=$1 AND domain='mail.example.test'`, tenant, now.Add(24*time.Hour)); err != nil {
		_ = futureTx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := futureTx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); !errors.Is(err, delivery.ErrUnverifiedDomain) {
		t.Fatalf("future DNS evidence passed send gate: %v", err)
	}
	failureAt := time.Now().UTC()
	failingVerifier := delivery.DomainReverifier{
		Profiles: store, DNS: dnsRecordsResolver{err: errors.New("DNS lookup timed out")}, Alerts: store,
		Now: func() time.Time { return failureAt },
	}
	if err := failingVerifier.RunOnce(context.Background()); !errors.Is(err, delivery.ErrDomainUnverified) {
		t.Fatalf("scheduled DNS failure did not report unverified domain: %v", err)
	}
	domains, err = store.ListActive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	active := domains[0]
	if active.Profile.Verified || active.Profile.SPFVerified || active.Profile.DKIMVerified || active.Profile.DMARCVerified || active.AlertCycle == "" {
		t.Fatalf("DNS failure was not durably recorded as unverified: %+v", active)
	}
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	var scope []byte
	if err := tx.QueryRow(context.Background(), `SELECT scope FROM operational_incident WHERE tenant_id=$1 AND incident_key LIKE 'mail-domain-auth:mail.example.test:%' AND status='OPEN'`, tenant).Scan(&scope); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("owned alert missing: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	var ownership map[string]any
	if err := json.Unmarshal(scope, &ownership); err != nil {
		t.Fatal(err)
	}
	if ownership["primary_owner"] != "mail-operations" || ownership["secondary_route"] != "operations-on-call" {
		t.Fatalf("alert has no owned route: %s", scope)
	}
	if err := store.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); !errors.Is(err, delivery.ErrUnverifiedDomain) {
		t.Fatalf("open owned DNS alert did not close the send gate: %v", err)
	}
	active.AlertCycle = "" // exercise recovery after a lost profile cycle value
	if err := store.SaveProfile(context.Background(), active); err != nil {
		t.Fatalf("persist profile after alert-cycle loss: %v", err)
	}
	recoveryAt := time.Now().UTC()
	recovery := delivery.DomainReverifier{
		Profiles: store,
		DNS: dnsRecordsResolver{records: delivery.DNSRecords{
			SPF: "v=spf1 -all", DKIMSelectors: map[string]string{"s1": "v=DKIM1; p=rotated-key"}, DMARC: "v=DMARC1; p=reject",
		}},
		Alerts: store,
		Now:    func() time.Time { return recoveryAt },
	}
	if err := recovery.RunOnce(context.Background()); err != nil {
		t.Fatalf("scheduled DNS recovery pass: %v", err)
	}
	domains, err = store.ListActive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	active = domains[0]
	if !active.Profile.Verified || active.AlertCycle != "" {
		t.Fatalf("DNS recovery did not reverify the profile and resolve its alert: %+v", active)
	}
	if err := store.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); err != nil {
		t.Fatalf("resolved alert kept a verified and fresh domain blocked: %v", err)
	}
	active.AlertCycle = recoveryAt.Add(time.Minute).Format(time.RFC3339Nano)
	if err := store.Raise(context.Background(), active, "DNS authentication drift recurred", recoveryAt.Add(time.Minute)); err != nil {
		t.Fatalf("raise recurring alert: %v", err)
	}
	secondTx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), secondTx, tenant); err != nil {
		_ = secondTx.Rollback(context.Background())
		t.Fatal(err)
	}
	var openAlerts int
	if err := secondTx.QueryRow(context.Background(), `SELECT count(*) FROM operational_incident WHERE tenant_id=$1 AND incident_key LIKE 'mail-domain-auth:mail.example.test:%' AND status='OPEN'`, tenant).Scan(&openAlerts); err != nil {
		_ = secondTx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := secondTx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if openAlerts != 1 {
		t.Fatalf("recurring DNS failure did not open a fresh owned incident: open=%d", openAlerts)
	}
	if err := store.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); !errors.Is(err, delivery.ErrUnverifiedDomain) {
		t.Fatalf("recurring owned incident did not close the send gate: %v", err)
	}
	if err := store.Resolve(context.Background(), active, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("resolve recurring alert: %v", err)
	}
	domains[0].Profile.Verified = false
	if err := store.SaveProfile(context.Background(), domains[0]); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); !errors.Is(err, delivery.ErrUnverifiedDomain) {
		t.Fatalf("unverified profile passed send gate: %v", err)
	}
}

func TestTodo_REV_058_01_Fault(t *testing.T) {
	tenant := uuid.New()
	store, err := sendingdomainstore.New(failedPool{}, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); !errors.Is(err, delivery.ErrUnverifiedDomain) {
		t.Fatalf("database fault did not fail closed: %v", err)
	}
}

func TestTodo_REV_058_01_TransientWriteFailure(t *testing.T) {
	now := time.Now().UTC()
	db := &transientWritePool{now: now}
	tenant := uuid.New()
	store, err := sendingdomainstore.New(db, tenant, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	domain := delivery.ActiveSendingDomain{TenantID: tenant.String(), Owner: "mail-operations", Profile: delivery.DomainProfile{Domain: "mail.example.test", Digest: strings.Repeat("a", 64), Verified: false}, LastCheckedAt: now}
	if err := store.SaveProfile(context.Background(), domain); err == nil {
		t.Fatal("transient profile write failure was accepted")
	}
	// The read below reports the previous durable row as verified, but this
	// Store observed a failed unverification write and must fail closed.
	if err := store.AuthorizeSendingDomain(context.Background(), domain.TenantID, domain.Profile.Domain); !errors.Is(err, delivery.ErrUnverifiedDomain) {
		t.Fatalf("failed unverification still authorized sends: %v", err)
	}
	if db.reads != 1 {
		t.Fatalf("send gate did not observe successful stale row read: reads=%d", db.reads)
	}
}

func TestTodo_REV_058_01_TwoStoreFault(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local','DNS two-store test','ACTIVE',now())`, tenant, "rev058-two-store-"+tenant.String())
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema, "role": tenancy.AppRole})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	writer, err := sendingdomainstore.New(pool, tenant)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := sendingdomainstore.New(pool, tenant)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = writer.ActivateFromDNS(context.Background(), "mail.example.test", delivery.DNSRecords{SPF: "v=spf1 -all", DKIMSelectors: map[string]string{"s1": "v=DKIM1; p=key"}, DMARC: "v=DMARC1; p=reject"}, "mail-operations", now)
	if err != nil {
		t.Fatal(err)
	}
	domains, err := writer.ListActive(context.Background())
	if err != nil || len(domains) != 1 {
		t.Fatalf("domains=%+v err=%v", domains, err)
	}
	active := domains[0]
	if err := writer.BeginCheck(context.Background(), active, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Simulate the verifier's later profile write and alert route both failing.
	failing, err := sendingdomainstore.New(failingWriteBeginner{db: pool}, tenant)
	if err != nil {
		t.Fatal(err)
	}
	active.Profile.Verified = false
	active.Profile.SPFVerified = false
	active.Profile.DKIMVerified = false
	active.Profile.DMARCVerified = false
	active.LastCheckedAt = now.Add(time.Minute)
	active.AlertCycle = active.LastCheckedAt.Format(time.RFC3339Nano)
	if err := failing.SaveProfile(context.Background(), active); err == nil {
		t.Fatal("profile write fault was not injected")
	}
	if err := failing.Raise(context.Background(), active, "DNS authentication check failed", active.LastCheckedAt); err == nil {
		t.Fatal("alert route fault was not injected")
	}
	if err := reader.AuthorizeSendingDomain(context.Background(), tenant.String(), "mail.example.test"); !errors.Is(err, delivery.ErrUnverifiedDomain) {
		t.Fatalf("second Store authorized sends after durable permit revocation: %v", err)
	}
}

type failingWriteBeginner struct{ db dbport.Beginner }

func (b failingWriteBeginner) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failingWriteTx{Tx: tx}, nil
}

type failingWriteTx struct{ dbport.Tx }

func (tx failingWriteTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	if strings.Contains(query, "UPDATE sending_domain_profile") || strings.Contains(query, "INSERT INTO operational_incident") {
		return 0, errors.New("injected durable write failure")
	}
	return tx.Tx.Exec(ctx, query, args...)
}

// failedPool models a database outage at transaction acquisition.
type failedPool struct{}

func (failedPool) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("database unavailable")
}

type transientWritePool struct {
	now   time.Time
	reads int
}

func (p *transientWritePool) Begin(context.Context) (dbport.Tx, error) {
	return &transientWriteTx{pool: p}, nil
}

type transientWriteTx struct{ pool *transientWritePool }

func (tx *transientWriteTx) Exec(_ context.Context, query string, _ ...any) (int64, error) {
	if strings.Contains(query, "UPDATE sending_domain_profile") {
		return 0, errors.New("transient write failure")
	}
	return 1, nil
}
func (tx *transientWriteTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (tx *transientWriteTx) QueryRow(context.Context, string, ...any) dbport.Row {
	tx.pool.reads++
	return transientWriteRow{values: []any{true, tx.pool.now.Add(-time.Minute), tx.pool.now, false}}
}
func (tx *transientWriteTx) Commit(context.Context) error   { return nil }
func (tx *transientWriteTx) Rollback(context.Context) error { return nil }

type transientWriteRow struct{ values []any }

func (r transientWriteRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return errors.New("unexpected scan shape")
	}
	for i, value := range r.values {
		switch target := dest[i].(type) {
		case *bool:
			*target = value.(bool)
		case *time.Time:
			*target = value.(time.Time)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}
