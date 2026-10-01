// Package client adapts an administrator-owned MCP connection to the agent
// security boundary. Discovery is an explicit import/review operation; the
// runtime client never refreshes the remote tool list.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

const (
	// MaxResponseBytes bounds both discovery and invocation responses before
	// they can enter a parser or an agent result.
	MaxResponseBytes = 1 << 20
	MaxTools         = 256
	MaxDescription   = 16 << 10
	MaxSchemaBytes   = 256 << 10
)

var (
	ErrInvalidConnection = errors.New("agentmcp: invalid connection")
	ErrInvalidResponse   = errors.New("agentmcp: invalid MCP response")
	ErrResponseTooLarge  = errors.New("agentmcp: MCP response is too large")
	ErrSnapshotMismatch  = errors.New("agentmcp: snapshot digest mismatch")
	ErrReviewRequired    = errors.New("agentmcp: tool requires review")
	ErrCredential        = errors.New("agentmcp: invalid audience-bound credential lease")
)

// Tier is the reviewed side-effect ceiling assigned by an administrator.
// T2-T4 are retained in the reviewed snapshot, but the existing agentsecurity
// gateway refuses them until the corresponding approval path admits them.
type Tier uint8

const (
	TierRead          Tier = 0
	TierPrivateDraft  Tier = 1
	TierCommunicate   Tier = 2
	TierGovernedWrite Tier = 3
	TierExternalWrite Tier = 4
)

func (t Tier) valid() bool { return t <= TierExternalWrite }

// Connection is the non-secret identity of an administrator-owned MCP
// server. Credentials are resolved by the connector and lease issuer, never
// carried in this value or in a tool argument.
type Connection struct {
	ID                string
	Endpoint          string
	ResourceIndicator string
	ProtocolVersion   string
}

// ToolSpec is a server-advertised tool. It is untrusted until included in a
// reviewed snapshot. Description is intentionally available to the reviewer,
// not to the runtime planner through this type.
type ToolSpec struct {
	Name         string
	Description  string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
}

// ToolList is the bounded, parsed form of an MCP tools/list response.
type ToolList struct {
	ProtocolVersion string
	Tools           []ToolSpec
}

// Snapshot pins the exact server-advertised tool set returned by import. A
// Snapshot is not executable; ReviewSnapshot is the only way to produce a
// runtime-usable ReviewedSnapshot.
type Snapshot struct {
	Connection      Connection
	ProtocolVersion string
	Tools           []ToolSpec
	Digest          string
}

// ToolReview is the administrator's review decision for one exact tool in a
// pinned snapshot.
type ToolReview struct {
	ToolName   string
	SkillID    string
	ReviewedBy string
	ReviewRef  string
	Tier       Tier
	DataScope  []string
	Capability string
	Version    uint32
	Cost       int
}

// SkillDefinition is the immutable reviewed projection used by the runtime
// gateway. Its snapshot digest binds the definition to the imported tool
// description and schemas.
type SkillDefinition struct {
	ID             string
	ConnectionID   string
	SnapshotDigest string
	ToolName       string
	Description    string
	InputSchema    json.RawMessage
	OutputSchema   json.RawMessage
	Tier           Tier
	DataScope      []string
	Capability     string
	Version        uint32
	Cost           int
	ReviewedBy     string
	ReviewRef      string
}

// ReviewedSnapshot contains only tools with an explicit review decision.
type ReviewedSnapshot struct {
	Snapshot Snapshot
	Skills   []SkillDefinition
}

// PlannerTool is a reviewed-only projection. An unreviewed Snapshot has no
// method that produces this projection.
type PlannerTool struct {
	ID           string
	ToolName     string
	Description  string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	Tier         Tier
}

// CredentialRequest asks the server-side broker for a lease scoped to one
// MCP resource. It has no field for an HCM token.
type CredentialRequest struct {
	ConnectionID      string
	ResourceIndicator string
	Tool              string
	Purpose           string
}

// CredentialLease is an opaque server-held credential reference. Handle is a
// broker reference, not an access token; the MCP client has no API accepting a
// caller-supplied token.
type CredentialLease struct {
	ConnectionID string
	Audience     string
	Handle       string
	ExpiresAt    time.Time
}

// NewCredentialLease is intended for a governed credential broker and test
// doubles. Handle must identify a server-held secret, never a bearer token.
func NewCredentialLease(connectionID, audience, handle string, expiresAt time.Time) (CredentialLease, error) {
	lease := CredentialLease{ConnectionID: connectionID, Audience: audience, Handle: handle, ExpiresAt: expiresAt}
	if err := validateLease(lease); err != nil {
		return CredentialLease{}, err
	}
	return lease, nil
}

// Connector is the MCP connector SPI. Implementations own transport and the
// administrator's connection credential. Runtime calls receive only a
// destination-bound lease from CredentialIssuer.
type Connector interface {
	ListTools(context.Context, Connection) ([]byte, error)
	Call(context.Context, Connection, CredentialLease, CallRequest) ([]byte, error)
}

// CredentialIssuer exchanges a run-bound delegated grant for an
// audience-bound MCP lease. It must reject a revoked grant before returning.
type CredentialIssuer interface {
	Issue(context.Context, CredentialRequest) (CredentialLease, error)
}

// CallRequest is the only payload sent to an MCP server. It contains the
// exact reviewed tool name and canonical arguments, never HCM credentials.
type CallRequest struct {
	Tool           string
	Arguments      json.RawMessage
	SnapshotDigest string
}

// CallResult preserves the typed gateway result and a quarantined semantic
// observation. The observation can never become a canonical fact because MCP
// results carry external taint.
type CallResult struct {
	Tool           string
	SnapshotDigest string
	Value          json.RawMessage
	Typed          agentsecurity.TypedResult
	Observation    agentsecurity.Datum
}

// ImportSnapshot performs the only discovery operation. The connector's
// ListTools implementation is the administrator connection; no user token is
// accepted by this API.
func ImportSnapshot(ctx context.Context, connector Connector, connection Connection) (Snapshot, error) {
	if err := validContext(ctx); err != nil {
		return Snapshot{}, err
	}
	if connector == nil {
		return Snapshot{}, fmt.Errorf("%w: connector is required", ErrInvalidConnection)
	}
	if err := validateConnection(connection); err != nil {
		return Snapshot{}, err
	}
	raw, err := connector.ListTools(ctx, connection)
	if err != nil {
		return Snapshot{}, err
	}
	list, err := ParseToolsListResponse(raw)
	if err != nil {
		return Snapshot{}, err
	}
	if list.ProtocolVersion == "" {
		list.ProtocolVersion = connection.ProtocolVersion
	}
	digest, err := digestSnapshot(connection, list)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Connection: connection, ProtocolVersion: list.ProtocolVersion, Tools: cloneTools(list.Tools), Digest: digest}, nil
}

// ReviewSnapshot converts a pinned discovery result into reviewed skill
// definitions. Reviews for unknown tools or a changed snapshot are refused;
// unreviewed tools are omitted from the executable projection.
func ReviewSnapshot(snapshot Snapshot, reviews []ToolReview) (ReviewedSnapshot, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return ReviewedSnapshot{}, err
	}
	byName := make(map[string]ToolSpec, len(snapshot.Tools))
	for _, tool := range snapshot.Tools {
		byName[tool.Name] = tool
	}
	seen := make(map[string]struct{}, len(reviews))
	skills := make([]SkillDefinition, 0, len(reviews))
	for _, review := range reviews {
		if _, duplicate := seen[review.ToolName]; duplicate {
			return ReviewedSnapshot{}, fmt.Errorf("%w: duplicate review for %q", ErrReviewRequired, review.ToolName)
		}
		seen[review.ToolName] = struct{}{}
		tool, ok := byName[review.ToolName]
		if !ok || strings.TrimSpace(review.ReviewedBy) == "" || strings.TrimSpace(review.ReviewRef) == "" || !review.Tier.valid() {
			return ReviewedSnapshot{}, fmt.Errorf("%w: review for %q is incomplete or not in the snapshot", ErrReviewRequired, review.ToolName)
		}
		if len(review.DataScope) == 0 {
			review.DataScope = []string{"external:mcp"}
		}
		for _, scope := range review.DataScope {
			if strings.TrimSpace(scope) == "" || strings.TrimSpace(scope) != scope {
				return ReviewedSnapshot{}, fmt.Errorf("%w: invalid data scope for %q", ErrReviewRequired, review.ToolName)
			}
		}
		id := review.SkillID
		if id == "" {
			id = "mcp." + snapshot.Connection.ID + "." + tool.Name
		}
		capability := review.Capability
		if capability == "" {
			capability = "mcp." + snapshot.Connection.ID + "." + tool.Name
		}
		version := review.Version
		if version == 0 {
			version = 1
		}
		cost := review.Cost
		if cost <= 0 {
			cost = 1
		}
		skills = append(skills, SkillDefinition{ID: id, ConnectionID: snapshot.Connection.ID, SnapshotDigest: snapshot.Digest, ToolName: tool.Name, Description: tool.Description, InputSchema: cloneRaw(tool.InputSchema), OutputSchema: cloneRaw(tool.OutputSchema), Tier: review.Tier, DataScope: append([]string(nil), review.DataScope...), Capability: capability, Version: version, Cost: cost, ReviewedBy: review.ReviewedBy, ReviewRef: review.ReviewRef})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].ToolName < skills[j].ToolName })
	return ReviewedSnapshot{Snapshot: cloneSnapshot(snapshot), Skills: skills}, nil
}

// PlannerTools exposes only reviewed definitions. Descriptions are therefore
// unavailable to a planner until an administrator has reviewed the snapshot.
func (s ReviewedSnapshot) PlannerTools() []PlannerTool {
	tools := make([]PlannerTool, 0, len(s.Skills))
	for _, skill := range s.Skills {
		tools = append(tools, PlannerTool{ID: skill.ID, ToolName: skill.ToolName, Description: skill.Description, InputSchema: cloneRaw(skill.InputSchema), OutputSchema: cloneRaw(skill.OutputSchema), Tier: skill.Tier})
	}
	return tools
}

// Client executes only the exact reviewed snapshot supplied at construction.
type Client struct {
	connector  Connector
	issuer     CredentialIssuer
	connection Connection
	snapshot   ReviewedSnapshot
	skills     map[string]SkillDefinition
}

func NewClient(connector Connector, issuer CredentialIssuer, connection Connection, snapshot ReviewedSnapshot) (*Client, error) {
	if connector == nil || issuer == nil {
		return nil, fmt.Errorf("%w: connector and credential issuer are required", ErrInvalidConnection)
	}
	if err := validateConnection(connection); err != nil {
		return nil, err
	}
	if err := validateSnapshot(snapshot.Snapshot); err != nil || snapshot.Snapshot.Connection.ID != connection.ID {
		return nil, fmt.Errorf("%w: reviewed snapshot does not bind to connection", ErrSnapshotMismatch)
	}
	skills := make(map[string]SkillDefinition, len(snapshot.Skills))
	for _, skill := range snapshot.Skills {
		if skill.ConnectionID != connection.ID || skill.SnapshotDigest != snapshot.Snapshot.Digest || skill.ToolName == "" {
			return nil, fmt.Errorf("%w: skill %q is not pinned to the snapshot", ErrSnapshotMismatch, skill.ToolName)
		}
		if _, exists := skills[skill.ToolName]; exists {
			return nil, fmt.Errorf("%w: duplicate reviewed tool %q", ErrSnapshotMismatch, skill.ToolName)
		}
		skills[skill.ToolName] = cloneSkill(skill)
	}
	return &Client{connector: connector, issuer: issuer, connection: connection, snapshot: cloneReviewedSnapshot(snapshot), skills: skills}, nil
}

// Call admits the exact reviewed tool before issuing a credential or touching
// the remote server. It deliberately does not call ListTools.
func (c *Client) Call(ctx context.Context, call agentsecurity.ToolCall) (CallResult, error) {
	if err := validContext(ctx); err != nil {
		return CallResult{}, err
	}
	if c == nil || c.connector == nil || c.issuer == nil {
		return CallResult{}, fmt.Errorf("%w: client is not configured", ErrInvalidConnection)
	}
	skill, ok := c.skills[call.Tool]
	if !ok {
		return CallResult{}, fmt.Errorf("%w: %q", ErrReviewRequired, call.Tool)
	}
	source := "mcp:" + c.connection.ID + "/" + skill.ToolName
	bound := call
	bound.InputTaint = appendUnique(append([]string(nil), call.InputTaint...), string(agentsecurity.TaintExternal))
	bound.Provenance = appendUnique(append([]string(nil), call.Provenance...), source)
	gateway, err := gatewayForSkill(skill, bound.InputTaint, bound.Provenance)
	if err != nil {
		return CallResult{}, err
	}
	admission, err := gateway.Admit(bound)
	if err != nil {
		return CallResult{}, err
	}
	lease, err := c.issuer.Issue(ctx, CredentialRequest{ConnectionID: c.connection.ID, ResourceIndicator: c.connection.ResourceIndicator, Tool: skill.ToolName, Purpose: call.Purpose})
	if err != nil {
		return CallResult{}, err
	}
	if err := validateLeaseForConnection(lease, c.connection); err != nil {
		return CallResult{}, err
	}
	args, err := agentsecurity.DigestArguments(call.Args)
	if err != nil || args != call.ArgsDigest {
		return CallResult{}, &agentsecurity.Refusal{Code: agentsecurity.RefusalArgsDigest, Field: "args_digest", Detail: "arguments changed"}
	}
	raw, err := c.connector.Call(ctx, c.connection, lease, CallRequest{Tool: skill.ToolName, Arguments: mustJSON(call.Args), SnapshotDigest: c.snapshot.Snapshot.Digest})
	if err != nil {
		return CallResult{}, err
	}
	if len(raw) > MaxResponseBytes {
		return CallResult{}, ErrResponseTooLarge
	}
	typed, err := gateway.ValidateOutput(admission, skill.ToolName, raw)
	if err != nil {
		return CallResult{}, err
	}
	digest := contentDigest(raw)
	observation, err := gateway.Observe(agentsecurity.SourceMCP, string(raw), agentsecurity.KindObservation, agentsecurity.Citation{SourceID: source, Location: skill.ToolName, Digest: digest})
	if err != nil {
		return CallResult{}, err
	}
	return CallResult{Tool: skill.ToolName, SnapshotDigest: c.snapshot.Snapshot.Digest, Value: cloneRaw(raw), Typed: typed, Observation: observation}, nil
}

// ParseToolsListResponse accepts the MCP direct tools/list result and the
// JSON-RPC envelope. It validates and bounds all untrusted fields.
func ParseToolsListResponse(raw []byte) (ToolList, error) {
	if len(raw) > MaxResponseBytes {
		return ToolList{}, ErrResponseTooLarge
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return ToolList{}, ErrInvalidResponse
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return ToolList{}, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	payload := raw
	if result, ok := top["result"]; ok {
		payload = result
	}
	var payloadObject map[string]json.RawMessage
	if err := json.Unmarshal(payload, &payloadObject); err != nil {
		return ToolList{}, ErrInvalidResponse
	}
	toolsRaw, hasTools := payloadObject["tools"]
	if !hasTools {
		return ToolList{}, ErrInvalidResponse
	}
	var envelope struct {
		ProtocolVersion string            `json:"protocolVersion"`
		Tools           []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil || len(envelope.Tools) > MaxTools || string(toolsRaw) == "null" {
		return ToolList{}, ErrInvalidResponse
	}
	tools := make([]ToolSpec, 0, len(envelope.Tools))
	seen := make(map[string]struct{}, len(envelope.Tools))
	for _, encoded := range envelope.Tools {
		var tool struct {
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			InputSchema  json.RawMessage `json:"inputSchema"`
			OutputSchema json.RawMessage `json:"outputSchema"`
		}
		if err := json.Unmarshal(encoded, &tool); err != nil || strings.TrimSpace(tool.Name) == "" || len(tool.Description) > MaxDescription || len(tool.InputSchema) == 0 || len(tool.InputSchema) > MaxSchemaBytes || !json.Valid(tool.InputSchema) || (len(tool.OutputSchema) > 0 && (len(tool.OutputSchema) > MaxSchemaBytes || !json.Valid(tool.OutputSchema))) {
			return ToolList{}, ErrInvalidResponse
		}
		if _, exists := seen[tool.Name]; exists || strings.ContainsAny(tool.Name, " \t\r\n") {
			return ToolList{}, ErrInvalidResponse
		}
		seen[tool.Name] = struct{}{}
		tools = append(tools, ToolSpec{Name: tool.Name, Description: tool.Description, InputSchema: compactJSON(tool.InputSchema), OutputSchema: compactJSON(tool.OutputSchema)})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return ToolList{ProtocolVersion: envelope.ProtocolVersion, Tools: tools}, nil
}

func gatewayForSkill(skill SkillDefinition, taint, provenance []string) (*agentsecurity.ToolGateway, error) {
	class := agentsecurity.ToolRead
	switch skill.Tier {
	case TierPrivateDraft:
		class = agentsecurity.ToolDraft
	case TierCommunicate:
		class = agentsecurity.ToolSend
	case TierGovernedWrite:
		class = agentsecurity.ToolWrite
	case TierExternalWrite:
		class = agentsecurity.ToolExecute
	}
	schema := "mcp-output/" + skill.SnapshotDigest + "/" + skill.ToolName
	return agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{{Name: skill.ToolName, Capability: skill.Capability, Version: skill.Version, Class: class, DataScope: append([]string(nil), skill.DataScope...), Cost: skill.Cost, Schema: schema, Validate: func(value any) (agentsecurity.TypedResult, error) {
		raw, ok := value.([]byte)
		if !ok {
			if encoded, isRaw := value.(json.RawMessage); isRaw {
				raw = []byte(encoded)
			} else {
				return agentsecurity.TypedResult{}, ErrInvalidResponse
			}
		}
		if len(raw) == 0 || len(raw) > MaxResponseBytes || !json.Valid(raw) {
			return agentsecurity.TypedResult{}, ErrInvalidResponse
		}
		return agentsecurity.TypedResult{Schema: schema, Value: json.RawMessage(cloneRaw(raw)), Validated: true, Taint: append([]string(nil), taint...), Provenance: append([]string(nil), provenance...)}, nil
	}}})
}

func validateConnection(c Connection) error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Endpoint) == "" || strings.TrimSpace(c.ResourceIndicator) == "" || strings.TrimSpace(c.ID) != c.ID || strings.ContainsAny(c.ID, " \t\r\n") {
		return fmt.Errorf("%w: id, endpoint and resource indicator are required", ErrInvalidConnection)
	}
	return nil
}

func validateSnapshot(s Snapshot) error {
	if err := validateConnection(s.Connection); err != nil || len(s.Tools) > MaxTools || s.Digest == "" {
		return fmt.Errorf("%w: invalid snapshot", ErrSnapshotMismatch)
	}
	list := ToolList{ProtocolVersion: s.ProtocolVersion, Tools: cloneTools(s.Tools)}
	digest, err := digestSnapshot(s.Connection, list)
	if err != nil || digest != s.Digest {
		return ErrSnapshotMismatch
	}
	return nil
}

func digestSnapshot(connection Connection, list ToolList) (string, error) {
	canonical := struct {
		Connection      Connection `json:"connection"`
		ProtocolVersion string     `json:"protocol_version"`
		Tools           []ToolSpec `json:"tools"`
	}{connection, list.ProtocolVersion, cloneTools(list.Tools)}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("hcm-next-mcp-snapshot/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateLease(lease CredentialLease) error {
	if strings.TrimSpace(lease.ConnectionID) == "" || strings.TrimSpace(lease.Audience) == "" || strings.TrimSpace(lease.Handle) == "" || lease.ExpiresAt.IsZero() || !lease.ExpiresAt.After(time.Now()) {
		return ErrCredential
	}
	return nil
}

func validateLeaseForConnection(lease CredentialLease, connection Connection) error {
	if err := validateLease(lease); err != nil || lease.ConnectionID != connection.ID || lease.Audience != connection.ResourceIndicator {
		return ErrCredential
	}
	return nil
}

func validContext(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func contentDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mustJSON(args map[string]any) json.RawMessage {
	encoded, _ := json.Marshal(args)
	return encoded
}

func compactJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		return cloneRaw(raw)
	}
	return json.RawMessage(compacted.String())
}

func cloneRaw(raw json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), raw...) }

func cloneTools(tools []ToolSpec) []ToolSpec {
	result := make([]ToolSpec, len(tools))
	for i, tool := range tools {
		result[i] = ToolSpec{Name: tool.Name, Description: tool.Description, InputSchema: cloneRaw(tool.InputSchema), OutputSchema: cloneRaw(tool.OutputSchema)}
	}
	return result
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Tools = cloneTools(snapshot.Tools)
	return snapshot
}

func cloneSkill(skill SkillDefinition) SkillDefinition {
	skill.InputSchema = cloneRaw(skill.InputSchema)
	skill.OutputSchema = cloneRaw(skill.OutputSchema)
	skill.DataScope = append([]string(nil), skill.DataScope...)
	return skill
}

func cloneReviewedSnapshot(snapshot ReviewedSnapshot) ReviewedSnapshot {
	snapshot.Snapshot = cloneSnapshot(snapshot.Snapshot)
	snapshot.Skills = make([]SkillDefinition, len(snapshot.Skills))
	for i, skill := range snapshot.Skills {
		snapshot.Skills[i] = cloneSkill(skill)
	}
	return snapshot
}

func appendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		found := false
		for _, value := range values {
			if value == addition {
				found = true
				break
			}
		}
		if !found {
			values = append(values, addition)
		}
	}
	return values
}
