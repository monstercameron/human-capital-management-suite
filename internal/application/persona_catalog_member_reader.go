package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaCatalogMemberReader = errors.New("application: persona catalog member reader unavailable")

// PersonaCatalogMemberIdentity resolves a chat-facing workforce key to the
// canonical entity ID required by people.FactQuery. It must use an exact,
// tenant-scoped workforce lookup.
type PersonaCatalogMemberIdentity interface {
	ResolvePersonaCatalogMemberID(context.Context, values.TenantId, string) (string, bool, error)
}

// CorePersonaCatalogMemberReader resolves the current label for one exact
// worker through the authoritative people facts port. It does not enumerate
// workers or accept a display label from a caller.
type CorePersonaCatalogMemberReader struct {
	Workers  people.WorkerFacts
	Identity PersonaCatalogMemberIdentity
	Now      func() time.Time
}

// NewCorePersonaCatalogMemberReader constructs a fail-closed member reader.
// The clock is supplied so the bitemporal query has an explicit current
// coordinate and remains deterministic in tests.
func NewCorePersonaCatalogMemberReader(workers people.WorkerFacts, now func() time.Time) (*CorePersonaCatalogMemberReader, error) {
	return NewCorePersonaCatalogMemberReaderWithIdentity(workers, nil, now)
}

// NewCorePersonaCatalogMemberReaderWithIdentity constructs a reader that
// resolves chat-facing workforce keys before asking for governed worker facts.
func NewCorePersonaCatalogMemberReaderWithIdentity(workers people.WorkerFacts, identity PersonaCatalogMemberIdentity, now func() time.Time) (*CorePersonaCatalogMemberReader, error) {
	if workers == nil || now == nil {
		return nil, fmt.Errorf("%w: authoritative workers and clock are required", errPersonaCatalogMemberReader)
	}
	return &CorePersonaCatalogMemberReader{Workers: workers, Identity: identity, Now: now}, nil
}

// ResolvePersonaCatalogMember reads the preferred or legal name of one exact
// tenant-qualified worker. Missing, malformed, or non-value facts fail closed.
func (r *CorePersonaCatalogMemberReader) ResolvePersonaCatalogMember(ctx context.Context, tenant values.TenantId, subject string) (PersonaCatalogMember, error) {
	if r == nil || r.Workers == nil || r.Now == nil || ctx == nil || tenant.Validate() != nil || strings.TrimSpace(subject) == "" || strings.TrimSpace(subject) != subject {
		return PersonaCatalogMember{}, errPersonaCatalogMemberReader
	}
	at := r.Now().UTC()
	effective, err := values.NewLocalDate(at.Year(), at.Month(), at.Day())
	if err != nil {
		return PersonaCatalogMember{}, fmt.Errorf("%w: current date: %v", errPersonaCatalogMemberReader, err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		return PersonaCatalogMember{}, fmt.Errorf("%w: current knowledge time: %v", errPersonaCatalogMemberReader, err)
	}
	workerID := subject
	if r.Identity != nil {
		resolved, found, resolveErr := r.Identity.ResolvePersonaCatalogMemberID(ctx, tenant, subject)
		if resolveErr != nil {
			return PersonaCatalogMember{}, personaCatalogStage("member_identity_read", fmt.Errorf("%w: resolve exact worker identity: %w", errPersonaCatalogMemberReader, resolveErr))
		}
		if !found {
			return PersonaCatalogMember{}, personaCatalogStage("member_identity_absent", errPersonaCatalogMemberReader)
		}
		workerID = resolved
	}
	worker := values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: workerID}
	if err := worker.Validate(); err != nil {
		return PersonaCatalogMember{}, personaCatalogStage("member_identity_malformed", fmt.Errorf("%w: resolved worker identity invalid", errPersonaCatalogMemberReader))
	}
	set, err := r.Workers.WorkerFactsAt(ctx, people.FactQuery{
		Tenant: tenant,
		Worker: worker,
		AsOf:   people.AsOf{EffectiveOn: effective, KnownAt: knownAt},
		Fields: []people.FieldID{people.FieldPreferredName, people.FieldLegalName},
	})
	if err != nil {
		return PersonaCatalogMember{}, personaCatalogStage("member_facts_read", fmt.Errorf("%w: read exact worker: %w", errPersonaCatalogMemberReader, err))
	}
	if set.Worker != worker {
		return PersonaCatalogMember{}, personaCatalogStage("member_facts_scope", errPersonaCatalogMemberReader)
	}
	if !set.Exists {
		return PersonaCatalogMember{}, personaCatalogStage("member_facts_absent", errPersonaCatalogMemberReader)
	}
	label := factLabel(set, people.FieldPreferredName)
	if label == "" {
		label = factLabel(set, people.FieldLegalName)
	}
	if label == "" {
		return PersonaCatalogMember{}, personaCatalogStage("member_name_absent", errPersonaCatalogMemberReader)
	}
	return PersonaCatalogMember{TenantID: tenant, SubjectID: subject, Label: label}, nil
}

func factLabel(set people.FactSet, field people.FieldID) string {
	fact, ok := set.Lookup(field)
	if !ok {
		return ""
	}
	label, ok := fact.Value.Get()
	if !ok {
		return ""
	}
	return strings.TrimSpace(label)
}

var _ PersonaCatalogMemberReader = (*CorePersonaCatalogMemberReader)(nil)
