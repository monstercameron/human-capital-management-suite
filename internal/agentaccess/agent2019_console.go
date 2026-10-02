package agentaccess

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

var (
	ErrDenied         = errors.New("agentaccess: administrator permission required")
	ErrSeparation     = errors.New("agentaccess: a second administrator must approve this revision, with step-up")
	ErrWrongState     = errors.New("agentaccess: the revision is not in a state that allows this")
	ErrRevisionAbsent = errors.New("agentaccess: revision not found")
)

// Revision states. A published revision is replaced by the next published
// one and kept, so publication can be reversed by publishing an older one.
const (
	StatusDraft            = "DRAFT"
	StatusAwaitingApproval = "AWAITING_APPROVAL"
	StatusApproved         = "APPROVED"
	StatusPublished        = "PUBLISHED"
	StatusSuperseded       = "SUPERSEDED"
)

// Skill is one operation a connection exposes. Tier is the side-effect tier
// the administrator assigned; an imported tool has none until one is set.
type Skill struct {
	ID           string
	Tier         agentconnect.SideEffectTier
	Capability   string
	SharedRead   bool
	RecordFilter string
}

// Revision is one administrator-authored version of a connection's exposure.
type Revision struct {
	ConnectionID   string
	TenantID       string
	Number         uint64
	Provider       string
	Status         string
	CredentialMode agentconnect.CredentialMode
	MCPSnapshotID  string
	Skills         []Skill
	Grants         []agentconnect.GrantScope
	CreatedBy      string
	RequestedBy    string
	ApprovedBy     string
	ApprovedAt     time.Time
	StepUp         bool
	// Digest covers the content an approver saw. Any edit changes it, so an
	// approval never carries over to different content.
	Digest      string
	PublishedAt time.Time
}

// ID is the identifier the page uses for the revision.
func (r Revision) ID() string { return r.ConnectionID + "#" + strconv.FormatUint(r.Number, 10) }

// RequiresSecondAdmin is true for a brokered credential and for any grant of
// a T3 or T4 skill. The tier gate is stricter than the registry's own, which
// guards the brokered mode only.
func (r Revision) RequiresSecondAdmin() bool {
	if r.CredentialMode == agentconnect.Brokered {
		return true
	}
	granted := map[string]bool{}
	for _, grant := range r.Grants {
		for _, id := range grant.Skills {
			granted[id] = true
		}
	}
	for _, skill := range r.Skills {
		if granted[skill.ID] && highImpact(skill.Tier) {
			return true
		}
	}
	return false
}

func highImpact(tier agentconnect.SideEffectTier) bool {
	return tier == agentconnect.TierT3 || tier == agentconnect.TierT4
}

func (r Revision) digest() string {
	skills := append([]Skill(nil), r.Skills...)
	sort.Slice(skills, func(i, j int) bool { return skills[i].ID < skills[j].ID })
	grants := make([]agentconnect.GrantScope, len(r.Grants))
	for i, grant := range r.Grants {
		grant.Roles = sortedCopy(grant.Roles)
		grant.OrganizationScopes = sortedCopy(grant.OrganizationScopes)
		grant.Skills = sortedCopy(grant.Skills)
		grants[i] = grant
	}
	sort.Slice(grants, func(i, j int) bool { return grants[i].ID < grants[j].ID })
	raw, _ := json.Marshal(struct {
		Connection, Tenant, Mode, Snapshot string
		Number                             uint64
		Skills                             []Skill
		Grants                             []agentconnect.GrantScope
	}{r.ConnectionID, r.TenantID, string(r.CredentialMode), r.MCPSnapshotID, r.Number, skills, grants})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// AuditEvent records one console action. It never carries credential
// material.
type AuditEvent struct {
	At       time.Time
	Tenant   string
	Actor    string
	Action   string
	Revision string
}

// Authorizer says whether the actor administers agent connections for the
// tenant. The transport answers it from the signed-in session, not the page.
type Authorizer interface {
	CanAdminister(tenant, actor string) bool
}

// Publisher makes a revision the live one. The production implementation
// registers it with the agent connection registry; the console has already
// checked separation of duties by then.
type Publisher interface {
	Publish(revision Revision) error
}

// MCPTool is one tool in an imported MCP snapshot.
type MCPTool struct {
	Name     string
	ReadOnly bool
}

// MCPSnapshots reads an already captured tool snapshot. The console never
// connects to the MCP server itself.
type MCPSnapshots interface {
	Tools(tenant, snapshotID string) ([]MCPTool, error)
}

// Console is the administrator console's server side.
type Console struct {
	authorizer Authorizer
	publisher  Publisher
	snapshots  MCPSnapshots
	now        func() time.Time

	mu        sync.Mutex
	revisions map[string][]Revision
	audit     []AuditEvent
	// store and loaded are set by WithStore; without a store the console is
	// purely in memory.
	store  RevisionStore
	loaded map[string]bool
}

// NewConsole composes the console. snapshots may be nil: importing then fails
// with a plain error rather than silently doing nothing.
func NewConsole(authorizer Authorizer, publisher Publisher, snapshots MCPSnapshots, now func() time.Time) (*Console, error) {
	if authorizer == nil || publisher == nil {
		return nil, fmt.Errorf("%w: authorizer and publisher are required", ErrInvalid)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Console{authorizer: authorizer, publisher: publisher, snapshots: snapshots, now: now, revisions: map[string][]Revision{}}, nil
}

func key(tenant, connection string) string { return tenant + "\x00" + connection }

// AuditTrail returns the actions taken in a tenant, oldest first.
func (c *Console) AuditTrail(tenant string) []AuditEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store != nil {
		events, err := c.store.Audit(tenant)
		if err != nil {
			return nil
		}
		return events
	}
	var out []AuditEvent
	for _, event := range c.audit {
		if event.Tenant == tenant {
			out = append(out, event)
		}
	}
	return out
}

func (c *Console) latestLocked(tenant, connection string) (int, bool) {
	list := c.revisions[key(tenant, connection)]
	if len(list) == 0 {
		return 0, false
	}
	return len(list) - 1, true
}

func (c *Console) find(tenant, id string) (string, int, error) {
	connection, number, ok := strings.Cut(id, "#")
	if !ok {
		return "", 0, ErrRevisionAbsent
	}
	want, err := strconv.ParseUint(number, 10, 64)
	if err != nil {
		return "", 0, ErrRevisionAbsent
	}
	for index, revision := range c.revisions[key(tenant, connection)] {
		if revision.Number == want {
			return connection, index, nil
		}
	}
	return "", 0, ErrRevisionAbsent
}

func (c *Console) authorize(tenant, actor string) error {
	if strings.TrimSpace(actor) == "" || !c.authorizer.CanAdminister(tenant, actor) {
		return ErrDenied
	}
	return nil
}

// Create stores a new draft revision of a connection, numbered after the
// last one. The draft grants nothing until it is published.
func (c *Console) Create(tenant, actor string, draft Revision) (Revision, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return Revision{}, err
	}
	if strings.TrimSpace(draft.ConnectionID) == "" || draft.CredentialMode == "" {
		return Revision{}, fmt.Errorf("%w: connection and credential mode are required", ErrInvalid)
	}
	known := map[string]bool{}
	for _, skill := range draft.Skills {
		if strings.TrimSpace(skill.ID) == "" {
			return Revision{}, fmt.Errorf("%w: a skill has no id", ErrInvalid)
		}
		known[skill.ID] = true
	}
	for _, grant := range draft.Grants {
		if len(grant.Roles) == 0 || strings.TrimSpace(grant.Population) == "" || len(grant.OrganizationScopes) == 0 {
			return Revision{}, fmt.Errorf("%w: grant %q needs a role, a population and an organization scope", ErrInvalid, grant.ID)
		}
		for _, id := range grant.Skills {
			if !known[id] {
				return Revision{}, fmt.Errorf("%w: grant %q names unknown skill %q", ErrInvalid, grant.ID, id)
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return Revision{}, err
	}
	draft.TenantID, draft.CreatedBy, draft.Status = tenant, actor, StatusDraft
	draft.RequestedBy, draft.ApprovedBy, draft.ApprovedAt, draft.StepUp, draft.PublishedAt = "", "", time.Time{}, false, time.Time{}
	draft.Number = 1
	if index, ok := c.latestLocked(tenant, draft.ConnectionID); ok {
		draft.Number = c.revisions[key(tenant, draft.ConnectionID)][index].Number + 1
	}
	draft.Digest = draft.digest()
	if err := c.persistLocked(tenant, actor, "create", draft.ID(), draft); err != nil {
		return Revision{}, err
	}
	c.revisions[key(tenant, draft.ConnectionID)] = append(c.revisions[key(tenant, draft.ConnectionID)], draft)
	return draft, nil
}

// ImportSnapshot adds the tools of a captured MCP snapshot to the connection's
// newest draft. A tool that is not marked read-only comes in as T4 until an
// administrator assigns a lower tier, so an import never widens what agents
// can do by itself, and it needs the second administrator like any T4 grant.
func (c *Console) ImportSnapshot(tenant, actor, connectionID, snapshotID string) (Revision, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return Revision{}, err
	}
	if c.snapshots == nil {
		return Revision{}, fmt.Errorf("%w: no tool snapshot service is connected", ErrInvalid)
	}
	tools, err := c.snapshots.Tools(tenant, snapshotID)
	if err != nil {
		return Revision{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return Revision{}, err
	}
	index, ok := c.latestLocked(tenant, connectionID)
	if !ok {
		return Revision{}, ErrRevisionAbsent
	}
	list := c.revisions[key(tenant, connectionID)]
	revision := list[index]
	if revision.Status != StatusDraft {
		return Revision{}, ErrWrongState
	}
	have := map[string]bool{}
	for _, skill := range revision.Skills {
		have[skill.ID] = true
	}
	for _, tool := range tools {
		if have[tool.Name] {
			continue
		}
		tier := agentconnect.TierT4
		if tool.ReadOnly {
			tier = agentconnect.TierT0
		}
		revision.Skills = append(revision.Skills, Skill{ID: tool.Name, Tier: tier, Capability: tool.Name, SharedRead: tool.ReadOnly && revision.CredentialMode == agentconnect.Brokered})
	}
	revision.MCPSnapshotID = snapshotID
	revision.Digest = revision.digest()
	if err := c.persistLocked(tenant, actor, "import-snapshot", revision.ID(), revision); err != nil {
		return Revision{}, err
	}
	list[index] = revision
	return revision, nil
}

// SetTier assigns a skill's tier on a draft. It resets approval: whoever
// approved earlier approved different content.
func (c *Console) SetTier(tenant, actor, revisionID, skillID string, tier agentconnect.SideEffectTier) (Revision, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return Revision{}, err
	}
	switch tier {
	case agentconnect.TierT0, agentconnect.TierT1, agentconnect.TierT2, agentconnect.TierT3, agentconnect.TierT4:
	default:
		return Revision{}, ErrInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return Revision{}, err
	}
	connection, index, err := c.find(tenant, revisionID)
	if err != nil {
		return Revision{}, err
	}
	list := c.revisions[key(tenant, connection)]
	revision := list[index]
	if revision.Status == StatusPublished || revision.Status == StatusSuperseded {
		return Revision{}, ErrWrongState
	}
	changed := false
	for i := range revision.Skills {
		if revision.Skills[i].ID == skillID {
			revision.Skills[i].Tier, changed = tier, true
		}
	}
	if !changed {
		return Revision{}, ErrInvalid
	}
	revision.Status, revision.RequestedBy, revision.ApprovedBy, revision.ApprovedAt, revision.StepUp = StatusDraft, "", "", time.Time{}, false
	revision.Digest = revision.digest()
	if err := c.persistLocked(tenant, actor, "set-tier", revision.ID(), revision); err != nil {
		return Revision{}, err
	}
	list[index] = revision
	return revision, nil
}

// RequestApproval asks a second administrator to approve the draft as it is
// now.
func (c *Console) RequestApproval(tenant, actor, revisionID string) (Revision, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return Revision{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return Revision{}, err
	}
	connection, index, err := c.find(tenant, revisionID)
	if err != nil {
		return Revision{}, err
	}
	list := c.revisions[key(tenant, connection)]
	revision := list[index]
	if revision.Status != StatusDraft {
		return Revision{}, ErrWrongState
	}
	revision.Status, revision.RequestedBy = StatusAwaitingApproval, actor
	if err := c.persistLocked(tenant, actor, "request-approval", revision.ID(), revision); err != nil {
		return Revision{}, err
	}
	list[index] = revision
	return revision, nil
}

// Approve records the second administrator's approval. The approver must not
// be the author or the requester, must have passed step-up, and approves the
// digest the revision has at this moment.
func (c *Console) Approve(tenant, actor, revisionID string, stepUp bool) (Revision, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return Revision{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return Revision{}, err
	}
	connection, index, err := c.find(tenant, revisionID)
	if err != nil {
		return Revision{}, err
	}
	list := c.revisions[key(tenant, connection)]
	revision := list[index]
	if revision.Status != StatusAwaitingApproval {
		return Revision{}, ErrWrongState
	}
	if !stepUp || actor == revision.CreatedBy || actor == revision.RequestedBy || revision.Digest != revision.digest() {
		return Revision{}, ErrSeparation
	}
	revision.Status, revision.ApprovedBy, revision.ApprovedAt, revision.StepUp = StatusApproved, actor, c.now(), true
	if err := c.persistLocked(tenant, actor, "approve", revision.ID(), revision); err != nil {
		return Revision{}, err
	}
	list[index] = revision
	return revision, nil
}

// Publish makes the revision the live one. A revision that needs a second
// administrator is refused unless it is approved for exactly its current
// content. The previously published revision is kept as superseded.
func (c *Console) Publish(tenant, actor, revisionID string) (Revision, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return Revision{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return Revision{}, err
	}
	connection, index, err := c.find(tenant, revisionID)
	if err != nil {
		return Revision{}, err
	}
	list := c.revisions[key(tenant, connection)]
	revision := list[index]
	switch revision.Status {
	case StatusPublished, StatusSuperseded:
		return Revision{}, ErrWrongState
	}
	if revision.RequiresSecondAdmin() {
		if revision.Status != StatusApproved || !revision.StepUp || revision.ApprovedBy == "" || revision.ApprovedBy == revision.CreatedBy || revision.Digest != revision.digest() {
			return Revision{}, ErrSeparation
		}
	}
	return c.publishLocked(tenant, actor, list, index, "publish")
}

func (c *Console) publishLocked(tenant, actor string, list []Revision, index int, action string) (Revision, error) {
	revision := list[index]
	if err := c.publisher.Publish(revision); err != nil {
		return Revision{}, err
	}
	superseded := []Revision{}
	for i := range list {
		if list[i].Status == StatusPublished {
			older := list[i]
			older.Status = StatusSuperseded
			superseded = append(superseded, older)
		}
	}
	revision.Status, revision.PublishedAt = StatusPublished, c.now()
	// The record is written before the in-memory list changes, so a failed write
	// leaves the console showing what is stored.
	if err := c.persistLocked(tenant, actor, action, revision.ID(), append(superseded, revision)...); err != nil {
		return Revision{}, err
	}
	for _, older := range superseded {
		for i := range list {
			if list[i].Number == older.Number {
				list[i] = older
			}
		}
	}
	list[index] = revision
	return revision, nil
}

// Rollback republishes an earlier published revision under a new number, so
// the history only ever grows. The earlier revision's approval still covers
// it because its content is unchanged.
func (c *Console) Rollback(tenant, actor, revisionID string) (Revision, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return Revision{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return Revision{}, err
	}
	connection, index, err := c.find(tenant, revisionID)
	if err != nil {
		return Revision{}, err
	}
	k := key(tenant, connection)
	earlier := c.revisions[k][index]
	if earlier.Status != StatusSuperseded {
		return Revision{}, ErrWrongState
	}
	next := earlier
	next.Number = c.revisions[k][len(c.revisions[k])-1].Number + 1
	next.Status, next.PublishedAt = StatusApproved, time.Time{}
	if !earlier.RequiresSecondAdmin() {
		next.Status = StatusDraft
	}
	next.Digest = next.digest()
	c.revisions[k] = append(c.revisions[k], next)
	published, err := c.publishLocked(tenant, actor, c.revisions[k], len(c.revisions[k])-1, "rollback")
	if err != nil {
		// The new revision was never stored or made live: drop it again.
		c.revisions[k] = c.revisions[k][:len(c.revisions[k])-1]
		return Revision{}, err
	}
	return published, nil
}

// Revisions lists a connection's revisions, newest first.
func (c *Console) Revisions(tenant, actor string) ([]Revision, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return nil, err
	}
	var out []Revision
	for _, list := range c.revisions {
		for _, revision := range list {
			if revision.TenantID == tenant {
				out = append(out, revision)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ConnectionID != out[j].ConnectionID {
			return out[i].ConnectionID < out[j].ConnectionID
		}
		return out[i].Number > out[j].Number
	})
	return out, nil
}

// PreviewResult is what the user's agents could do with the connection under
// one revision, and what an administrator should look at before publishing.
type PreviewResult struct {
	Skills   []Skill
	Warnings []string
}

// Preview evaluates one revision for one person or population, using the same
// grant matching the registry uses at call time. Nothing is changed.
func (c *Console) Preview(tenant, actor, revisionID string, subject agentconnect.UserContext) (PreviewResult, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return PreviewResult{}, err
	}
	if subject.TenantID != tenant {
		return PreviewResult{}, ErrDenied
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return PreviewResult{}, err
	}
	connection, index, err := c.find(tenant, revisionID)
	if err != nil {
		return PreviewResult{}, err
	}
	revision := c.revisions[key(tenant, connection)][index]
	var result PreviewResult
	for _, skill := range revision.Skills {
		for _, grant := range revision.Grants {
			if grant.Matches(subject, skill.ID) {
				result.Skills = append(result.Skills, skill)
				break
			}
		}
	}
	sort.Slice(result.Skills, func(i, j int) bool { return result.Skills[i].ID < result.Skills[j].ID })
	for _, grant := range revision.Grants {
		wide := containsWildcard(grant.Roles) && grant.Population == agentconnect.AnyScope
		for _, id := range grant.Skills {
			for _, skill := range revision.Skills {
				if skill.ID == id && highImpact(skill.Tier) && wide {
					result.Warnings = append(result.Warnings, fmt.Sprintf("%s (%s) is granted to everyone.", skill.ID, tierWord(skill.Tier)))
				}
			}
		}
	}
	if revision.RequiresSecondAdmin() && revision.Status != StatusApproved && revision.Status != StatusPublished && revision.Status != StatusSuperseded {
		result.Warnings = append(result.Warnings, "Publishing needs a second administrator.")
	}
	return result, nil
}

func containsWildcard(values []string) bool {
	for _, value := range values {
		if value == agentconnect.AnyScope {
			return true
		}
	}
	return false
}

func tierWord(tier agentconnect.SideEffectTier) string {
	switch tier {
	case agentconnect.TierT3:
		return "T3"
	case agentconnect.TierT4:
		return "T4"
	}
	return string(tier)
}

// AdminSession implements productui.AgentAdminAccessClient for one signed-in
// administrator: every call is made as that actor in that tenant.
type AdminSession struct {
	console *Console
	tenant  string
	actor   string
	stepUp  bool
}

var _ productui.AgentAdminAccessClient = (*AdminSession)(nil)

// For binds the console to the administrator the transport resolved.
func (c *Console) For(tenant, actor string) *AdminSession {
	return &AdminSession{console: c, tenant: tenant, actor: actor}
}

// CreateConnectionRevision turns the page's draft into a stored one.
func (s *AdminSession) CreateConnectionRevision(draft productui.AgentConnectionRevision) error {
	revision := Revision{ConnectionID: draft.ID, Provider: draft.Provider, CredentialMode: agentconnect.CredentialMode(strings.ToUpper(draft.CredentialMode)), MCPSnapshotID: draft.MCPSnapshotID}
	if revision.CredentialMode == "" {
		revision.CredentialMode = agentconnect.UserDelegated
	}
	for _, grant := range draft.Grants {
		revision.Skills = append(revision.Skills, Skill{ID: grant.SkillID, Tier: tierFromPage(grant.Tier), Capability: grant.SkillID})
	}
	_, err := s.console.Create(s.tenant, s.actor, revision)
	return err
}

func tierFromPage(value string) agentconnect.SideEffectTier {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "T0", "T0_READ":
		return agentconnect.TierT0
	case "T1", "T1_PRIVATE_DRAFT":
		return agentconnect.TierT1
	case "T2", "T2_COMMUNICATE":
		return agentconnect.TierT2
	case "T3", "T3_SUBMIT_GOVERNED":
		return agentconnect.TierT3
	}
	// An unknown or missing tier is the most restrictive one.
	return agentconnect.TierT4
}

// ImportMCPSnapshot imports the snapshot into the connection's newest draft.
func (s *AdminSession) ImportMCPSnapshot(connectionID, snapshotID string) error {
	_, err := s.console.ImportSnapshot(s.tenant, s.actor, connectionID, snapshotID)
	return err
}

// PublishConnectionRevision publishes a revision the console allows.
func (s *AdminSession) PublishConnectionRevision(revisionID string) error {
	_, err := s.console.Publish(s.tenant, s.actor, revisionID)
	return err
}

// RequestSecondAdminApproval asks another administrator to approve.
func (s *AdminSession) RequestSecondAdminApproval(revisionID string) error {
	_, err := s.console.RequestApproval(s.tenant, s.actor, revisionID)
	return err
}

// WithStepUp returns the session marked as having passed step-up. Only the
// transport, after its own challenge, calls it; the page cannot.
func (s *AdminSession) WithStepUp(passed bool) *AdminSession {
	copy := *s
	copy.stepUp = passed
	return &copy
}

// ApproveRevision is the second administrator's approval; the step-up result
// comes from the session, not from the page.
func (s *AdminSession) ApproveRevision(revisionID string) error {
	_, err := s.console.Approve(s.tenant, s.actor, revisionID, s.stepUp)
	return err
}

// RollBack republishes an earlier revision.
func (s *AdminSession) RollBack(revisionID string) error {
	_, err := s.console.Rollback(s.tenant, s.actor, revisionID)
	return err
}

// Snapshot is the console page's data: every revision, newest first, and the
// preview for the person the administrator chose, when they chose one.
func (s *AdminSession) Snapshot(subjectLabel string, subject *agentconnect.UserContext, previewRevisionID string) (productui.AgentAdminAccessSnapshot, error) {
	revisions, err := s.console.Revisions(s.tenant, s.actor)
	if err != nil {
		return productui.AgentAdminAccessSnapshot{}, err
	}
	var snapshot productui.AgentAdminAccessSnapshot
	for _, revision := range revisions {
		snapshot.Revisions = append(snapshot.Revisions, pageRevision(revision))
	}
	if subject != nil && previewRevisionID != "" {
		preview, err := s.console.Preview(s.tenant, s.actor, previewRevisionID, *subject)
		if err != nil {
			return productui.AgentAdminAccessSnapshot{}, err
		}
		connection := productui.AgentAccessConnection{ID: previewRevisionID, Provider: previewRevisionID, LinkState: "linked"}
		for _, skill := range preview.Skills {
			connection.Skills = append(connection.Skills, productui.AgentAccessSkill{ID: skill.ID, Name: skill.ID, Tier: string(skill.Tier)})
		}
		snapshot.Preview = productui.AgentEffectiveAccessPreview{SubjectLabel: subjectLabel, Connections: []productui.AgentAccessConnection{connection}, Warnings: preview.Warnings}
		snapshot.PreviewReady, snapshot.PreviewSubject = true, subjectLabel
	}
	return snapshot, nil
}

func pageRevision(revision Revision) productui.AgentConnectionRevision {
	out := productui.AgentConnectionRevision{
		ID: revision.ID(), Provider: revision.Provider, Revision: strconv.FormatUint(revision.Number, 10), Status: revision.Status,
		CredentialMode: string(revision.CredentialMode), MCPSnapshotID: revision.MCPSnapshotID,
		RequiresSecondAdmin: revision.RequiresSecondAdmin(),
		SecondAdminApproved: revision.Status == StatusApproved || revision.Status == StatusPublished || revision.Status == StatusSuperseded,
	}
	for _, scope := range revision.Grants {
		out.GrantRows = append(out.GrantRows, productui.AgentGrantRow{ID: scope.ID, Roles: append([]string(nil), scope.Roles...), Population: scope.Population, OrganizationScopes: append([]string(nil), scope.OrganizationScopes...), Skills: append([]string(nil), scope.Skills...)})
	}
	for _, skill := range revision.Skills {
		grant := productui.AgentAdminSkillGrant{SkillID: skill.ID, SkillName: skill.ID, Tier: string(skill.Tier), RequiresSecondAdmin: highImpact(skill.Tier)}
		for _, scope := range revision.Grants {
			for _, id := range scope.Skills {
				if id != skill.ID {
					continue
				}
				for _, role := range scope.Roles {
					grant.Scopes = append(grant.Scopes, productui.AgentGrantScope{Kind: "role", Value: role})
				}
				grant.Scopes = append(grant.Scopes, productui.AgentGrantScope{Kind: "population", Value: scope.Population})
				for _, org := range scope.OrganizationScopes {
					grant.Scopes = append(grant.Scopes, productui.AgentGrantScope{Kind: "organization", Value: org})
				}
			}
		}
		out.Grants = append(out.Grants, grant)
	}
	return out
}
