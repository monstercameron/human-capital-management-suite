package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaRunTenantRuntime = errors.New("application: persona run tenant runtime unavailable")

// PersonaRunAdmissionReader reads an admission from an already tenant-scoped
// durable store.
type PersonaRunAdmissionReader interface {
	GetByID(context.Context, string) (agentrun.Record, error)
}

// PersonaRunAdmissionRechecker reloads the immutable admission and reruns
// current server authority before execution boundaries proceed.
type PersonaRunAdmissionRechecker struct {
	tenant    string
	reader    PersonaRunAdmissionReader
	authority agentrun.Authority
}

// NewPersonaRunAdmissionRechecker requires a tenant-scoped durable reader and
// the same current authority used to admit persona requests.
func NewPersonaRunAdmissionRechecker(tenant string, reader PersonaRunAdmissionReader, authority agentrun.Authority) (*PersonaRunAdmissionRechecker, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(tenant) != tenant || isNilPersonaOutputPort(reader) || isNilPersonaOutputPort(authority) {
		return nil, errPersonaRunTenantRuntime
	}
	return &PersonaRunAdmissionRechecker{tenant: tenant, reader: reader, authority: authority}, nil
}

// Recheck verifies tenant binding, reloads the accepted durable request, and
// requires its current authority snapshot to remain identical to admission.
func (r *PersonaRunAdmissionRechecker) Recheck(ctx context.Context, tenant, admissionID string) error {
	if r == nil || ctx == nil || tenant != r.tenant || strings.TrimSpace(admissionID) == "" {
		return errPersonaRunTenantRuntime
	}
	// Every runstate boundary invalidates the helper-level snapshot first. The
	// successful result below may then be reused by helpers inside this one
	// boundary, but never by the next checkpoint or side effect.
	agentUXSpeedInvalidateAuthority(ctx)
	done := agentUXSpeedEvent(ctx, "store.admission.get")
	record, err := r.reader.GetByID(ctx, admissionID)
	done()
	if err != nil || record.Request.Source.TenantID != tenant || record.ID != admissionID || record.Decision != agentrun.DecisionAccepted {
		return errPersonaRunTenantRuntime
	}
	if personaRunReplyDelivered(ctx, admissionID) {
		// The reply is already written. Its own write was fenced against the
		// current audience, and it has changed the conversation and thread
		// this admission pinned, so comparing them again can only refuse to
		// record a delivery that happened. The record binding above still holds.
		return nil
	}
	done = agentUXSpeedEvent(ctx, "authority.boundary_verify")
	current, err := r.authority.VerifyAdmission(ctx, record.Request)
	done()
	if err != nil {
		return fmt.Errorf("%w: current authority: %v", errPersonaRunTenantRuntime, err)
	}
	if !reflect.DeepEqual(current, record.Authority) {
		return fmt.Errorf("%w: authority changed since admission: admitted=%+v current=%+v", errPersonaRunTenantRuntime, record.Authority, current)
	}
	return nil
}

type personaRunAdmissionReader struct {
	store *agentrunstore.AdmissionRepository
}

func (r personaRunAdmissionReader) GetByID(ctx context.Context, id string) (agentrun.Record, error) {
	if r.store == nil {
		return agentrun.Record{}, errPersonaRunTenantRuntime
	}
	return r.store.GetByID(ctx, id)
}

type personaRunTenantTxRunner interface {
	RunTenantTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
}

// DatabasePersonaRunTenantRuntimeFactory binds one starter's durable
// admission and execution stores to the invocation's authoritative tenant.
// All model, work, output, delivery, and policy ports remain caller supplied.
type DatabasePersonaRunTenantRuntimeFactory struct {
	db         personaRunTenantTxRunner
	tenantUUID func(values.TenantId) uuid.UUID
	base       PersonaRunStarterConfig
}

// NewDatabasePersonaRunTenantRuntimeFactory composes per-tenant durable
// stores around the supplied production worker ports. It does not provide
// memory fallbacks for any dependency.
func NewDatabasePersonaRunTenantRuntimeFactory(db personaRunTenantTxRunner, tenantUUID func(values.TenantId) uuid.UUID, base PersonaRunStarterConfig) (*DatabasePersonaRunTenantRuntimeFactory, error) {
	if db == nil || tenantUUID == nil {
		return nil, errPersonaRunTenantRuntime
	}
	factory := &DatabasePersonaRunTenantRuntimeFactory{db: db, tenantUUID: tenantUUID, base: base}
	if err := factory.ValidatePersonaRunTenantRuntime(); err != nil {
		return nil, err
	}
	return factory, nil
}

// ValidatePersonaRunTenantRuntime checks static worker dependencies before
// serving advertises persona invocation as available.
func (f *DatabasePersonaRunTenantRuntimeFactory) ValidatePersonaRunTenantRuntime() error {
	if f == nil || f.db == nil || f.tenantUUID == nil || f.base.Builder == nil || f.base.Builder.source == nil ||
		isNilPersonaOutputPort(f.base.Authority) || isNilPersonaOutputPort(f.base.Model) ||
		isNilPersonaOutputPort(f.base.Work) || isNilPersonaOutputPort(f.base.Output) || isNilPersonaOutputPort(f.base.Reply) ||
		strings.TrimSpace(f.base.WorkerID) == "" || f.base.LeaseTTL <= 0 || f.base.Now == nil {
		return errPersonaRunTenantRuntime
	}
	return nil
}

// ForPersonaRunTenant returns a fresh starter configuration scoped to tenant.
func (f *DatabasePersonaRunTenantRuntimeFactory) ForPersonaRunTenant(_ context.Context, tenant string) (PersonaRunStarterConfig, error) {
	if f == nil || f.ValidatePersonaRunTenantRuntime() != nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(tenant) != tenant {
		return PersonaRunStarterConfig{}, errPersonaRunTenantRuntime
	}
	tenantID := f.tenantUUID(values.TenantId(tenant))
	if tenantID == uuid.Nil {
		return PersonaRunStarterConfig{}, errPersonaRunTenantRuntime
	}
	admission, err := agentrunstore.NewAdmissionRepository(f.db, tenantID, values.TenantId(tenant))
	if err != nil {
		return PersonaRunStarterConfig{}, fmt.Errorf("%w: admission store: %v", errPersonaRunTenantRuntime, err)
	}
	runStores, err := agentrunstate.New(f.db, func(value string) uuid.UUID { return f.tenantUUID(values.TenantId(value)) })
	if err != nil {
		return PersonaRunStarterConfig{}, fmt.Errorf("%w: execution store: %v", errPersonaRunTenantRuntime, err)
	}
	execution, err := runStores.ForTenant(tenant)
	if err != nil {
		return PersonaRunStarterConfig{}, fmt.Errorf("%w: tenant execution store: %v", errPersonaRunTenantRuntime, err)
	}
	recheck, err := NewPersonaRunAdmissionRechecker(tenant, personaRunAdmissionReader{store: admission}, f.base.Authority)
	if err != nil {
		return PersonaRunStarterConfig{}, err
	}
	result := f.base
	result.AdmissionStore = admission
	result.ExecutionStore = execution
	result.AdmissionRecheck = recheck
	return result, nil
}

var _ runstate.AdmissionRechecker = (*PersonaRunAdmissionRechecker)(nil)
var _ PersonaRunTenantRuntimeValidator = (*DatabasePersonaRunTenantRuntimeFactory)(nil)

type personaRunReplyDeliveredKey struct{}

// withPersonaRunReplyDelivered marks the context of the one checkpoint that
// records a delivery receipt for the named admission.
func withPersonaRunReplyDelivered(ctx context.Context, admissionID string) context.Context {
	return context.WithValue(ctx, personaRunReplyDeliveredKey{}, admissionID)
}

func personaRunReplyDelivered(ctx context.Context, admissionID string) bool {
	delivered, ok := ctx.Value(personaRunReplyDeliveredKey{}).(string)
	return ok && delivered != "" && delivered == admissionID
}
