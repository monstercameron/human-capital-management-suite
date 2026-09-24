package application

// This file owns the application-side adapter for the read-only integration
// service. The transport adapter forwards the generated RPCs here; this
// service is the only layer that knows how to project the connectivity plane
// onto that contract and how to derive tenant scope from the authenticated
// principal.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/diagnostics"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	integrationDefaultPageSize int32 = 50
	integrationMaxPageSize     int32 = 100
	integrationCursorPrefix          = "integration:v1:"
)

// IntegrationConnection binds one real connectivity connection to the
// connector implementation that serves it. OrgID and SystemID are carried as
// metadata because ConnectorConnection intentionally exposes only the
// security-safe accessors needed by the kernel.
type IntegrationConnection struct {
	Connection  *connectivity.ConnectorConnection
	Connector   connectivity.Connector
	OrgID       string
	SystemID    string
	Environment connectivity.Environment
	Residency   string
	AuthMode    connectivity.AuthMode
	CreatedAt   time.Time
}

// IntegrationOptions supplies the existing connectivity values to the
// application adapter. Definitions are immutable and versioned; connections
// and observations are the real values composed by the cell. A nil connector
// is valid for discovery and observation-history reads, while the diagnostic
// RPC reports skipped checks when no read-only provider implementation exists.
type IntegrationOptions struct {
	Definitions  ConnectorDefinitionLoader
	TenantID     func(string) uuid.UUID
	Connections  []IntegrationConnection
	Observations observe.ObservationStore
	Now          func() time.Time
}

// ConnectorDefinitionLoader returns the durable registry visible to one
// authenticated tenant and optional organization. The caller supplies scope
// derived from its principal, never directly from an RPC request.
type ConnectorDefinitionLoader interface {
	LoadRegistry(context.Context, uuid.UUID, string) (*connectivity.Registry, error)
}

// IntegrationService implements transport.IntegrationHandler over the
// connectivity definition registry, tenant-bound connections and observation
// store. Every RPC is read-only; TestConnectorConnection invokes only the
// connector's SchemaVersion and Snapshot methods.
type IntegrationService struct {
	definitions  ConnectorDefinitionLoader
	tenantID     func(string) uuid.UUID
	connections  []IntegrationConnection
	observations observe.ObservationStore
	now          func() time.Time
}

var _ transport.IntegrationHandler = (*IntegrationService)(nil)

// NewIntegrationService validates the application adapter wiring.
func NewIntegrationService(opts IntegrationOptions) (*IntegrationService, error) {
	if opts.Definitions == nil || opts.TenantID == nil {
		return nil, errors.New("application: scoped integration definition loader and tenant id resolver are required")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	connections := append([]IntegrationConnection(nil), opts.Connections...)
	seenConnections := make(map[string]struct{}, len(connections))
	for i, item := range connections {
		if item.Connection == nil {
			return nil, fmt.Errorf("application: integration connection %d is nil", i)
		}
		if item.Connector != nil && item.Connector.Descriptor().ConnectionID != "" && item.Connector.Descriptor().ConnectionID != item.Connection.ID() {
			return nil, fmt.Errorf("application: connector %q is bound to connection %q, want %q", item.Connector.Descriptor().ConnectorID, item.Connector.Descriptor().ConnectionID, item.Connection.ID())
		}
		if _, exists := seenConnections[item.Connection.ID()]; exists {
			return nil, fmt.Errorf("application: integration connection %q is registered more than once", item.Connection.ID())
		}
		seenConnections[item.Connection.ID()] = struct{}{}
	}
	return &IntegrationService{definitions: opts.Definitions, tenantID: opts.TenantID, connections: connections, observations: opts.Observations, now: now}, nil
}

func (s *IntegrationService) ListConnectorDefinitions(ctx context.Context, req *integrationv1.ListConnectorDefinitionsRequest) (*integrationv1.ListConnectorDefinitionsResponse, error) {
	principal, err := integrationCaller(ctx, req.GetScope())
	if err != nil {
		return nil, err
	}
	definitions, scopedErr := s.definitionRegistry(ctx, principal)
	if scopedErr != nil {
		return nil, scopedErr
	}
	items := make([]integrationv1.ConnectorDefinition, 0)
	for _, id := range definitions.ConnectorIDs() {
		for _, version := range definitions.Versions(id) {
			pub, resolveErr := definitions.Resolve(id, version)
			if resolveErr != nil {
				return nil, integrationUnavailable(resolveErr)
			}
			items = append(items, *definitionToProto(pub))
		}
	}
	start, size, err := integrationPage(req.GetPage(), len(items))
	if err != nil {
		return nil, err
	}
	end := min(start+size, len(items))
	result := &integrationv1.ListConnectorDefinitionsResponse{Page: &commonv1.PageResponse{}}
	for i := start; i < end; i++ {
		result.ConnectorDefinitions = append(result.ConnectorDefinitions, &items[i])
	}
	if end < len(items) {
		result.Page.NextCursor = integrationCursor(end)
	}
	return result, nil
}

func (s *IntegrationService) GetConnectorDefinition(ctx context.Context, req *integrationv1.GetConnectorDefinitionRequest) (*integrationv1.GetConnectorDefinitionResponse, error) {
	principal, err := integrationCaller(ctx, req.GetScope())
	if err != nil {
		return nil, err
	}
	definitions, scopedErr := s.definitionRegistry(ctx, principal)
	if scopedErr != nil {
		return nil, scopedErr
	}
	id := strings.TrimSpace(req.GetConnectorId())
	version, parseErr := connectivity.ParseVersion(req.GetConnectorVersion())
	if id == "" || parseErr != nil {
		return nil, integrationInvalid("connector_id or connector_version", errors.New("connector definition reference is malformed"))
	}
	pub, resolveErr := definitions.Resolve(id, version)
	if resolveErr != nil {
		return nil, integrationNotFound(resolveErr)
	}
	return &integrationv1.GetConnectorDefinitionResponse{ConnectorDefinition: definitionToProto(pub)}, nil
}

func (s *IntegrationService) ListConnectorConnections(ctx context.Context, req *integrationv1.ListConnectorConnectionsRequest) (*integrationv1.ListConnectorConnectionsResponse, error) {
	principal, err := integrationCaller(ctx, req.GetScope())
	if err != nil {
		return nil, err
	}
	items := make([]*integrationv1.ConnectorConnection, 0, len(s.connections))
	for _, item := range s.connections {
		if !connectionVisibleToPrincipal(item, principal) {
			continue
		}
		items = append(items, connectionToProto(item))
	}
	start, size, err := integrationPage(req.GetPage(), len(items))
	if err != nil {
		return nil, err
	}
	end := min(start+size, len(items))
	result := &integrationv1.ListConnectorConnectionsResponse{Page: &commonv1.PageResponse{}}
	result.Connections = append(result.Connections, items[start:end]...)
	if end < len(items) {
		result.Page.NextCursor = integrationCursor(end)
	}
	return result, nil
}

func (s *IntegrationService) GetConnectorConnection(ctx context.Context, req *integrationv1.GetConnectorConnectionRequest) (*integrationv1.GetConnectorConnectionResponse, error) {
	principal, err := integrationCaller(ctx, req.GetScope())
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.GetConnectionId())
	if id == "" {
		return nil, integrationInvalid("connection_id", errors.New("connection id is required"))
	}
	for _, item := range s.connections {
		if item.Connection.ID() == id && connectionVisibleToPrincipal(item, principal) {
			return &integrationv1.GetConnectorConnectionResponse{Connection: connectionToProto(item)}, nil
		}
	}
	return nil, integrationNotFound(errors.New("connection not found"))
}

func (s *IntegrationService) TestConnectorConnection(ctx context.Context, req *integrationv1.TestConnectorConnectionRequest) (*integrationv1.TestConnectorConnectionResponse, error) {
	principal, err := integrationCaller(ctx, req.GetScope())
	if err != nil {
		return nil, err
	}
	item, err := s.connectionForPrincipal(req.GetConnectionId(), principal)
	if err != nil {
		return nil, err
	}
	at := s.now().UTC()
	diagnostic := &integrationv1.ConnectionDiagnostic{ConnectionId: item.Connection.ID(), TestedAt: timestamppb.New(at)}
	objects := make([]connectivity.ObjectKind, 0, len(req.GetObjects()))
	for _, object := range req.GetObjects() {
		objects = append(objects, objectKindFromProto(object))
	}
	if len(objects) == 0 {
		definitions, scopedErr := s.definitionRegistry(ctx, principal)
		if scopedErr != nil {
			return nil, scopedErr
		}
		pub, resolveErr := definitions.Resolve(item.Connection.ConnectorID(), item.Connection.ConnectorVersion())
		if resolveErr != nil {
			return nil, integrationUnavailable(resolveErr)
		}
		objects = append(objects, pub.Definition.Objects...)
	}
	for _, object := range objects {
		if !object.Valid() {
			return nil, integrationInvalid("objects", errors.New("object kind is invalid"))
		}
	}
	if item.Connector == nil {
		for _, name := range []string{"authentication", "reachability", "scopes"} {
			diagnostic.Checks = append(diagnostic.Checks, &integrationv1.DiagnosticCheck{Name: name, Result: integrationv1.DiagnosticResult_DIAGNOSTIC_RESULT_SKIPPED, Detail: "connector diagnostics are unavailable"})
		}
		diagnostic.Impact = []string{"connector implementation is not composed"}
		return &integrationv1.TestConnectorConnectionResponse{Diagnostic: diagnostic}, nil
	}
	probeObject := connectivity.ObjectWorker
	if len(objects) > 0 {
		probeObject = objects[0]
	}
	probe := &integrationConnectorProbe{connector: item.Connector, connection: item.Connection, object: probeObject}
	report, runErr := diagnostics.Diagnose(ctx, probe, diagnostics.Request{Connection: item.Connection, RequiredScopes: item.Connection.Scopes(), Capabilities: capabilityImpacts(item.Connection, objects)})
	if runErr != nil {
		return nil, integrationUnavailable(runErr)
	}
	for _, finding := range report.Findings {
		check := &integrationv1.DiagnosticCheck{Name: string(finding.Check), Result: diagnosticResult(finding.Status), Detail: finding.Detail}
		if finding.Capability.Valid() {
			check.Name = "capability:" + finding.Capability.String()
		}
		diagnostic.Checks = append(diagnostic.Checks, check)
	}
	if !report.Healthy() {
		diagnostic.Impact = []string{"connector capability or permission checks require attention"}
	}
	return &integrationv1.TestConnectorConnectionResponse{Diagnostic: diagnostic}, nil
}

func (s *IntegrationService) definitionRegistry(ctx context.Context, principal *trust.Principal) (*connectivity.Registry, *envelope.Error) {
	if principal == nil {
		return nil, envelope.New(envelope.CodeUnauthenticated, "authentication.no_principal", "the request carries no valid authentication")
	}
	tenantID := s.tenantID(principal.Tenant().String())
	if tenantID == uuid.Nil {
		return nil, integrationUnavailable(errors.New("tenant id resolver returned an empty identity"))
	}
	organizationScopeID := strings.TrimSpace(principal.OrganizationScopeID())
	registry, err := s.definitions.LoadRegistry(ctx, tenantID, organizationScopeID)
	if err != nil {
		return nil, integrationUnavailable(err)
	}
	if registry == nil {
		return nil, integrationUnavailable(errors.New("definition loader returned no registry"))
	}
	return registry, nil
}

func (s *IntegrationService) ListExternalObservations(ctx context.Context, req *integrationv1.ListExternalObservationsRequest) (*integrationv1.ListExternalObservationsResponse, error) {
	principal, err := integrationCaller(ctx, req.GetScope())
	if err != nil {
		return nil, err
	}
	if s.observations == nil {
		return nil, integrationUnavailable(errors.New("observation store is not composed"))
	}
	if req.GetConnectionId() != "" {
		if _, findErr := s.connectionForPrincipal(req.GetConnectionId(), principal); findErr != nil {
			return nil, findErr
		}
	}
	query := observe.Query{TenantID: principal.Tenant().String(), ConnectionID: req.GetConnectionId(), Limit: 0}
	if req.Object != nil {
		object := objectKindFromProto(req.GetObject())
		if !object.Valid() {
			return nil, integrationInvalid("object", errors.New("object kind is invalid"))
		}
		query.Object = object
	}
	items, listErr := s.observations.List(ctx, query)
	if listErr != nil {
		return nil, integrationUnavailable(listErr)
	}
	visible := items[:0]
	for _, item := range items {
		if _, findErr := s.connectionForPrincipal(item.ConnectionID, principal); findErr == nil {
			visible = append(visible, item)
		} else if findErr.Code() != envelope.CodeNotFound {
			return nil, findErr
		}
	}
	items = visible
	start, size, pageErr := integrationPage(req.GetPage(), len(items))
	if pageErr != nil {
		return nil, pageErr
	}
	end := min(start+size, len(items))
	result := &integrationv1.ListExternalObservationsResponse{Page: &commonv1.PageResponse{}}
	for _, item := range items[start:end] {
		result.Observations = append(result.Observations, observationToProto(item))
	}
	if end < len(items) {
		result.Page.NextCursor = integrationCursor(end)
	}
	return result, nil
}

func (s *IntegrationService) GetExternalObservation(ctx context.Context, req *integrationv1.GetExternalObservationRequest) (*integrationv1.GetExternalObservationResponse, error) {
	principal, err := integrationCaller(ctx, req.GetScope())
	if err != nil {
		return nil, err
	}
	if s.observations == nil {
		return nil, integrationUnavailable(errors.New("observation store is not composed"))
	}
	id, parseErr := uuid.Parse(strings.TrimSpace(req.GetObservationId()))
	if parseErr != nil {
		return nil, integrationInvalid("observation_id", parseErr)
	}
	item, getErr := s.observations.Get(ctx, principal.Tenant().String(), id)
	if getErr != nil {
		if errors.Is(getErr, observe.ErrNotFound) {
			return nil, integrationNotFound(getErr)
		}
		return nil, integrationUnavailable(getErr)
	}
	if _, visibleErr := s.connectionForPrincipal(item.ConnectionID, principal); visibleErr != nil {
		return nil, visibleErr
	}
	return &integrationv1.GetExternalObservationResponse{Observation: observationToProto(item)}, nil
}

func integrationCaller(ctx context.Context, scope *commonv1.ScopeContext) (*trust.Principal, *envelope.Error) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return nil, envelope.New(envelope.CodeUnauthenticated, "authentication.no_principal", "the request carries no valid authentication")
	}
	tenant := principal.Tenant().String()
	if scope != nil && scope.GetTenantId() != "" && scope.GetTenantId() != tenant {
		return nil, envelope.New(envelope.CodePermissionDenied, "authorization.tenant_scope", "the action is not permitted for this principal").WithViolation("scope.tenant_id", "scope is outside the authenticated tenant", "authorization.tenant_scope")
	}
	if scope != nil && scope.GetOrganizationScopeId() != "" && principal.OrganizationScopeID() != "" && scope.GetOrganizationScopeId() != principal.OrganizationScopeID() {
		return nil, envelope.New(envelope.CodePermissionDenied, "authorization.organization_scope", "the action is not permitted for this principal").WithViolation("scope.organization_scope_id", "scope is outside the authenticated organization", "authorization.organization_scope")
	}
	return principal, nil
}

func (s *IntegrationService) connectionForPrincipal(id string, principal *trust.Principal) (IntegrationConnection, *envelope.Error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return IntegrationConnection{}, integrationInvalid("connection_id", errors.New("connection id is required"))
	}
	for _, item := range s.connections {
		if item.Connection.ID() == id && connectionVisibleToPrincipal(item, principal) {
			return item, nil
		}
	}
	return IntegrationConnection{}, integrationNotFound(errors.New("connection not found"))
}

func connectionVisibleToPrincipal(item IntegrationConnection, principal *trust.Principal) bool {
	if principal == nil || item.Connection == nil || item.Connection.TenantID() != principal.Tenant().String() {
		return false
	}
	org := principal.OrganizationScopeID()
	return org == "" || item.OrgID == org
}

type integrationConnectorProbe struct {
	connector  connectivity.Connector
	connection *connectivity.ConnectorConnection
	object     connectivity.ObjectKind
}

func (p *integrationConnectorProbe) Authenticate(ctx context.Context) error {
	_, err := p.connector.SchemaVersion(ctx, p.object)
	return err
}
func (p *integrationConnectorProbe) Reachable(ctx context.Context) error {
	_, err := p.connector.Snapshot(ctx, p.object)
	return err
}
func (p *integrationConnectorProbe) GrantedScopes(context.Context) ([]string, error) {
	return p.connection.Scopes(), nil
}
func (p *integrationConnectorProbe) CheckCapability(ctx context.Context, cap connectivity.Capability) error {
	if !p.connection.Supports(cap) {
		return connectivity.Fail("integration diagnostic", connectivity.ErrPermission, "capability is not granted")
	}
	_, err := p.connector.SchemaVersion(ctx, cap.Object)
	return err
}

func capabilityImpacts(connection *connectivity.ConnectorConnection, objects []connectivity.ObjectKind) []diagnostics.CapabilityImpact {
	if len(objects) == 0 {
		objects = connectivity.ObjectKinds()
	}
	result := make([]diagnostics.CapabilityImpact, 0, len(objects))
	for _, object := range objects {
		result = append(result, diagnostics.CapabilityImpact{Capability: connectivity.Capability{Object: object, Operation: connectivity.OperationRead}})
	}
	return result
}

func diagnosticResult(status diagnostics.Status) integrationv1.DiagnosticResult {
	switch status {
	case diagnostics.Pass:
		return integrationv1.DiagnosticResult_DIAGNOSTIC_RESULT_PASS
	case diagnostics.Fail:
		return integrationv1.DiagnosticResult_DIAGNOSTIC_RESULT_FAIL
	default:
		return integrationv1.DiagnosticResult_DIAGNOSTIC_RESULT_SKIPPED
	}
}

func integrationPage(page *commonv1.PageRequest, count int) (int, int, *envelope.Error) {
	size := integrationDefaultPageSize
	cursor := ""
	if page != nil {
		size = page.GetPageSize()
		cursor = page.GetCursor()
	}
	if size == 0 {
		size = integrationDefaultPageSize
	}
	if size < 0 || size > integrationMaxPageSize {
		return 0, 0, envelope.New(envelope.CodeInvalidArgument, "integration.page_size", "the requested page size is invalid").WithViolation("page.page_size", "page size exceeds the bounded integration list limit", "integration.page_size")
	}
	offset, err := parseIntegrationCursor(cursor)
	if err != nil {
		return 0, 0, envelope.New(envelope.CodeInvalidArgument, "integration.cursor", "the page cursor is invalid").WithViolation("page.cursor", "cursor is not a valid integration cursor", "integration.cursor").WithDiagnostic(err)
	}
	if offset > count {
		return 0, 0, envelope.New(envelope.CodeInvalidArgument, "integration.cursor", "the page cursor is invalid").WithViolation("page.cursor", "cursor is outside the result set", "integration.cursor")
	}
	return offset, int(size), nil
}

func integrationCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(integrationCursorPrefix + strconv.Itoa(offset)))
}

func parseIntegrationCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !strings.HasPrefix(string(raw), integrationCursorPrefix) {
		return 0, errors.New("invalid integration cursor")
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(string(raw), integrationCursorPrefix))
	if err != nil || offset < 0 {
		return 0, errors.New("invalid integration cursor")
	}
	return offset, nil
}

func integrationInvalid(field string, err error) *envelope.Error {
	return envelope.New(envelope.CodeInvalidArgument, "integration.request_rejected", "the request is malformed or structurally invalid").WithViolation(field, "the field is malformed", "integration.request_rejected").WithDiagnostic(err)
}
func integrationNotFound(err error) *envelope.Error {
	return envelope.New(envelope.CodeNotFound, "integration.not_found", "the resource does not exist or is not visible").WithDiagnostic(err)
}
func integrationUnavailable(err error) *envelope.Error {
	return envelope.New(envelope.CodeUnavailable, "integration.unavailable", "the integration service could not be served").WithDiagnostic(err)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func versionToProto(v connectivity.Version) string { return v.String() }

func definitionToProto(pub connectivity.Publication) *integrationv1.ConnectorDefinition {
	d := pub.Definition
	out := &integrationv1.ConnectorDefinition{
		ConnectorId: d.ConnectorID, Vendor: d.Vendor, Product: d.Product,
		ConnectorVersion: versionToProto(d.Version), Maturity: maturityToProto(d.Maturity),
		Objects:   append([]integrationv1.ObjectKind(nil), objectKindsToProto(d.Objects)...),
		AuthModes: authModesToProto(d.AuthModes), ReadModes: readModesToProto(d.ReadModes),
		WriteModes: append([]string(nil), d.WriteModes...), EventModes: append([]string(nil), d.EventModes...),
		Capabilities: capabilitiesToProto(d.Capabilities), SchemaRefs: schemaRefsToProto(d.SchemaRefs),
		Bounds:         &integrationv1.Bounds{MaxPageSize: int32(d.Bounds.MaxPageSize), MaxPagesPerRun: int32(d.Bounds.MaxPagesPerRun), MaxRecordsPerRun: int32(d.Bounds.MaxRecordsPerRun), MaxRecordBytes: int32(d.Bounds.MaxRecordBytes), MinRequestInterval: durationpb.New(d.Bounds.MinRequestInterval)},
		Pagination:     &integrationv1.PaginationContract{Style: d.Pagination.Style, SortKeyField: d.Pagination.SortKeyField, TieBreakField: d.Pagination.TieBreakField, StableUnderSnapshot: d.Pagination.StableUnderSnapshot, TokenTtl: durationpb.New(d.Pagination.TokenTTL)},
		Rate:           &integrationv1.RateContract{RequestsPerMinute: int32(d.Rate.RequestsPerMinute), ConcurrentReads: int32(d.Rate.ConcurrentReads), BurstRequests: int32(d.Rate.BurstRequests)},
		Idempotency:    &integrationv1.IdempotencyContract{KeyField: d.Idempotency.KeyField, RetentionWindow: durationpb.New(d.Idempotency.RetentionWindow), ReadsAreIdempotent: d.Idempotency.ReadsAreIdempotent},
		Observation:    &integrationv1.ObservationContract{WatermarkField: d.Observation.WatermarkField, FreshnessBudget: durationpb.New(d.Observation.FreshnessBudget), SupportsCompleteness: d.Observation.SupportsCompleteness},
		Reconciliation: &integrationv1.ReconciliationContract{KeyFields: append([]string(nil), d.Reconciliation.KeyFields...), ComparableFields: append([]string(nil), d.Reconciliation.ComparableFields...), SupportsPointRead: d.Reconciliation.SupportsPointRead},
		Health:         &integrationv1.HealthContract{ProbeObject: objectKindToProto(d.Health.ProbeObject), Interval: durationpb.New(d.Health.Interval), DegradedAfterFailures: int32(d.Health.DegradedAfterFailures)},
	}
	return out
}

func connectionToProto(item IntegrationConnection) *integrationv1.ConnectorConnection {
	c := item.Connection
	out := &integrationv1.ConnectorConnection{ConnectionId: c.ID(), TenantId: c.TenantID(), ConnectorId: c.ConnectorID(), ConnectorVersion: c.ConnectorVersion().String(), CredentialRef: c.CredentialRef().String(), Scopes: c.Scopes(), State: lifecycleToProto(c.State()), StateVersion: c.StateVersion(), Capabilities: capabilitiesToProto(c.Capabilities()), Bounds: boundsToProto(c.Bounds()), EndpointPolicy: endpointPolicyToProto(c.EndpointPolicy())}
	out.OrgId, out.SystemId, out.Residency = item.OrgID, item.SystemID, item.Residency
	out.Environment, out.AuthMode = environmentToProto(item.Environment), authModeToProto(item.AuthMode)
	for _, transition := range c.History() {
		out.History = append(out.History, &integrationv1.Transition{Sequence: transition.Sequence, From: lifecycleToProto(transition.From), To: lifecycleToProto(transition.To), Evidence: &integrationv1.TransitionEvidence{Reason: transition.Evidence.Reason, ActorRef: transition.Evidence.ActorRef, EvidenceRef: transition.Evidence.EvidenceRef, OccurredAt: timestamppb.New(transition.Evidence.OccurredAt)}})
	}
	if !item.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(item.CreatedAt.UTC())
	}
	return out
}

func observationToProto(o observe.Observation) *integrationv1.ExternalObservation {
	out := &integrationv1.ExternalObservation{ObservationId: o.ObservationID.String(), TenantId: o.TenantID, ConnectionId: o.ConnectionID, ConnectorId: o.ConnectorID, ConnectorVersion: o.ConnectorVersion, SourceRef: o.SourceRef, AuthorityRef: o.AuthorityRef, Object: objectKindToProto(o.Object), SchemaVersion: o.SchemaVersion, SnapshotId: o.SnapshotID, PageSequence: o.PageSequence, StartCursor: o.StartCursor, NextCursor: o.NextCursor, RecordCount: int32(o.RecordCount), Complete: o.Complete, Freshness: freshnessToProto(o.Freshness), Classification: integrationv1.ObservationClassification_OBSERVATION_CLASSIFICATION_EXTERNAL_OBSERVATION, ContentDigest: o.ContentDigest}
	if !o.RetrievedAt.IsZero() {
		out.RetrievedAt = timestamppb.New(o.RetrievedAt.UTC())
	}
	if !o.Watermark.IsZero() {
		out.Watermark = timestamppb.New(o.Watermark.UTC())
	}
	if o.RawArtifactRef != nil {
		ref := *o.RawArtifactRef
		out.RawArtifactRef = &ref
	}
	return out
}

func objectKindToProto(v connectivity.ObjectKind) integrationv1.ObjectKind {
	return integrationv1.ObjectKind(integrationv1.ObjectKind_value["OBJECT_KIND_"+string(v)])
}
func objectKindFromProto(v integrationv1.ObjectKind) connectivity.ObjectKind {
	switch v {
	case integrationv1.ObjectKind_OBJECT_KIND_WORKER:
		return connectivity.ObjectWorker
	case integrationv1.ObjectKind_OBJECT_KIND_POSITION:
		return connectivity.ObjectPosition
	case integrationv1.ObjectKind_OBJECT_KIND_COMPENSATION:
		return connectivity.ObjectCompensation
	default:
		return ""
	}
}
func objectKindsToProto(in []connectivity.ObjectKind) []integrationv1.ObjectKind {
	out := make([]integrationv1.ObjectKind, len(in))
	for i, v := range in {
		out[i] = objectKindToProto(v)
	}
	return out
}
func authModeToProto(v connectivity.AuthMode) integrationv1.AuthMode {
	return integrationv1.AuthMode(integrationv1.AuthMode_value[string(v)])
}
func authModesToProto(in []connectivity.AuthMode) []integrationv1.AuthMode {
	out := make([]integrationv1.AuthMode, len(in))
	for i, v := range in {
		out[i] = authModeToProto(v)
	}
	return out
}
func readModeToProto(v connectivity.ReadMode) integrationv1.ReadMode {
	return integrationv1.ReadMode(integrationv1.ReadMode_value[string(v)])
}
func readModesToProto(in []connectivity.ReadMode) []integrationv1.ReadMode {
	out := make([]integrationv1.ReadMode, len(in))
	for i, v := range in {
		out[i] = readModeToProto(v)
	}
	return out
}
func maturityToProto(v connectivity.Maturity) integrationv1.Maturity {
	return integrationv1.Maturity(integrationv1.Maturity_value[string(v)])
}
func lifecycleToProto(v connectivity.LifecycleState) integrationv1.LifecycleState {
	return integrationv1.LifecycleState(integrationv1.LifecycleState_value[string(v)])
}
func environmentToProto(v connectivity.Environment) integrationv1.Environment {
	return integrationv1.Environment(integrationv1.Environment_value[string(v)])
}
func freshnessToProto(v observe.Freshness) integrationv1.Freshness {
	return integrationv1.Freshness(integrationv1.Freshness_value[string(v)])
}
func capabilitiesToProto(in []connectivity.Capability) []*integrationv1.Capability {
	out := make([]*integrationv1.Capability, len(in))
	for i, v := range in {
		out[i] = &integrationv1.Capability{Object: objectKindToProto(v.Object), Operation: integrationv1.ConnectorOperation(integrationv1.ConnectorOperation_value[string(v.Operation)])}
	}
	return out
}
func schemaRefsToProto(in []connectivity.SchemaRef) []*integrationv1.SchemaRef {
	out := make([]*integrationv1.SchemaRef, len(in))
	for i, v := range in {
		out[i] = &integrationv1.SchemaRef{Object: objectKindToProto(v.Object), SchemaId: v.SchemaID, SchemaVersion: v.SchemaVer, Descriptor_: v.Descriptor}
	}
	return out
}
func boundsToProto(v connectivity.Bounds) *integrationv1.Bounds {
	return &integrationv1.Bounds{MaxPageSize: int32(v.MaxPageSize), MaxPagesPerRun: int32(v.MaxPagesPerRun), MaxRecordsPerRun: int32(v.MaxRecordsPerRun), MaxRecordBytes: int32(v.MaxRecordBytes), MinRequestInterval: durationpb.New(v.MinRequestInterval)}
}
func endpointPolicyToProto(v connectivity.EndpointPolicy) *integrationv1.EndpointPolicy {
	return &integrationv1.EndpointPolicy{AllowedHosts: append([]string(nil), v.AllowedHosts...), RequireTls: v.RequireTLS, EgressProfile: v.EgressProfile}
}
