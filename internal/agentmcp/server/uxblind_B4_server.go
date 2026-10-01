// Package server exposes the HCM skill gateway through the MCP HTTP protocol.
//
// This package is intentionally an HTTP binding, not another authorization
// implementation. The TokenVerifier and SkillGateway seams are where the
// platform's delegated-token service and AGENT2-005 gate are attached. The
// server only performs transport-boundary checks and never forwards a raw
// bearer token to a skill owner.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

const (
	// ProtocolRevision is the MCP revision used by the skill registry.
	ProtocolRevision = agentskills.MCPProtocolRevision
	defaultMCPPath   = "/mcp"
	metadataPath     = "/.well-known/oauth-protected-resource"
	defaultMaxBody   = 1 << 20
)

var (
	ErrInvalidConfig = errors.New("agentmcp: invalid server configuration")
	ErrUnauthorized  = errors.New("agentmcp: unauthorized MCP request")
	ErrUnavailable   = errors.New("agentmcp: MCP dependency unavailable")
)

// TokenClaims is the security-relevant result of verifying an external
// client's bearer token. A verifier must resolve CurrentUser and
// ClientApproved from current server-side state; they are not values the MCP
// request may supply.
type TokenClaims struct {
	Subject        string
	Tenant         string
	ClientID       string
	Audience       []string
	Scopes         []string
	Active         bool
	CurrentUser    bool
	ClientApproved bool
	ExpiresAt      time.Time
}

// TokenVerifier validates a bearer token for the one resource audience. A
// production implementation should use the delegated-token/introspection
// service, not parse credentials in this package.
type TokenVerifier interface {
	Verify(context.Context, string, string) (TokenClaims, error)
}

// Caller is the trusted identity passed to the skill gate. It deliberately
// does not contain the original credential.
type Caller struct {
	UserID   string
	TenantID string
	ClientID string
	Scopes   []string
}

// SkillGateway is the AGENT2-005 seam. ListSkills must apply current
// user/role/organization/purpose authorization, and CallSkill must repeat that
// decision for the exact skill and arguments before invoking the common skill
// gateway.
type SkillGateway interface {
	ListSkills(context.Context, Caller) ([]agentskills.SkillRecord, error)
	CallSkill(context.Context, Caller, agentskills.SkillRecord, json.RawMessage) (SkillResult, error)
}

// SkillResult is the transport-neutral result of a T0/T1 skill call.
type SkillResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError"`
}

// Content is an MCP content item. Text is used for text content; Data is
// retained for typed content supplied by an implementation.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Data any    `json:"data,omitempty"`
}

// ApprovalRequest is the exact, immutable input that needs product approval
// before a T2-T4 skill can proceed.
type ApprovalRequest struct {
	Caller          Caller
	Skill           agentskills.SkillRecord
	Arguments       json.RawMessage
	ArgumentsDigest string
	Tier            agentskills.SideEffectTier
}

// Approval is a product-owned approval card link. The MCP server never treats
// a link as approval; the product must complete the approval and the common
// skill gateway must re-check the digest before execution.
type Approval struct {
	ID        string    `json:"id,omitempty"`
	URL       string    `json:"url"`
	Digest    string    `json:"digest"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// ApprovalService creates a product approval card without executing the skill.
type ApprovalService interface {
	CreateApproval(context.Context, ApprovalRequest) (Approval, error)
}

// Config wires the HTTP binding to the already-reviewed token and skill
// gateways.
type Config struct {
	// Resource is the exact protected-resource/audience URI, normally the MCP
	// endpoint URI. It is compared against every verified token audience.
	Resource string
	// MetadataURL is advertised in WWW-Authenticate. When empty, a relative
	// metadata path is used.
	MetadataURL          string
	AuthorizationServers []string
	ScopesSupported      []string
	MCPPath              string
	Verifier             TokenVerifier
	Skills               SkillGateway
	Approvals            ApprovalService
	// ApprovalURLBase is a test/bootstrap fallback only. A deployed product
	// should provide Approvals so links identify a durable approval card.
	ApprovalURLBase string
	MaxBodyBytes    int64
	Clock           func() time.Time
}

// Server is an MCP streamable-HTTP JSON-RPC endpoint.
type Server struct {
	resource        string
	metadataURL     string
	authorization   []string
	scopes          []string
	mcpPath         string
	verifier        TokenVerifier
	skills          SkillGateway
	approvals       ApprovalService
	approvalURLBase string
	maxBodyBytes    int64
	clock           func() time.Time
}

// New validates the transport binding. Authorization and skill decisions are
// deliberately delegated to the injected ports.
func New(cfg Config) (*Server, error) {
	if strings.TrimSpace(cfg.Resource) == "" || cfg.Verifier == nil || cfg.Skills == nil {
		return nil, fmt.Errorf("%w: resource, verifier and skills are required", ErrInvalidConfig)
	}
	path := cfg.MCPPath
	if path == "" {
		path = defaultMCPPath
	}
	if !strings.HasPrefix(path, "/") || path == metadataPath {
		return nil, fmt.Errorf("%w: MCP path must be a non-metadata absolute path", ErrInvalidConfig)
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBody
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Server{
		resource: cfg.Resource, metadataURL: cfg.MetadataURL,
		authorization: append([]string(nil), cfg.AuthorizationServers...),
		scopes:        append([]string(nil), cfg.ScopesSupported...), mcpPath: path,
		verifier: cfg.Verifier, skills: cfg.Skills, approvals: cfg.Approvals,
		approvalURLBase: cfg.ApprovalURLBase, maxBodyBytes: maxBody, clock: clock,
	}, nil
}

// Handler returns the HTTP handler for the metadata and MCP routes.
func (s *Server) Handler() http.Handler { return s }

// ServeHTTP serves protected-resource metadata and the MCP JSON-RPC endpoint.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case metadataPath:
		s.serveMetadata(w, r)
	case s.mcpPath:
		s.serveMCP(w, r)
	default:
		http.NotFound(w, r)
	}
}

type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

func (s *Server) serveMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, protectedResourceMetadata{
		Resource: s.resource, AuthorizationServers: append([]string(nil), s.authorization...),
		ScopesSupported: append([]string(nil), s.scopes...), BearerMethodsSupported: []string{"header"},
	})
}

func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("MCP-Protocol-Version", ProtocolRevision)
	caller, ok := s.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+s.metadataLocation(r)+`"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	limited := http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	defer limited.Close()
	var request rpcRequest
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(&request); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}
	if err := ensureEOF(decoder); err != nil {
		writeRPCError(w, request.ID, -32600, "invalid request")
		return
	}
	if len(request.ID) == 0 {
		if request.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeRPCError(w, nil, -32600, "request id is required")
		return
	}
	result, code, message := s.dispatch(r.Context(), caller, request)
	if code != 0 {
		writeRPCError(w, request.ID, code, message)
		return
	}
	writeRPCResult(w, request.ID, result)
}

func (s *Server) authenticate(r *http.Request) (Caller, bool) {
	token, ok := bearerToken(r.Header.Values("Authorization"))
	if !ok {
		return Caller{}, false
	}
	claims, err := s.verifier.Verify(r.Context(), token, s.resource)
	if err != nil || strings.TrimSpace(claims.Subject) == "" || strings.TrimSpace(claims.Tenant) == "" || strings.TrimSpace(claims.ClientID) == "" {
		return Caller{}, false
	}
	if !claims.Active || !claims.CurrentUser || !claims.ClientApproved || !contains(claims.Audience, s.resource) {
		return Caller{}, false
	}
	if claims.ExpiresAt.IsZero() || !claims.ExpiresAt.After(s.clock()) {
		return Caller{}, false
	}
	return Caller{UserID: claims.Subject, TenantID: claims.Tenant, ClientID: claims.ClientID, Scopes: append([]string(nil), claims.Scopes...)}, true
}

func bearerToken(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s *Server) dispatch(ctx context.Context, caller Caller, req rpcRequest) (any, int, string) {
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		return nil, -32600, "invalid request"
	}
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params)
	case "tools/list":
		records, err := s.skills.ListSkills(ctx, caller)
		if err != nil {
			return nil, -32603, "skill discovery unavailable"
		}
		projection := make([]agentskills.SkillRecord, 0, len(records))
		for _, record := range records {
			if record.Status == agentskills.StatusActive && record.Definition.SideEffectTier <= agentskills.TierPrivateDraft {
				projection = append(projection, record)
			}
		}
		// Use the registry's exact projection shape without reconstructing
		// schemas or annotations in this transport package.
		tools := make([]agentskills.MCPTool, 0, len(projection))
		for _, record := range projection {
			tools = append(tools, mcpToolFromRecord(record))
		}
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		return agentskills.MCPToolsList{Tools: tools}, 0, ""
	case "tools/call":
		return s.call(ctx, caller, req.Params)
	default:
		return nil, -32601, "method not found"
	}
}

func (s *Server) initialize(raw json.RawMessage) (any, int, string) {
	if len(raw) != 0 && string(raw) != "null" {
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, -32602, "invalid initialize parameters"
		}
		if params.ProtocolVersion != "" && params.ProtocolVersion != ProtocolRevision {
			return nil, -32602, "unsupported MCP protocol revision"
		}
	}
	return map[string]any{
		"protocolVersion": ProtocolRevision,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]string{"name": "hcm-next", "version": "1"},
	}, 0, ""
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) call(ctx context.Context, caller Caller, raw json.RawMessage) (any, int, string) {
	var params callParams
	if len(raw) == 0 || json.Unmarshal(raw, &params) != nil || strings.TrimSpace(params.Name) == "" {
		return nil, -32602, "invalid tools/call parameters"
	}
	arguments, err := canonicalArguments(params.Arguments)
	if err != nil {
		return nil, -32602, "tool arguments must be a JSON object"
	}
	records, err := s.skills.ListSkills(ctx, caller)
	if err != nil {
		return nil, -32603, "skill discovery unavailable"
	}
	var record agentskills.SkillRecord
	found := false
	for _, candidate := range records {
		if candidate.Status == agentskills.StatusActive && agentskills.MCPToolName(candidate.Definition.Key()) == params.Name {
			record, found = candidate, true
			break
		}
	}
	if !found {
		return nil, -32602, "tool is not available"
	}
	if record.Definition.SideEffectTier > agentskills.TierPrivateDraft {
		return s.approval(ctx, caller, record, arguments)
	}
	result, err := s.skills.CallSkill(ctx, caller, record, arguments)
	if err != nil {
		return nil, -32603, "skill invocation failed"
	}
	return result, 0, ""
}

func (s *Server) approval(ctx context.Context, caller Caller, record agentskills.SkillRecord, arguments json.RawMessage) (any, int, string) {
	digest := digestArguments(record, arguments)
	var approval Approval
	var err error
	if s.approvals != nil {
		approval, err = s.approvals.CreateApproval(ctx, ApprovalRequest{Caller: caller, Skill: record, Arguments: append(json.RawMessage(nil), arguments...), ArgumentsDigest: digest, Tier: record.Definition.SideEffectTier})
	} else if s.approvalURLBase != "" {
		approval.URL = approvalLink(s.approvalURLBase, agentskills.MCPToolName(record.Definition.Key()), digest)
	} else {
		return nil, -32603, "approval service unavailable"
	}
	if err != nil || strings.TrimSpace(approval.URL) == "" {
		return nil, -32603, "approval service unavailable"
	}
	if approval.Digest != "" && approval.Digest != digest {
		return nil, -32603, "approval digest mismatch"
	}
	approval.Digest = digest
	return map[string]any{
		"content": []Content{{Type: "text", Text: "Approval required in HCM: " + approval.URL}},
		"isError": false,
		"_meta":   map[string]any{"hcmApproval": approval},
	}, 0, ""
}

func canonicalArguments(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errors.New("arguments are not an object")
	}
	return json.Marshal(object)
}

func digestArguments(record agentskills.SkillRecord, arguments json.RawMessage) string {
	hash := sha256.Sum256(append([]byte("hcm-next-mcp-approval/v1\x00"+agentskills.MCPToolName(record.Definition.Key())+"\x00"), arguments...))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func approvalLink(base, tool, digest string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	query := parsed.Query()
	query.Set("tool", tool)
	query.Set("digest", digest)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func mcpToolFromRecord(record agentskills.SkillRecord) agentskills.MCPTool {
	return agentskills.MCPTool{
		Name: agentskills.MCPToolName(record.Definition.Key()), Description: record.Definition.Description,
		InputSchema: append(json.RawMessage(nil), record.Definition.InputSchema...), OutputSchema: append(json.RawMessage(nil), record.Definition.OutputSchema...),
		Annotations: agentskills.MCPToolAnnotations{ReadOnlyHint: record.Definition.SideEffectTier == agentskills.TierRead && record.HighestCapabilityTier == agentskills.TierRead, DestructiveHint: false},
	}
}

func (s *Server) metadataLocation(r *http.Request) string {
	if s.metadataURL != "" {
		return s.metadataURL
	}
	if r.TLS == nil && r.Host == "" {
		return metadataPath
	}
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host + metadataPath
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
