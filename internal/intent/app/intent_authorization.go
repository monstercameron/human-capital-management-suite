package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// RBAC-RT-003: intent reads and actions authorized by subject and
// relationship, not tenant alone.
//
// Before this file, GetIntent, ListIntents, ListIntentTimeline, CreateIntent,
// SubmitIntent, CancelIntent and SupersedeIntent checked authentication and
// tenant only: any principal in the tenant could read, list, submit or cancel
// any intent, including proposals carrying pay. The gates below require a
// read caller to be the initiator, a participant, in the subject's management
// chain, or to hold a role granting the intent's data domain; lists are
// filtered before paging; actions additionally require the capability for
// that intent type.
//
// The gates are active only when the service is composed with the durable
// role store (see Options.RoleAccess, wired by NewCell). A bare
// NewIntentService composition keeps the historical behavior so unit
// harnesses without a role store are unaffected. Effective roles resolve
// from durable assignments with the credential-role fallback (RBAC-RT-002),
// so a revoked administrator is refused even while the credential still
// signs the revoked role.

// intentAuthEnforced reports whether subject-and-relationship authorization
// applies. The durable role store is the backbone: without it there is no
// server-resolved role set to decide under.
func (s *IntentService) intentAuthEnforced() bool {
	return s != nil && s.roleAccess != nil
}

// effectiveIntentRoles resolves the caller's server-side role set: the
// durable assignment when the role store carries one for the subject, else
// the admitted credential roles. A resolution failure falls back to the
// admitted roles, exactly as the journey transport does; RBAC-RT-006 owns
// failing those paths closed.
func (s *IntentService) effectiveIntentRoles(ctx context.Context, principal *trust.Principal) []string {
	admitted := principal.Roles()
	if s == nil || s.roleAccess == nil {
		return admitted
	}
	roles, err := s.intentRoleResolver().Resolve(ctx, s.roleAccess,
		principal.Tenant(), principal.OrganizationScopeID(), principal.Subject(), admitted)
	if err != nil {
		return admitted
	}
	return roles
}

// intentRoleResolver returns the service's short-TTL role cache, creating it
// on first use. The resolver owns its own lock; this only guards creation.
func (s *IntentService) intentRoleResolver() *roleaccess.Resolver {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	if s.roleCache == nil {
		s.roleCache = roleaccess.NewResolver(roleaccess.DefaultResolverTTL, nil)
	}
	return s.roleCache
}

// intentLocator returns the cell's worker-reference resolver, defaulting to
// the corpus-only resolver exactly as the journey engine does.
func (s *IntentService) intentLocator() WorkerLocator {
	if s != nil && s.locate != nil {
		return s.locate
	}
	return corpusWorkerLocator
}

// workerSubjectKinds are the intent subject kinds that name a worker. They
// mirror resolvesAgainst's worker-identity cases: only these kinds resolve
// through the worker locator for subject and management-chain checks.
var workerSubjectKinds = map[string]struct{}{
	"PERSON": {}, "EMPLOYMENT": {}, "ASSIGNMENT": {}, "WORKER": {}, "COMPENSATION": {},
}

// intentWorkerSubjects resolves the intent's worker subjects through the
// cell's own locator, in declaration order. An unresolvable subject is
// skipped: absence of proof is never proof of authority.
func (s *IntentService) intentWorkerSubjects(ctx context.Context, tenant values.TenantId, subjects []intent.SubjectReference) []WorkerLocation {
	locate := s.intentLocator()
	var out []WorkerLocation
	for _, sub := range subjects {
		if _, ok := workerSubjectKinds[strings.ToUpper(strings.TrimSpace(sub.Kind))]; !ok {
			continue
		}
		id := strings.TrimSpace(sub.SubjectID)
		if id == "" {
			continue
		}
		loc, found, err := locate(ctx, tenant, id)
		if err != nil || !found {
			continue
		}
		out = append(out, loc)
	}
	return out
}

// isIntentParticipant reports whether viewer participates in the intent
// directly: the viewer is one of the intent's worker subjects, by key or by
// entity id, or names a non-worker subject verbatim (a transaction or plan
// reference the viewer holds is still participation).
func isIntentParticipant(viewer string, subjects []intent.SubjectReference, workers []WorkerLocation) bool {
	viewer = strings.TrimSpace(viewer)
	if viewer == "" {
		return false
	}
	for _, w := range workers {
		if w.Key == viewer || w.Ref.Id == viewer {
			return true
		}
	}
	for _, sub := range subjects {
		if id := strings.TrimSpace(sub.SubjectID); id != "" && id == viewer {
			return true
		}
	}
	return false
}

// intentAuthFields maps one intent type onto the policy fields its envelope
// discloses, mirroring the governed-read authorization each domain resolver
// already evaluates (inputs.go, inputs_diagnostics.go): a caller who may
// create or simulate an intent of this type is authorized for exactly these
// fields, so creation, simulation and reads agree. Types outside the P1A
// table fall back to the non-sensitive core, where scope alone decides.
func intentAuthFields(typeID string) (gate, read []authz.FieldID) {
	switch typeID {
	case promotion.IntentType:
		return []authz.FieldID{authz.FieldBaseSalary, authz.FieldBonusTarget},
			peopleFields(promotion.RequiredWorkerFields())
	case people.ExplainWorkerStateIntentType:
		return nil, peopleFields(people.AllFields())
	case rewards.SimulateCompensationIntentType:
		return []authz.FieldID{authz.FieldBaseSalary, authz.FieldBonusTarget}, nil
	case rewards.EvaluatePayBandIntentType:
		return []authz.FieldID{authz.FieldBaseSalary}, nil
	case dataops.DetectDriftIntentType,
		repair.CreateRepairPlanIntentType,
		repair.SimulateRepairIntentType:
		return nil, peopleFields(comparisonPeopleFields())
	case intelligence.ExplainTransactionIntentType:
		return nil, []authz.FieldID{authz.FieldWorkerNumber}
	default:
		return nil, []authz.FieldID{authz.FieldWorkerNumber, authz.FieldJobTitle}
	}
}

// intentAuthSubject selects the policy subject one intent is authorized
// over: its first resolved worker, else its first subject verbatim. A
// subject that is not a well-formed reference authorizes nothing.
func intentAuthSubject(tenant values.TenantId, subjects []intent.SubjectReference, workers []WorkerLocation) (values.EntityRef, error) {
	if len(workers) > 0 {
		return workers[0].Ref, nil
	}
	if len(subjects) > 0 {
		ref := values.EntityRef{
			Tenant: tenant,
			Kind:   values.Kind(strings.ToLower(strings.TrimSpace(subjects[0].Kind))),
			Id:     strings.TrimSpace(subjects[0].SubjectID),
		}
		if err := ref.Validate(); err != nil {
			return values.EntityRef{}, fmt.Errorf("%w: the intent carries no valid authorization subject: %v",
				ErrAuthorizationDenied, err)
		}
		return ref, nil
	}
	return values.EntityRef{}, fmt.Errorf("%w: the intent carries no authorization subject",
		ErrAuthorizationDenied)
}

// denyIntentRead authorizes one intent read by subject and relationship. It
// returns nil when the caller is the initiator, a participant, in the
// subject's management chain, or holds a role granting the intent's data
// domain (over the server-resolved effective roles). Otherwise it returns
// the policy denial: callers project it as the visibility answer (NOT_FOUND,
// which never distinguishes "does not exist" from "not visible to you") or
// as the policy refusal (PERMISSION_DENIED), depending on which contract
// the method already promises.
func (s *IntentService) denyIntentRead(ctx context.Context, principal *trust.Principal, purpose string, inst intent.Instance) error {
	if !s.intentAuthEnforced() {
		return nil
	}
	if isJourneyInitiator(inst.Initiator.PrincipalID, principal.Subject()) {
		return nil
	}
	workers := s.intentWorkerSubjects(ctx, inst.Tenant, inst.Subjects)
	if isIntentParticipant(principal.Subject(), inst.Subjects, workers) {
		return nil
	}
	subject, err := intentAuthSubject(inst.Tenant, inst.Subjects, workers)
	if err != nil {
		return err
	}
	effective := s.effectiveIntentRoles(ctx, principal)
	gate, read := intentAuthFields(inst.Definition.TypeID)
	var relationships []authz.RelationshipFact
	if len(workers) > 0 {
		relationships = managerChainFacts(ctx, s.intentLocator(), principal, subject)
	}
	evaluatedAt := inst.CreatedAt
	if !evaluatedAt.IsSet() {
		evaluatedAt = s.clock()
	}
	_, authErr := authorizeRead(principal, purpose, authorizationRequest{
		Subject:        subject,
		EvaluatedAt:    evaluatedAt,
		EffectiveRoles: effective,
		Gate:           gate,
		Read:           read,
		Relationships:  relationships,
	})
	return authErr
}

// readRefusal projects a read denial onto the visibility answer: a policy
// denial hides (NOT_FOUND); anything else is this cell's fault.
func readRefusal(err error) *envelope.Error {
	if errors.Is(err, ErrAuthorizationDenied) {
		return notFound()
	}
	return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
		"the operation could not be completed").WithDiagnostic(err)
}

// authorizeIntentAction checks the capability half of one intent action:
// the capability for the intent type must be published and invokable by
// this caller under the resolved purpose. It returns the refusal when the
// capability is what refuses, else the raw subject-rule denial (nil when
// the subject rule allows).
func (s *IntentService) authorizeIntentAction(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	def intent.Definition,
	tenant values.TenantId,
	subjects []intent.SubjectReference,
	initiator string,
	createdAt values.Instant,
) (*envelope.Error, error) {
	if !s.intentAuthEnforced() {
		return nil, nil
	}
	key := capabilityKeyFor(def.Ref)
	rec, found := s.caps.Lookup(key)
	if !found {
		return envelope.New(envelope.CodePermissionDenied, reasonAuthorizationDenied,
			"the caller is not authorized to act under this capability").
			WithViolation("capability_id", "no capability is published for this intent type", "capability.published"), nil
	}
	if decision := authorize(principal, purpose, rec.Definition); decision.Decision != capability.Allow {
		reason := decision.Reason
		if reason == "" {
			reason = "the principal is not authorized for this capability under the resolved purpose"
		}
		return envelope.New(envelope.CodePermissionDenied, reasonAuthorizationDenied,
			"the caller is not authorized to act under this capability").
			WithViolation("capability_id", reason, "capability_gateway."+string(decision.Decision)), nil
	}
	inst := intent.Instance{
		Definition: def.Ref,
		Tenant:     tenant,
		Initiator:  intent.PrincipalReference{PrincipalID: initiator},
		Subjects:   subjects,
		CreatedAt:  createdAt,
	}
	return nil, s.denyIntentRead(ctx, principal, purpose, inst)
}

// denyIntentAction authorizes one intent creation. A subject refusal is
// PERMISSION_DENIED: a creation names no existing row to hide.
func (s *IntentService) denyIntentAction(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	def intent.Definition,
	tenant values.TenantId,
	subjects []intent.SubjectReference,
	initiator string,
	createdAt values.Instant,
) *envelope.Error {
	capErr, subjErr := s.authorizeIntentAction(ctx, principal, purpose, def, tenant, subjects, initiator, createdAt)
	if capErr != nil {
		return capErr
	}
	if subjErr == nil {
		return nil
	}
	if errors.Is(subjErr, ErrAuthorizationDenied) {
		return envelope.New(envelope.CodePermissionDenied, reasonAuthorizationDenied,
			"the caller is not authorized to create this intent").
			WithViolation("subjects", "the caller is not a participant, in the subject's management chain, and holds no role granting the intent's data domain", AuthorizationPolicyVersion).
			WithDiagnostic(subjErr)
	}
	return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
		"the operation could not be completed").WithDiagnostic(subjErr)
}

// denyStoredIntentAction authorizes an action on a stored intent: the same
// capability-plus-subject rule, evaluated over the stored envelope. A
// subject refusal hides (NOT_FOUND).
func (s *IntentService) denyStoredIntentAction(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	def intent.Definition,
	inst intent.Instance,
) *envelope.Error {
	capErr, subjErr := s.authorizeIntentAction(ctx, principal, purpose, def,
		inst.Tenant, inst.Subjects, inst.Initiator.PrincipalID, inst.CreatedAt)
	if capErr != nil {
		return capErr
	}
	if subjErr != nil {
		return readRefusal(subjErr)
	}
	return nil
}

// listCallAllowed is the call-level gate for ListIntents: a caller with no
// effective role at all is refused the list rather than handed an empty
// page (the K-07 work-queue precedent from RBAC-RT-004). Row filtering
// below then decides which rows a role-holding caller sees.
func (s *IntentService) listCallAllowed(ctx context.Context, principal *trust.Principal) *envelope.Error {
	if !s.intentAuthEnforced() {
		return nil
	}
	if len(s.effectiveIntentRoles(ctx, principal)) == 0 {
		return envelope.New(envelope.CodePermissionDenied, reasonAuthorizationDenied,
			"the caller is not authorized to list intents").
			WithViolation("role", "the caller holds no role granting intent visibility", AuthorizationPolicyVersion)
	}
	return nil
}

// intentListCursor is the opaque offset into the caller's own filtered list.
// It binds the tenant and subject it was minted for, so one caller's cursor
// is invalid in another caller's hands rather than a window into a
// differently-filtered page.
type intentListCursor struct {
	Owner  string `json:"o"`
	Offset int    `json:"n"`
}

// encodeIntentCursor mints a cursor for owner at offset.
func encodeIntentCursor(tenant, subject string, offset int) string {
	raw, _ := json.Marshal(intentListCursor{Owner: tenant + "|" + subject, Offset: offset})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeIntentCursor validates a caller-supplied cursor for owner. An empty
// cursor starts at zero; anything else must decode and name this owner.
func decodeIntentCursor(cursor, tenant, subject string) (int, *envelope.Error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, invalidIntentCursor()
	}
	var decoded intentListCursor
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Offset < 0 {
		return 0, invalidIntentCursor()
	}
	if decoded.Owner != tenant+"|"+subject {
		return 0, invalidIntentCursor()
	}
	return decoded.Offset, nil
}

func invalidIntentCursor() *envelope.Error {
	return envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
		"the request is malformed or structurally invalid").
		WithViolation("page.cursor", "the page cursor is not a cursor this service minted for this caller", rulePhaseCeiling)
}

// intentListScanPageSize bounds one store round-trip while collecting a
// filtered page. Collection always terminates: every store page advances
// the store cursor, and the scan stops at the store's end.
const intentListScanPageSize int32 = 200

// filterIntentList returns the caller's visible intents in store order:
// every record is decoded, projected and authorized before paging is
// applied, so a page boundary never decides visibility.
func (s *IntentService) filterIntentList(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	tenant string,
) ([]intent.Instance, *envelope.Error) {
	var visible []intent.Instance
	cursor := ""
	for {
		page, err := s.store.ListIntents(ctx, tenant, intentListScanPageSize, cursor)
		if err != nil {
			return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
				"the operation could not be completed").WithDiagnostic(err)
		}
		for _, rec := range page.Records {
			inst, decodeErr := decodeEnvelope(rec.Envelope)
			if decodeErr != nil {
				return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
					"the operation could not be completed").WithDiagnostic(decodeErr)
			}
			mergeCurrentProjection(&inst, rec)
			if denyErr := s.denyIntentRead(ctx, principal, purpose, inst); denyErr != nil {
				continue
			}
			visible = append(visible, inst)
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	return visible, nil
}
