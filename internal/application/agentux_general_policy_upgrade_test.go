package application

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func newAgentUXGeneralPolicyTestPool(t *testing.T, db *pgtest.DB) (*pgxadapter.Pool, error) {
	t.Helper()
	pool, err := pgxadapter.NewPool(context.Background(), personaChatSchemaDSN(t, db.URL, db.Schema), map[string]string{"role": agentmodelpolicystore.AuthorityRole})
	if err == nil {
		t.Cleanup(pool.Close)
	}
	return pool, err
}

func agentUXGeneralUpgradePolicyContracts(t *testing.T, ctx context.Context, core dbport.Beginner, agents *agentstore.Store, db *pgtest.DB, mapper func(values.TenantId) uuid.UUID, tenant values.TenantId, material LocalPersonaModelSigningMaterial, now time.Time, seedOnly bool) {
	t.Helper()
	seed, err := base64.StdEncoding.DecodeString(material.PolicySeed)
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("invalid policy signing fixture: %v", err)
	}
	key := ed25519.NewKeyFromSeed(seed)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentmodelpolicystore.AuthorityRole); err != nil {
		t.Fatal(err)
	}
	state := AgentPolicySourceStateFunc(func(context.Context, values.TenantId, string) (uint64, bool, error) { return 1, false, nil })
	sources, err := NewLocalPersonaOpenAIPolicySources(key.Public().(ed25519.PublicKey), mapper, state)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := agentmodelpolicystore.NewPublisher(conn, sources)
	if err != nil {
		t.Fatal(err)
	}
	records := LocalPersonaOpenAIPolicyRecords()
	if seedOnly {
		for _, record := range records[:3] {
			doc, err := LocalPersonaOpenAIPolicyAuthorityDocument(tenant, mapper(tenant), record, LocalPersonaOpenAIPolicySourceID, LocalPersonaOpenAIPolicyKeyID, 1, now.Add(-time.Hour), now.Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			assertion, err := SignAgentPolicyAuthority(doc, key)
			if err != nil {
				t.Fatal(err)
			}
			if err := publisher.Publish(ctx, mapper(tenant), record, assertion); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	var original string
	query := `SELECT jsonb_agg(to_jsonb(a) ORDER BY kind,contract_id)::text FROM agent_contract_authority a WHERE tenant_id=$1 AND source_revision=1`
	if err := db.SQL.QueryRow(query, mapper(tenant)).Scan(&original); err != nil {
		t.Fatal(err)
	}
	login, password := "general_policy_"+strings.ReplaceAll(uuid.NewString(), "-", ""), uuid.NewString()
	db.Exec(t, fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD '%s'", login, password))
	db.Exec(t, "GRANT "+agentmodelpolicystore.AuthorityRole+" TO "+login)
	t.Cleanup(func() { db.Exec(t, "DROP ROLE "+login) })
	policyURL, err := url.Parse(personaChatSchemaDSN(t, db.URL, db.Schema))
	if err != nil {
		t.Fatal(err)
	}
	policyURL.User = url.UserPassword(login, password)
	config := LocalAgentDemoConfig{Tenant: string(tenant), PolicyAuthorityDatabaseURL: policyURL.String(), AgentDatabaseURL: personaChatSchemaDSN(t, db.URL, db.Schema), DatabaseURL: "postgres://core@127.0.0.1:18540/general_core"}
	for run := 0; run < 2; run++ {
		if err := ensureLocalAgentDemoPolicyRecords(ctx, config, core, agents, mapper, material, now.Add(time.Duration(run)*time.Minute)); err != nil {
			t.Fatal(err)
		}
		var contracts, assertions int
		if err := db.SQL.QueryRow(`SELECT (SELECT count(*) FROM agent_immutable_contract WHERE tenant_id=$1),(SELECT count(*) FROM agent_contract_authority WHERE tenant_id=$1)`, mapper(tenant)).Scan(&contracts, &assertions); err != nil || contracts != 6 || assertions != 9 {
			t.Fatalf("upgrade/replay contracts=%d assertions=%d err=%v", contracts, assertions, err)
		}
		var retained string
		if err := db.SQL.QueryRow(query, mapper(tenant)).Scan(&retained); err != nil || retained != original {
			t.Fatalf("revision 1 changed: %v", err)
		}
	}
	sources.State = localAgentDemoPolicySourceState(core, agents, mapper)
	store, _ := agentmodelpolicystore.New(agents)
	registry, err := NewAgentModelPolicyRegistry(AgentModelPolicyRegistryConfig{Store: store, Sources: sources, TenantUUID: mapper, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		resolved, err := registry.resolve(ctx, tenant, record.Kind, record.Reference)
		if err != nil || resolved.Authority.SourceRevision != 2 || string(resolved.Record.Content) != string(record.Content) {
			t.Fatalf("upgraded contract %s resolved=%+v err=%v", record.Reference.ID, resolved, err)
		}
	}
	for _, tc := range []struct {
		tenant values.TenantId
		source string
	}{{"production", LocalPersonaOpenAIPolicySourceID}, {tenant, "foreign-source"}} {
		if _, revoked, err := sources.State.CurrentAgentPolicySource(ctx, tc.tenant, tc.source); err == nil || !revoked {
			t.Fatalf("foreign/unseeded policy source accepted: %+v", tc)
		}
	}
}

func TestAgentUXGeneral_PolicyRevision_Security(t *testing.T) {
	for _, fault := range []string{"revoked", "wrong-key", "partial", "expired", "publication-failure", "immutable-conflict"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			core, db := pgtest.New(t), pgtest.NewEmpty(t)
			if err := agentstore.Migrate(ctx, db.SQL); err != nil {
				t.Fatal(err)
			}
			tenantID := uuid.New()
			tenant := values.TenantId(localAgentDemoTenant)
			mapper := func(values.TenantId) uuid.UUID { return tenantID }
			core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,$2,'general','General','ACTIVE',now())`, tenantID, tenant)
			db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
			conn := db.NewConn(t)
			if _, err := conn.Exec(ctx, "SET ROLE "+agentmodelpolicystore.AuthorityRole); err != nil {
				t.Fatal(err)
			}
			key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
			now := time.Now().UTC().Truncate(time.Microsecond)
			state := &agentPolicySourceStateFixture{revision: 1}
			sources, err := NewLocalPersonaOpenAIPolicySources(key.Public().(ed25519.PublicKey), mapper, state)
			if err != nil {
				t.Fatal(err)
			}
			publisher, _ := agentmodelpolicystore.NewPublisher(conn, sources)
			for _, record := range LocalPersonaOpenAIPolicyRecords() {
				doc, _ := LocalPersonaOpenAIPolicyAuthorityDocument(tenant, tenantID, record, LocalPersonaOpenAIPolicySourceID, LocalPersonaOpenAIPolicyKeyID, 1, now.Add(-time.Hour), now.Add(time.Hour))
				issuer := publisher
				if fault == "immutable-conflict" && record.Reference.ID == localAgentDemoAssistantSuiteID {
					// Simulate an earlier independently approved suite with the same
					// immutable identity and different bytes, as on the review cell.
					suite := agenteval.AssistantSuite(personaPolicyHelperSkillID, personaChatReplySkillID)
					suite.Cases[0].Prompt += " Legacy fixture."
					record.Content, err = json.Marshal(suite)
					if err != nil {
						t.Fatal(err)
					}
					record.Reference.Digest = agenteval.PersonaSuiteDigest(suite)
					doc.Reference, doc.ContentDigest = record.Reference, agentmodelpolicystore.ContentDigest(record.Content)
					doc.Basis, doc.ReviewRef = "reviewed-deployment", "review:legacy-suite"
					legacy, err := NewLocalPersonaOpenAIPolicySources(key.Public().(ed25519.PublicKey), mapper, state)
					if err != nil {
						t.Fatal(err)
					}
					pin := legacy.Pins[LocalPersonaOpenAIPolicyKeyID]
					pin.LocalUserInstruction = false
					legacy.Pins[LocalPersonaOpenAIPolicyKeyID] = pin
					issuer, err = agentmodelpolicystore.NewPublisher(conn, legacy)
					if err != nil {
						t.Fatal(err)
					}
				}
				if fault == "expired" || fault == "publication-failure" {
					doc.EffectiveUntil = now.Add(-time.Minute)
				}
				if fault == "revoked" {
					doc.Revoked = true
				}
				assertion, _ := SignAgentPolicyAuthority(doc, key)
				if err := issuer.Publish(ctx, tenantID, record, assertion); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "partial" {
				state.revision = 2
				record := LocalPersonaOpenAIPolicyRecords()[3]
				doc, _ := LocalPersonaOpenAIPolicyAuthorityDocument(tenant, tenantID, record, LocalPersonaOpenAIPolicySourceID, LocalPersonaOpenAIPolicyKeyID, 2, now.Add(-time.Minute), now.Add(time.Hour))
				assertion, _ := SignAgentPolicyAuthority(doc, key)
				if err := publisher.Publish(ctx, tenantID, record, assertion); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "wrong-key" {
				key = ed25519.NewKeyFromSeed([]byte(strings.Repeat("x", ed25519.SeedSize)))
			}
			if fault == "publication-failure" {
				db.Exec(t, `CREATE FUNCTION reject_general_policy_upgrade() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.source_revision>1 AND NEW.kind='evaluation_suite' THEN RAISE EXCEPTION 'policy upgrade publication fault'; END IF; RETURN NEW; END $$`)
				db.Exec(t, `CREATE TRIGGER reject_general_policy_upgrade BEFORE INSERT ON agent_contract_authority FOR EACH ROW EXECUTE FUNCTION reject_general_policy_upgrade()`)
			}
			var before int
			if err := db.SQL.QueryRow(`SELECT count(*) FROM agent_contract_authority`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			// The upgrade uses the same restricted authority role as deployment.
			authority, err := newAgentUXGeneralPolicyTestPool(t, db)
			if err != nil {
				t.Fatal(err)
			}
			err = ensureLocalAgentDemoPolicyUpgrade(ctx, core.NewConn(t), commonAgentOpenIntegrationStore(t, db), authority, mapper, tenant, key, now)
			var after int
			if scanErr := db.SQL.QueryRow(`SELECT count(*) FROM agent_contract_authority`).Scan(&after); scanErr != nil {
				t.Fatal(scanErr)
			}
			if fault == "revoked" || fault == "wrong-key" {
				if !errors.Is(err, ErrAgentModelPolicyUnavailable) || after != before {
					t.Fatalf("unsafe repair err=%v assertions=%d -> %d", err, before, after)
				}
			} else if fault == "immutable-conflict" {
				if !errors.Is(err, agentmodelpolicystore.ErrConflict) || after != before || !strings.Contains(err.Error(), localAgentDemoAssistantSuiteID) {
					t.Fatalf("immutable contract replaced: err=%v assertions=%d -> %d", err, before, after)
				}
			} else if fault == "publication-failure" {
				if err == nil || !strings.Contains(err.Error(), "policy upgrade publication fault") || after != before {
					t.Fatalf("failed publication was not atomic: err=%v assertions=%d -> %d", err, before, after)
				}
			} else if err != nil || after != 12 {
				t.Fatalf("revision repair err=%v assertions=%d", err, after)
			}
		})
	}
}
