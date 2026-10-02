package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/agentcost"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcoststore"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The JSON routes of the agent access pages (AGENT2-018, AGENT2-019), the cost
// pages (AGENTCOST-006) and the task view's extend-budget action (AGENT2-017).
// They are JSON over HTTP rather than proto RPCs so the shared generated code is
// untouched; they take the same admission, tenancy and browser-origin rules as
// the other agent overlays, and every call is written to the request log with
// its outcome and error type.
const (
	AgentAccessPath     = "/api/agent-access/v1"
	AgentCostPath       = "/api/agent-cost/v1"
	AgentTaskBudgetPath = "/api/agent-tasks/v1/extend-budget"
)

// AgentCostOwners is what the cost routes need to know about agents: which a
// person owns and, through the request, whether they administer the tenant.
type AgentCostOwners interface {
	agentcost.Owners
	OwnedAgents(tenant, principalID string) ([]agentcoststore.OwnedAgent, error)
}

// AgentAccessHTTP serves the routes. Every field is optional: a route whose
// service is not composed answers "unavailable", never an empty success.
type AgentAccessHTTP struct {
	Users    *agentaccess.UserAccess
	Console  *agentaccess.Console
	Roles    roleaccess.Store
	Admins   *agentRequestAdmins
	Meter    agentcost.Meter
	Owners   AgentCostOwners
	Notices  *agentCostNotices
	Zone     *time.Location
	Extender AgentBudgetExtender
	Now      func() time.Time
}

// agentRequestAdmins is the console's and the gate's answer to "does this
// person administer the tenant": only the transport can say, from the verified
// principal of the request it is serving, so a person is listed for exactly the
// duration of their own admitted request. Nothing the page sends can add one.
type agentRequestAdmins struct {
	mu      sync.Mutex
	serving map[string]int
}

func newAgentRequestAdmins() *agentRequestAdmins {
	return &agentRequestAdmins{serving: map[string]int{}}
}

func (a *agentRequestAdmins) enter(tenant, subject string) func() {
	key := tenant + "\x00" + subject
	a.mu.Lock()
	a.serving[key]++
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		if a.serving[key]--; a.serving[key] <= 0 {
			delete(a.serving, key)
		}
		a.mu.Unlock()
	}
}

// CanAdminister implements agentaccess.Authorizer.
func (a *agentRequestAdmins) CanAdminister(tenant, actor string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.serving[tenant+"\x00"+actor] > 0
}

// agentCostNotices keeps the 80 percent notices an owner has not yet seen, so
// the cost panel can show them. It is not a delivery channel: a notice a person
// never opens the page for stays here until the process restarts.
type agentCostNotices struct {
	mu    sync.Mutex
	items map[string][]string
}

func newAgentCostNotices() *agentCostNotices { return &agentCostNotices{items: map[string][]string{}} }

// Notify implements agentcost.Notifier.
func (n *agentCostNotices) Notify(tenant, owner, message string) {
	if n == nil || owner == "" {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	key := tenant + "\x00" + owner
	for _, have := range n.items[key] {
		if have == message {
			return
		}
	}
	n.items[key] = append(n.items[key], message)
}

func (n *agentCostNotices) peek(tenant, owner string) []string {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.items[tenant+"\x00"+owner]...)
}

// agentAccessAgentOwners adapts the cost store and the request's administrators
// to agentcost.Owners.
type agentAccessAgentOwners struct {
	store  *agentcoststore.Store
	admins *agentRequestAdmins
}

func (o agentAccessAgentOwners) IsOwner(tenant, agentID, actor string) bool {
	return o.store != nil && o.store.IsBusinessOwner(tenant, agentID, actor)
}
func (o agentAccessAgentOwners) IsAdmin(tenant, actor string) bool {
	return o.admins.CanAdminister(tenant, actor)
}
func (o agentAccessAgentOwners) OwnedAgents(tenant, principalID string) ([]agentcoststore.OwnedAgent, error) {
	if o.store == nil {
		return nil, agentcost.ErrUnavailable
	}
	return o.store.OwnedAgents(tenant, principalID)
}

type agentAccessLinkBody struct{ ConnectionID string }
type agentAccessCompleteBody struct{ State, Code string }
type agentAccessRevokeBody struct{ TaskID string }
type agentAccessRevisionBody struct{ RevisionID string }
type agentAccessImportBody struct{ ConnectionID, SnapshotID string }
type agentAccessTierBody struct{ RevisionID, SkillID, Tier string }
type agentAccessPreviewBody struct{ RevisionID, UserID, Label string }
type agentAccessCreateBody struct {
	Revision productui.AgentConnectionRevision
}
type agentAccessGrantBody struct {
	ID                 string
	Roles              []string
	Population         string
	OrganizationScopes []string
	Skills             []string
}
type agentAccessGrantsBody struct {
	RevisionID string
	Grants     []agentAccessGrantBody
}
type agentCostLimitBody struct {
	AgentID              string
	MaxRunsPerDay        int64
	MaxSpendMicrosPerDay int64
}
type agentCostLimitsBody struct{ AgentIDs []string }
type agentTaskBudgetBody struct{ TaskID, RequestID string }

// AgentSpendLimitView is one agent's limit as the Agent setup card draws it.
type AgentSpendLimitView struct {
	AgentID              string
	MaxRunsPerDay        int64
	MaxSpendMicrosPerDay int64
	Reached              string
	// Denied is true when the viewer neither owns nor administers the agent: the
	// card is drawn read-only with no figures.
	Denied bool
}

// AgentCostReportView is the cost panel's data and the agents it covers.
type AgentCostReportView struct {
	Report productui.AgentCostReport
	Agents []agentcoststore.OwnedAgent
	Limits []AgentSpendLimitView
}

func agentAccessError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{code})
}

func agentAccessPath(path string) bool {
	return path == AgentAccessPath || strings.HasPrefix(path, AgentAccessPath+"/") ||
		path == AgentCostPath || strings.HasPrefix(path, AgentCostPath+"/") || path == AgentTaskBudgetPath
}

// OverlayAgentAccess admits and serves the routes above. A nil service leaves
// them to the next handler, which answers not found.
func OverlayAgentAccess(next http.Handler, service *AgentAccessHTTP, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	if service == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !agentAccessPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		ctx, inv, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			agentAccessError(w, denied.HTTPStatus(), "request_denied")
			agentAccessLog(admission, r.URL.Path, nil, started, denied)
			return
		}
		recorder := &agentAccessStatus{ResponseWriter: w, status: http.StatusOK}
		failure := service.serve(recorder, r.WithContext(ctx))
		var owned *envelope.Error
		if recorder.status >= 400 {
			owned = envelope.New(agentAccessEnvelopeCode(recorder.status), "agent_access.refused", "the request was refused")
			if failure != nil {
				owned = owned.WithDiagnostic(failure)
			}
		}
		agentAccessLog(admission, r.URL.Path, inv, started, owned)
	})
}

type agentAccessStatus struct {
	http.ResponseWriter
	status int
}

func (s *agentAccessStatus) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func agentAccessEnvelopeCode(status int) envelope.Code {
	switch status {
	case http.StatusBadRequest, http.StatusMethodNotAllowed:
		return envelope.CodeInvalidArgument
	case http.StatusUnauthorized:
		return envelope.CodeUnauthenticated
	case http.StatusForbidden:
		return envelope.CodePermissionDenied
	case http.StatusNotFound:
		return envelope.CodeNotFound
	case http.StatusConflict:
		return envelope.CodeAborted
	}
	return envelope.CodeUnavailable
}

func agentAccessLog(admission transport.Config, path string, inv *transport.Invocation, started time.Time, failure *envelope.Error) {
	if admission.Logger == nil {
		return
	}
	admission.Logger.LogRequest(transport.NewLogRecord(path, transport.KindHTTPEdge, inv, "", time.Since(started), failure))
}

func (h *AgentAccessHTTP) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func decodeAgentAccessBody(w http.ResponseWriter, r *http.Request, into any) bool {
	if r.URL.RawQuery != "" {
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(into) != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

// serve answers one admitted request and returns the cause of a refusal for the
// request log. It never writes a body the caller could not have read.
func (h *AgentAccessHTTP) serve(w http.ResponseWriter, r *http.Request) error {
	principal, ok := trust.FromContext(r.Context())
	if !ok || principal == nil || !time.Now().Before(principal.ExpiresAt()) {
		agentAccessError(w, http.StatusUnauthorized, "session_required")
		return errors.New("no session")
	}
	if principal.SubjectKind() != trust.SubjectKindHuman || !principal.Assurance().AtLeast(trust.AssuranceLow) {
		agentAccessError(w, http.StatusForbidden, "permission_denied")
		return errors.New("not a signed-in person")
	}
	tenant, subject := principal.Tenant().String(), principal.Subject()
	path := r.URL.Path
	reading := r.Method == http.MethodGet
	if !reading && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		agentAccessError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return errors.New("method not allowed")
	}
	admin := principalHasAdministratorRole(principal)
	if admin {
		defer h.Admins.enter(tenant, subject)()
	}
	var result any
	var err error
	switch {
	case path == AgentAccessPath+"/snapshot" && reading:
		result, err = h.snapshot(r.Context(), principal)
	case path == AgentAccessPath+"/link/start" && !reading:
		var body agentAccessLinkBody
		if !decodeAgentAccessBody(w, r, &body) {
			agentAccessError(w, http.StatusBadRequest, "invalid_request")
			return errors.New("invalid body")
		}
		result, err = h.linkStart(r.Context(), principal, body)
	case path == AgentAccessPath+"/link/complete" && !reading:
		var body agentAccessCompleteBody
		if !decodeAgentAccessBody(w, r, &body) {
			agentAccessError(w, http.StatusBadRequest, "invalid_request")
			return errors.New("invalid body")
		}
		result, err = h.linkComplete(r.Context(), principal, body)
	case path == AgentAccessPath+"/unlink" && !reading:
		var body agentAccessLinkBody
		if !decodeAgentAccessBody(w, r, &body) {
			agentAccessError(w, http.StatusBadRequest, "invalid_request")
			return errors.New("invalid body")
		}
		err = h.withUser(r.Context(), principal, func(s *agentaccess.UserSession) error { return s.UnlinkConnection(body.ConnectionID) })
		result = struct{ Unlinked bool }{err == nil}
	case path == AgentAccessPath+"/grants/revoke" && !reading:
		var body agentAccessRevokeBody
		if !decodeAgentAccessBody(w, r, &body) {
			agentAccessError(w, http.StatusBadRequest, "invalid_request")
			return errors.New("invalid body")
		}
		err = h.withUser(r.Context(), principal, func(s *agentaccess.UserSession) error { return s.RevokeDelegation(body.TaskID) })
		result = struct{ Revoked bool }{err == nil}
	case strings.HasPrefix(path, AgentAccessPath+"/console"):
		if !admin {
			agentAccessError(w, http.StatusForbidden, "permission_denied")
			return agentaccess.ErrDenied
		}
		result, err = h.console(w, r, principal, reading)
	case path == AgentCostPath+"/report" && reading:
		result, err = h.costReport(r.Context(), principal, admin)
	case path == AgentCostPath+"/limits" && !reading:
		var body agentCostLimitsBody
		if !decodeAgentAccessBody(w, r, &body) {
			agentAccessError(w, http.StatusBadRequest, "invalid_request")
			return errors.New("invalid body")
		}
		result, err = h.costLimits(principal, body)
	case path == AgentCostPath+"/limit" && !reading:
		var body agentCostLimitBody
		if !decodeAgentAccessBody(w, r, &body) {
			agentAccessError(w, http.StatusBadRequest, "invalid_request")
			return errors.New("invalid body")
		}
		result, err = h.costSave(principal, body)
	case path == AgentTaskBudgetPath && !reading:
		var body agentTaskBudgetBody
		if !decodeAgentAccessBody(w, r, &body) {
			agentAccessError(w, http.StatusBadRequest, "invalid_request")
			return errors.New("invalid body")
		}
		if h.Extender == nil {
			err = agentaccess.ErrUnavailable
		} else {
			err = h.Extender.ExtendTaskBudget(r.Context(), principal, body.TaskID, body.RequestID)
		}
		result = struct{ Extended bool }{err == nil}
	default:
		agentAccessError(w, http.StatusNotFound, "not_found")
		return errors.New("unknown route")
	}
	if err != nil {
		status, code := agentAccessErrorStatus(err)
		agentAccessError(w, status, code)
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
	return nil
}

var (
	errAgentAccessBadRequest = errors.New("agent access: invalid request body")
	errAgentAccessNotFound   = errors.New("agent access: unknown route")
)

func agentAccessErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, agentaccess.ErrDenied), errors.Is(err, agentcost.ErrDenied), errors.Is(err, agentconnect.ErrDenied), errors.Is(err, agentclient.ErrNotAuthorized):
		return http.StatusForbidden, "permission_denied"
	case errors.Is(err, agentaccess.ErrSeparation):
		return http.StatusForbidden, "separation_of_duties"
	case errors.Is(err, agentaccess.ErrStateRejected):
		return http.StatusBadRequest, "state_rejected"
	case errors.Is(err, agentaccess.ErrInvalid), errors.Is(err, agentcost.ErrInvalid), errors.Is(err, agentconnect.ErrInvalid):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, agentaccess.ErrRevisionAbsent), errors.Is(err, agentconnect.ErrNotFound), errors.Is(err, agentaccess.ErrNotLinkable):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, agentaccess.ErrWrongState):
		return http.StatusConflict, "conflict"
	case errors.Is(err, agentclient.ErrDisabled):
		return http.StatusConflict, "agents_disabled"
	case errors.Is(err, errAgentAccessBadRequest):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, errAgentAccessNotFound):
		return http.StatusNotFound, "not_found"
	}
	return http.StatusServiceUnavailable, "unavailable"
}

// userContext is the person's identity for grant matching: their assigned
// roles (falling back to the roles on their credential), the one population and
// their organization scope.
func (h *AgentAccessHTTP) userContext(ctx context.Context, principal *trust.Principal, subject string, credentialRoles []string, orgScope string) agentconnect.UserContext {
	roles := credentialRoles
	if h.Roles != nil {
		if snapshot, err := h.Roles.Load(ctx, principal.Tenant(), principal.OrganizationScopeID()); err == nil {
			roles = roleaccess.AssignedRoles(snapshot, subject, credentialRoles)
		}
	}
	if len(roles) == 0 {
		roles = []string{"employee"}
	}
	if strings.TrimSpace(orgScope) == "" {
		orgScope = principal.Tenant().String()
	}
	return agentconnect.UserContext{TenantID: principal.Tenant().String(), UserID: subject, Roles: roles, Population: "employees", OrganizationScopes: []string{orgScope}}
}

func (h *AgentAccessHTTP) withUser(ctx context.Context, principal *trust.Principal, fn func(*agentaccess.UserSession) error) error {
	if h.Users == nil {
		return agentaccess.ErrUnavailable
	}
	user := h.userContext(ctx, principal, principal.Subject(), principal.Roles(), principal.OrganizationScopeID())
	return fn(h.Users.For(user))
}

func (h *AgentAccessHTTP) snapshot(ctx context.Context, principal *trust.Principal) (any, error) {
	var snapshot productui.AgentAccessSnapshot
	err := h.withUser(ctx, principal, func(s *agentaccess.UserSession) error {
		var err error
		snapshot, err = s.Snapshot()
		return err
	})
	return snapshot, err
}

func (h *AgentAccessHTTP) linkStart(ctx context.Context, principal *trust.Principal, body agentAccessLinkBody) (any, error) {
	var start productui.AgentAuthorizationStart
	err := h.withUser(ctx, principal, func(s *agentaccess.UserSession) error {
		var err error
		start, err = s.StartProviderAuthorization(body.ConnectionID)
		return err
	})
	return start, err
}

func (h *AgentAccessHTTP) linkComplete(ctx context.Context, principal *trust.Principal, body agentAccessCompleteBody) (any, error) {
	if h.Users == nil {
		return nil, agentaccess.ErrUnavailable
	}
	user := h.userContext(ctx, principal, principal.Subject(), principal.Roles(), principal.OrganizationScopeID())
	connection, err := h.Users.CompleteLink(ctx, user, body.State, body.Code)
	return struct{ ConnectionID string }{connection}, err
}

func (h *AgentAccessHTTP) console(w http.ResponseWriter, r *http.Request, principal *trust.Principal, reading bool) (any, error) {
	if h.Console == nil {
		return nil, agentaccess.ErrUnavailable
	}
	tenant, subject := principal.Tenant().String(), principal.Subject()
	// Whether the administrator has passed step-up comes from their session's
	// assurance, never from the request.
	session := h.Console.For(tenant, subject).WithStepUp(principal.Assurance().AtLeast(trust.AssuranceSubstantial))
	path := r.URL.Path
	bad := func() (any, error) { return nil, errAgentAccessBadRequest }
	snapshot := func() (any, error) { return session.Snapshot("", nil, "") }
	if reading && path == AgentAccessPath+"/console" {
		return snapshot()
	}
	if reading {
		return nil, errAgentAccessNotFound
	}
	step := func(body agentAccessRevisionBody, run func(*agentaccess.AdminSession, string) error) (any, error) {
		if err := run(session, body.RevisionID); err != nil {
			return nil, err
		}
		return snapshot()
	}
	switch strings.TrimPrefix(path, AgentAccessPath+"/console") {
	case "/create":
		var body agentAccessCreateBody
		if !decodeAgentAccessBody(w, r, &body) {
			return bad()
		}
		if err := session.CreateConnectionRevision(body.Revision); err != nil {
			return nil, err
		}
		return snapshot()
	case "/import":
		var body agentAccessImportBody
		if !decodeAgentAccessBody(w, r, &body) {
			return bad()
		}
		if err := session.ImportMCPSnapshot(body.ConnectionID, body.SnapshotID); err != nil {
			return nil, err
		}
		return snapshot()
	case "/tier":
		var body agentAccessTierBody
		if !decodeAgentAccessBody(w, r, &body) {
			return bad()
		}
		if _, err := h.Console.SetTier(tenant, subject, body.RevisionID, body.SkillID, agentaccess.TierFromPage(body.Tier)); err != nil {
			return nil, err
		}
		return snapshot()
	case "/grants":
		var body agentAccessGrantsBody
		if !decodeAgentAccessBody(w, r, &body) {
			return bad()
		}
		grants := make([]agentconnect.GrantScope, 0, len(body.Grants))
		for _, grant := range body.Grants {
			grants = append(grants, agentconnect.GrantScope{ID: grant.ID, Roles: grant.Roles, Population: grant.Population, OrganizationScopes: grant.OrganizationScopes, Skills: grant.Skills})
		}
		if _, err := h.Console.SetGrants(tenant, subject, body.RevisionID, grants); err != nil {
			return nil, err
		}
		return snapshot()
	case "/request":
		var body agentAccessRevisionBody
		if !decodeAgentAccessBody(w, r, &body) {
			return bad()
		}
		return step(body, (*agentaccess.AdminSession).RequestSecondAdminApproval)
	case "/approve":
		var body agentAccessRevisionBody
		if !decodeAgentAccessBody(w, r, &body) {
			return bad()
		}
		return step(body, (*agentaccess.AdminSession).ApproveRevision)
	case "/publish":
		var body agentAccessRevisionBody
		if !decodeAgentAccessBody(w, r, &body) {
			return bad()
		}
		return step(body, (*agentaccess.AdminSession).PublishConnectionRevision)
	case "/rollback":
		var body agentAccessRevisionBody
		if !decodeAgentAccessBody(w, r, &body) {
			return bad()
		}
		return step(body, (*agentaccess.AdminSession).RollBack)
	case "/preview":
		var body agentAccessPreviewBody
		if !decodeAgentAccessBody(w, r, &body) {
			return bad()
		}
		if strings.TrimSpace(body.UserID) == "" {
			return bad()
		}
		// The chosen person's own assigned roles decide the preview; the
		// administrator's are not used for them.
		subjectUser := h.userContext(r.Context(), principal, body.UserID, nil, principal.OrganizationScopeID())
		label := strings.TrimSpace(body.Label)
		if label == "" {
			label = body.UserID
		}
		return session.Snapshot(label, &subjectUser, body.RevisionID)
	}
	return nil, errAgentAccessNotFound
}

func (h *AgentAccessHTTP) zone() *time.Location {
	if h.Zone != nil {
		return h.Zone
	}
	return time.UTC
}

func (h *AgentAccessHTTP) costReport(ctx context.Context, principal *trust.Principal, admin bool) (any, error) {
	if h.Owners == nil || h.Meter.Ledger == nil || h.Meter.Gate == nil {
		return nil, agentaccess.ErrUnavailable
	}
	tenant, subject := principal.Tenant().String(), principal.Subject()
	agents, err := h.Owners.OwnedAgents(tenant, subject)
	if err != nil {
		return nil, err
	}
	now := h.now()
	report, err := h.Meter.Ledger.ReportFor(h.Owners, tenant, subject, now, h.zone())
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, agent := range agents {
		names[agent.ID] = agent.Name
	}
	name := func(id string) string {
		if value := names[id]; value != "" {
			return value
		}
		return id
	}
	view := AgentCostReportView{Agents: agents, Report: productui.AgentCostReport{
		PerAnsweredQuestionMicros: report.PerAnsweredQuestionMicros, MonthToDateMicros: report.MonthToDateMicros, ForecastMonthMicros: report.ForecastMonthMicros,
		PerHundredMessagesMicros: report.PerHundredMessagesMicros, QuietSharePercent: report.QuietSharePercent,
		Notices: h.Notices.peek(tenant, subject),
	}}
	for _, day := range report.Trend {
		view.Report.Trend = append(view.Report.Trend, productui.AgentCostDay{Day: day.Day, Micros: day.SpendMicros, Runs: day.Runs})
	}
	for _, line := range report.RecentRuns {
		view.Report.Runs = append(view.Report.Runs, productui.AgentCostRun{Agent: name(line.AgentID), When: line.At.In(h.zone()).Format("2006-01-02 15:04"), Kind: string(line.Kind), Micros: line.SpendMicros})
	}
	for _, row := range report.PerAgentDay {
		view.Report.PerAgentDay = append(view.Report.PerAgentDay, productui.AgentCostAgentDay{Agent: name(row.AgentID), Day: row.Day, Micros: row.SpendMicros})
	}
	limits, err := h.costLimits(principal, agentCostLimitsBody{AgentIDs: agentIDs(agents)})
	if err != nil {
		return nil, err
	}
	view.Limits = limits.([]AgentSpendLimitView)
	_ = admin
	return view, nil
}

func agentIDs(agents []agentcoststore.OwnedAgent) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}
	return ids
}

// costLimits reads the agent-wide limit of each named agent. An owner or an
// administrator may read; anyone else is refused for that agent.
func (h *AgentAccessHTTP) costLimits(principal *trust.Principal, body agentCostLimitsBody) (any, error) {
	if h.Meter.Gate == nil {
		return nil, agentaccess.ErrUnavailable
	}
	if len(body.AgentIDs) > 200 {
		return nil, agentcost.ErrInvalid
	}
	tenant, subject := principal.Tenant().String(), principal.Subject()
	views := make([]AgentSpendLimitView, 0, len(body.AgentIDs))
	for _, id := range body.AgentIDs {
		limits, err := h.Meter.Gate.Limits(tenant, id, subject)
		if errors.Is(err, agentcost.ErrDenied) {
			views = append(views, AgentSpendLimitView{AgentID: id, Denied: true})
			continue
		}
		if err != nil {
			return nil, err
		}
		view := AgentSpendLimitView{AgentID: id}
		for _, limit := range limits {
			if limit.ConversationID == "" {
				view.MaxRunsPerDay, view.MaxSpendMicrosPerDay = limit.MaxRuns, limit.MaxSpendMicros
			}
		}
		if decision := h.Meter.Gate.Admit(agentcost.Subject{TenantID: tenant, AgentID: id}); !decision.Allowed && !decision.Unavailable {
			view.Reached = decision.Reached
		}
		views = append(views, view)
	}
	return views, nil
}

// costSave sets an agent's daily limits. The gate decides who may: an owner may
// raise or remove, a tenant administrator may only tighten, anyone else is
// refused; every change is written to the audit table in the same transaction.
func (h *AgentAccessHTTP) costSave(principal *trust.Principal, body agentCostLimitBody) (any, error) {
	if h.Meter.Gate == nil {
		return nil, agentaccess.ErrUnavailable
	}
	tenant, subject := principal.Tenant().String(), principal.Subject()
	err := h.Meter.Gate.Set(subject, agentcost.Limit{TenantID: tenant, AgentID: body.AgentID, MaxRuns: body.MaxRunsPerDay, MaxSpendMicros: body.MaxSpendMicrosPerDay})
	if err != nil {
		return nil, err
	}
	return h.costLimits(principal, agentCostLimitsBody{AgentIDs: []string{body.AgentID}})
}
