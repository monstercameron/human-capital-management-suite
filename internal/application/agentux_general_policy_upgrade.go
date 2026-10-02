package application

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// localAgentDemoPolicySourceState follows the append-only source in the agent
// database. Every contract in an upgrade receives the new source revision;
// immutable references and bytes from earlier publications are retained.
func localAgentDemoPolicySourceState(core, agents dbport.Beginner, mapper func(values.TenantId) uuid.UUID) AgentPolicySourceState {
	return AgentPolicySourceStateFunc(func(ctx context.Context, tenant values.TenantId, source string) (uint64, bool, error) {
		if !localPersonaOpenAIDemoTenant(string(tenant)) || source != LocalPersonaOpenAIPolicySourceID {
			return 0, true, ErrAgentModelPolicyUnavailable
		}
		present, err := registeredLocalPolicyTenant(ctx, core, mapper(tenant))
		if err != nil || !present {
			return 0, true, ErrAgentModelPolicyUnavailable
		}
		tx, err := agents.Begin(ctx)
		if err != nil {
			return 0, true, err
		}
		defer tx.Rollback(ctx)
		if err := tenancy.WithTenant(ctx, tx, mapper(tenant)); err != nil {
			return 0, true, err
		}
		var revision uint64
		var revoked bool
		err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(source_revision),1),COALESCE(bool_or(revoked),false) FROM (SELECT DISTINCT ON(kind,contract_id,version,schema_version) source_revision,revoked FROM agent_contract_authority WHERE tenant_id=$1 AND source_id=$2 ORDER BY kind,contract_id,version,schema_version,revision DESC) current_source`, mapper(tenant), source).Scan(&revision, &revoked)
		if err != nil {
			return 0, true, err
		}
		return revision, revoked, tx.Commit(ctx)
	})
}

// ensureLocalAgentDemoPolicyUpgrade serializes local publishers, validates the
// previous assertions, then appends the entire shipped set at the next source
// revision. A retry completes an interrupted revision instead of replaying it.
func ensureLocalAgentDemoPolicyUpgrade(ctx context.Context, core, agents, authority dbport.Beginner, mapper func(values.TenantId) uuid.UUID, tenant values.TenantId, key ed25519.PrivateKey, now time.Time) error {
	if ctx == nil || core == nil || agents == nil || authority == nil || mapper == nil || !localPersonaOpenAIDemoTenant(string(tenant)) || len(key) != ed25519.PrivateKeySize || now.IsZero() {
		return ErrAgentModelPolicyUnavailable
	}
	lock, err := authority.Begin(ctx)
	if err != nil {
		return err
	}
	defer lock.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, lock, mapper(tenant)); err != nil {
		return err
	}
	if _, err := lock.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "local-agent-policy-upgrade:"+mapper(tenant).String()); err != nil {
		return err
	}
	state := localAgentDemoPolicySourceState(core, agents, mapper)
	revision, revoked, err := state.CurrentAgentPolicySource(ctx, tenant, LocalPersonaOpenAIPolicySourceID)
	if err != nil {
		return err
	}
	if revoked {
		return fmt.Errorf("%w: local policy source is unavailable or revoked", ErrAgentModelPolicyUnavailable)
	}
	sources, err := NewLocalPersonaOpenAIPolicySources(key.Public().(ed25519.PublicKey), mapper, state)
	if err != nil {
		return err
	}
	records := LocalPersonaOpenAIPolicyRecords()
	current := make([]agentmodelpolicystore.Authority, len(records))
	complete, partial, published, expired := true, false, false, false
	for i, record := range records {
		var raw, content []byte
		var digest string
		ref := record.Reference
		err := lock.QueryRow(ctx, `SELECT c.content,c.digest,a.authority FROM agent_immutable_contract c JOIN LATERAL (SELECT authority FROM agent_contract_authority a WHERE a.tenant_id=c.tenant_id AND a.kind=c.kind AND a.contract_id=c.contract_id AND a.version=c.version AND a.schema_version=c.schema_version ORDER BY revision DESC LIMIT 1) a ON true WHERE c.tenant_id=$1 AND c.kind=$2 AND c.contract_id=$3 AND c.version=$4 AND c.schema_version=$5`, mapper(tenant), record.Kind, ref.ID, ref.Version, ref.SchemaVersion).Scan(&content, &digest, &raw)
		if errors.Is(err, dbport.ErrNoRows) {
			complete = false
			continue
		}
		if err != nil {
			return err
		}
		published = true
		if digest != ref.Digest || string(content) != string(record.Content) {
			return fmt.Errorf("local contract %s %s v%d: stored digest %s differs from required digest %s: %w", record.Kind, ref.ID, ref.Version, digest, ref.Digest, agentmodelpolicystore.ErrConflict)
		}
		if json.Unmarshal(raw, &current[i]) != nil || current[i].SourceID != LocalPersonaOpenAIPolicySourceID || current[i].Revoked || now.Before(current[i].EffectiveFrom) {
			return ErrAgentModelPolicyUnavailable
		}
		previous := *sources
		previous.State = AgentPolicySourceStateFunc(func(context.Context, values.TenantId, string) (uint64, bool, error) {
			return current[i].SourceRevision, false, nil
		})
		if err := previous.verify(ctx, mapper(tenant), record, current[i]); err != nil {
			return err
		}
		if current[i].SourceRevision < revision {
			partial = true
		}
		if current[i].SourceRevision != revision || !now.Before(current[i].EffectiveUntil) {
			complete = false
		}
		expired = expired || !now.Before(current[i].EffectiveUntil)
	}
	if complete {
		return lock.Commit(ctx)
	}
	if published && (!partial || expired) {
		revision++
	}
	// Publication verifies the selected next revision before it exists. Serving
	// readers continue to require the persisted current revision on every read.
	sources.State = AgentPolicySourceStateFunc(func(ctx context.Context, requested values.TenantId, source string) (uint64, bool, error) {
		if requested != tenant || source != LocalPersonaOpenAIPolicySourceID {
			return 0, true, ErrAgentModelPolicyUnavailable
		}
		present, err := registeredLocalPolicyTenant(ctx, core, mapper(tenant))
		return revision, !present, err
	})
	publisher, err := agentmodelpolicystore.NewPublisher(localAgentDemoPolicyPublication{Tx: lock}, sources)
	if err != nil {
		return err
	}
	// Add new contracts first, so a crash during growth leaves a visibly partial
	// revision that the next preparation can finish without allocating another.
	order := make([]int, 0, len(records))
	for _, absent := range []bool{true, false} {
		for i := range records {
			if (current[i].SourceRevision == 0) == absent {
				order = append(order, i)
			}
		}
	}
	for _, i := range order {
		record := records[i]
		if current[i].SourceRevision == revision && now.Before(current[i].EffectiveUntil) {
			continue
		}
		doc, err := LocalPersonaOpenAIPolicyAuthorityDocument(tenant, mapper(tenant), record, LocalPersonaOpenAIPolicySourceID, LocalPersonaOpenAIPolicyKeyID, revision, now.Add(-time.Minute), now.AddDate(1, 0, 0))
		if err != nil {
			return err
		}
		assertion, err := SignAgentPolicyAuthority(doc, key)
		if err != nil {
			return err
		}
		if err := publisher.Publish(ctx, mapper(tenant), record, assertion); err != nil {
			return fmt.Errorf("local contract %s %s v%d source revision %d: %w", record.Kind, record.Reference.ID, record.Reference.Version, revision, err)
		}
	}
	return lock.Commit(ctx)
}

// The publisher still performs its role, signature, tenant and immutable-byte
// checks for each record. Only the outer upgrade commits or rolls back, so
// readers see the complete previous set or the complete new set.
type localAgentDemoPolicyPublication struct{ dbport.Tx }

func (p localAgentDemoPolicyPublication) Begin(context.Context) (dbport.Tx, error) {
	return p, nil
}

func (localAgentDemoPolicyPublication) Commit(context.Context) error   { return nil }
func (localAgentDemoPolicyPublication) Rollback(context.Context) error { return nil }
