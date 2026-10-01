// Package agentegress applies the common data boundary for agent connections
// and model providers. It is deliberately an effect-free policy layer: an
// adapter receives the returned minimized payload only after this package has
// accepted the destination, region, classification, retention and DLP
// decision.
package agentegress

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// TargetKind distinguishes the two egress consumers without giving either a
// separate policy implementation.
type TargetKind string

const (
	TargetConnection TargetKind = "CONNECTION"
	TargetModel      TargetKind = "MODEL"
)

// RetentionMode is the provider's declared retention behavior.
type RetentionMode string

const (
	RetentionNone      RetentionMode = "NONE"
	RetentionEphemeral RetentionMode = "EPHEMERAL"
	RetentionBounded   RetentionMode = "BOUNDED"
)

// RetentionPolicy is a closed-world retention declaration. NONE must have a
// zero age; the other modes must state the maximum age explicitly.
type RetentionPolicy struct {
	Mode   RetentionMode
	MaxAge time.Duration
}

// Profile is the immutable policy declaration for one administrator-approved
// connection or model provider profile.
type Profile struct {
	ID             string
	Kind           TargetKind
	AllowedRegions []string
	AllowedClasses []trustdlp.DataClass
	Retention      RetentionPolicy
}

// TaskPolicy supplies the signed-in user's task ceilings. The evaluator does
// not widen these ceilings from a target profile.
type TaskPolicy struct {
	AllowedRegions       []string
	AllowedResultClasses []trustdlp.DataClass
	MaxExternalRetention time.Duration
	ResultRetention      time.Duration
}

// Field is a classified, tainted value from a skill argument or result. The
// labels use the same strings emitted by agentsecurity's tool gateway; this
// package never lowers or removes them.
type Field struct {
	Name       string
	Value      any
	Class      trustdlp.DataClass
	Taint      []string
	Provenance []string
}

// OutboundRequest is the common request for a connection argument or model
// context. DeclaredFields is the skill's minimum-necessary field set.
type OutboundRequest struct {
	TaskID         string
	Tenant         string
	Principal      string
	Purpose        string
	Profile        Profile
	Region         string
	DeclaredFields []string
	Fields         []Field
	Task           TaskPolicy
	Now            time.Time
}

// InboundRequest classifies a typed connection/model result before it enters
// task memory or the task ledger.
type InboundRequest struct {
	TaskID    string
	Tenant    string
	Principal string
	Purpose   string
	Profile   Profile
	Region    string
	Result    agentsecurity.TypedResult
	Fields    []Field
	Task      TaskPolicy
	Now       time.Time
}

// Decision is the egress outcome. REFUSE is returned as a typed Refusal error
// as well as in the decision receipt.
type Decision string

const (
	DecisionAllow  Decision = "ALLOW"
	DecisionRefuse Decision = "REFUSE"
)

// RefusalCode identifies a stable fail-closed reason.
type RefusalCode string

const (
	RefusalInvalidRequest   RefusalCode = "AGENT_EGRESS_INVALID_REQUEST"
	RefusalProfile          RefusalCode = "AGENT_EGRESS_PROFILE_INVALID"
	RefusalRegion           RefusalCode = "AGENT_EGRESS_REGION_NOT_ALLOWED"
	RefusalClass            RefusalCode = "AGENT_EGRESS_DATA_CLASS_NOT_ALLOWED"
	RefusalRetention        RefusalCode = "AGENT_EGRESS_RETENTION_NOT_ALLOWED"
	RefusalMinimumNecessary RefusalCode = "AGENT_EGRESS_MINIMUM_NECESSARY"
	RefusalClassification   RefusalCode = "AGENT_EGRESS_CLASSIFICATION_REQUIRED"
	RefusalDLP              RefusalCode = "AGENT_EGRESS_DLP_REFUSED"
	RefusalResult           RefusalCode = "AGENT_EGRESS_RESULT_INVALID"
	RefusalLedger           RefusalCode = "AGENT_EGRESS_LEDGER_POLICY"
)

var ErrRefused = errors.New("agent egress refused")

// Refusal is safe to expose to callers and audit logs; it never includes a
// field value, prompt text, credential, or result payload.
type Refusal struct {
	Code   RefusalCode
	Field  string
	Detail string
}

func (e *Refusal) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Field == "" {
		return string(e.Code) + ": " + e.Detail
	}
	return string(e.Code) + ": field " + e.Field + ": " + e.Detail
}

func (e *Refusal) Unwrap() error { return ErrRefused }

// EgressReceipt is an audit-safe projection of one decision. DLPReceipt is
// the existing redaction-safe receipt and binds the serialized payload by
// digest; this wrapper adds task, profile and minimum-necessary evidence.
type EgressReceipt struct {
	TaskID      string
	Tenant      string
	ProfileID   string
	Kind        TargetKind
	Region      string
	Purpose     string
	Fields      []string
	Classes     []trustdlp.DataClass
	Decision    Decision
	Reason      string
	DLPDecision trustdlp.Decision
	DLPReceipt  trustdlp.EgressReceipt
	Digest      string
}

// Canonical is stable and contains no payload material.
func (r EgressReceipt) Canonical() string {
	fields := append([]string(nil), r.Fields...)
	classes := append([]trustdlp.DataClass(nil), r.Classes...)
	sort.Strings(fields)
	sort.Slice(classes, func(i, j int) bool { return classes[i] < classes[j] })
	return fmt.Sprintf("task=%s\x00tenant=%s\x00profile=%s\x00kind=%s\x00region=%s\x00purpose=%s\x00fields=%s\x00classes=%s\x00decision=%s\x00reason=%s\x00dlp=%s",
		r.TaskID, r.Tenant, r.ProfileID, r.Kind, r.Region, r.Purpose,
		strings.Join(fields, ","), strings.Join(classStrings(classes), ","),
		r.Decision, r.Reason, r.DLPReceipt.Digest)
}

// DigestReceipt returns the stable receipt digest.
func DigestReceipt(r EgressReceipt) string {
	sum := sha256.Sum256([]byte(r.Canonical()))
	return hex.EncodeToString(sum[:])
}

// OutboundDecision is the result handed to a connection/model adapter. The
// payload is present only when Allowed is true and contains only declared
// fields.
type OutboundDecision struct {
	Allowed  bool
	Payload  []byte
	Fields   []Field
	Classes  []trustdlp.DataClass
	Decision trustdlp.Decision
	Reason   string
	Receipt  EgressReceipt
}

// RetainedResult is the classified, time-bounded projection admitted to task
// memory. Callers must use ExpiresAt as the deletion fence.
type RetainedResult struct {
	Schema     string
	Value      any
	Fields     []Field
	Classes    []trustdlp.DataClass
	Taint      []string
	Provenance []string
	ExpiresAt  time.Time
	Receipt    EgressReceipt
}

// Evaluator is the single policy evaluator for model and connection egress.
type Evaluator struct {
	policy    *trustdlp.Policy
	inspector *trustdlp.Inspector
	receipts  *trustdlp.ReceiptLog
}

// NewEvaluator binds the existing destination DLP policy, scanner and
// append-only receipt stream. A missing scanner is never treated as allow.
func NewEvaluator(policy *trustdlp.Policy, inspector *trustdlp.Inspector, receipts *trustdlp.ReceiptLog) (*Evaluator, error) {
	if policy == nil || inspector == nil {
		return nil, fmt.Errorf("%w: policy and inspector are required", ErrRefused)
	}
	if receipts == nil {
		receipts = trustdlp.NewReceiptLog()
	}
	return &Evaluator{policy: policy, inspector: inspector, receipts: receipts}, nil
}

// EvaluateOutbound checks policy and returns a minimized payload before an
// adapter is allowed to make a network/provider call.
func (e *Evaluator) EvaluateOutbound(req OutboundRequest) (OutboundDecision, error) {
	if e == nil {
		return OutboundDecision{}, refuse(RefusalInvalidRequest, "evaluator", "evaluator is required")
	}
	if err := validateRequest(req.TaskID, req.Tenant, req.Principal, req.Purpose, req.Region, req.Now); err != nil {
		return OutboundDecision{}, err
	}
	if err := validateProfile(req.Profile); err != nil {
		return OutboundDecision{}, err
	}
	if err := validateTask(req.Task); err != nil {
		return OutboundDecision{}, err
	}
	if !contains(req.Profile.AllowedRegions, req.Region) || !contains(req.Task.AllowedRegions, req.Region) {
		return OutboundDecision{}, refuse(RefusalRegion, "region", "target region is outside the profile and task residency intersection")
	}
	if req.Profile.Retention.MaxAge > req.Task.MaxExternalRetention {
		return OutboundDecision{}, refuse(RefusalRetention, "retention.max_age", "target retention exceeds the task ceiling")
	}
	fields, classes, err := minimize(req.DeclaredFields, req.Fields)
	if err != nil {
		return OutboundDecision{}, err
	}
	if err := ensureAllowedClasses(classes, req.Profile.AllowedClasses, RefusalClass); err != nil {
		return OutboundDecision{}, err
	}
	payload, err := fieldsJSON(fields)
	if err != nil {
		return OutboundDecision{}, refuse(RefusalInvalidRequest, "fields", "fields are not serializable JSON")
	}
	inspection, err := e.inspector.Inspect(payload)
	if err != nil {
		return OutboundDecision{}, refuse(RefusalDLP, "inspection", "DLP inspection failed closed")
	}
	classes = appendInspectionClasses(classes, inspection)
	if err := ensureAllowedClasses(classes, req.Profile.AllowedClasses, RefusalClass); err != nil {
		return OutboundDecision{}, err
	}
	evaluation, err := e.policy.Evaluate(trustdlp.DecisionRequest{
		Destination: req.Profile.ID, Purpose: req.Purpose, Principal: req.Principal,
		DeclaredClasses: classes, Inspection: inspection,
	})
	if err != nil {
		return OutboundDecision{}, refuse(RefusalDLP, "destination", "DLP policy could not evaluate the destination")
	}
	receipt, receiptErr := e.receipt(req.TaskID, req.Tenant, req.Profile, req.Region, req.Purpose, fieldNames(fields), classes, evaluation.Decision, evaluation.Reason, payload, inspection)
	if receiptErr != nil {
		return OutboundDecision{}, receiptErr
	}
	decision := OutboundDecision{Allowed: evaluation.Decision == trustdlp.Allow, Fields: cloneFields(fields), Classes: append([]trustdlp.DataClass(nil), classes...), Decision: evaluation.Decision, Reason: evaluation.Reason, Receipt: receipt}
	if evaluation.Decision != trustdlp.Allow {
		decision.Allowed = false
		if evaluation.Decision == trustdlp.Redact || evaluation.Decision == trustdlp.ApprovalRequired {
			return decision, refuse(RefusalDLP, "decision", "DLP requires transformation or separate approval before egress")
		}
		return decision, refuse(RefusalDLP, "decision", "DLP refused the outbound payload")
	}
	decision.Payload = append([]byte(nil), payload...)
	return decision, nil
}

// AcceptInbound classifies a typed result and applies the task ledger
// retention ceiling before the result can be retained in agent memory.
func (e *Evaluator) AcceptInbound(req InboundRequest) (RetainedResult, error) {
	if e == nil {
		return RetainedResult{}, refuse(RefusalInvalidRequest, "evaluator", "evaluator is required")
	}
	if err := validateRequest(req.TaskID, req.Tenant, req.Principal, req.Purpose, req.Region, req.Now); err != nil {
		return RetainedResult{}, err
	}
	if err := validateProfile(req.Profile); err != nil {
		return RetainedResult{}, err
	}
	if err := validateTask(req.Task); err != nil {
		return RetainedResult{}, err
	}
	if !req.Result.Validated || strings.TrimSpace(req.Result.Schema) == "" || req.Result.Value == nil || len(req.Result.Taint) == 0 || len(req.Result.Provenance) == 0 {
		return RetainedResult{}, refuse(RefusalResult, "result", "result must be typed, validated, tainted and attributable")
	}
	if !contains(req.Profile.AllowedRegions, req.Region) || !contains(req.Task.AllowedRegions, req.Region) {
		return RetainedResult{}, refuse(RefusalRegion, "region", "result region is outside the profile and task residency intersection")
	}
	if req.Task.ResultRetention <= 0 {
		return RetainedResult{}, refuse(RefusalLedger, "result_retention", "task ledger retention is required")
	}
	fields, classes, err := classifyInbound(req.Fields)
	if err != nil {
		return RetainedResult{}, err
	}
	if err := ensureAllowedClasses(classes, req.Profile.AllowedClasses, RefusalClass); err != nil {
		return RetainedResult{}, err
	}
	if err := ensureAllowedClasses(classes, req.Task.AllowedResultClasses, RefusalLedger); err != nil {
		return RetainedResult{}, err
	}
	payload, err := fieldsJSON(fields)
	if err != nil {
		return RetainedResult{}, refuse(RefusalClassification, "fields", "classified result is not serializable JSON")
	}
	inspection, err := e.inspector.Inspect(payload)
	if err != nil {
		return RetainedResult{}, refuse(RefusalDLP, "inspection", "DLP inspection failed closed")
	}
	classes = appendInspectionClasses(classes, inspection)
	if err := ensureAllowedClasses(classes, req.Profile.AllowedClasses, RefusalClass); err != nil {
		return RetainedResult{}, err
	}
	if err := ensureAllowedClasses(classes, req.Task.AllowedResultClasses, RefusalLedger); err != nil {
		return RetainedResult{}, err
	}
	evaluation, err := e.policy.Evaluate(trustdlp.DecisionRequest{
		Destination: req.Profile.ID, Purpose: req.Purpose, Principal: req.Principal,
		DeclaredClasses: classes, Inspection: inspection,
	})
	if err != nil || evaluation.Decision != trustdlp.Allow {
		return RetainedResult{}, refuse(RefusalDLP, "result", "inbound result is not cleared for the profile")
	}
	receipt, err := e.receipt(req.TaskID, req.Tenant, req.Profile, req.Region, req.Purpose, fieldNames(fields), classes, evaluation.Decision, "inbound result classified and cleared", payload, inspection)
	if err != nil {
		return RetainedResult{}, err
	}
	value, err := cloneJSON(req.Result.Value)
	if err != nil {
		return RetainedResult{}, refuse(RefusalResult, "result", "result value is not safely serializable")
	}
	if err := validateResultFields(value, fields); err != nil {
		return RetainedResult{}, err
	}
	return RetainedResult{
		Schema: req.Result.Schema, Value: value, Fields: cloneFields(fields), Classes: append([]trustdlp.DataClass(nil), classes...),
		Taint: append([]string(nil), req.Result.Taint...), Provenance: append([]string(nil), req.Result.Provenance...),
		ExpiresAt: req.Now.Add(req.Task.ResultRetention), Receipt: receipt,
	}, nil
}

// Receipts returns the underlying redaction-safe DLP receipt stream.
func (e *Evaluator) Receipts() []trustdlp.EgressReceipt {
	if e == nil || e.receipts == nil {
		return nil
	}
	return e.receipts.Receipts()
}

// ResultLedger is a small retention-enforcing task-memory projection for
// callers that need a local implementation in tests or a bounded worker.
type ResultLedger struct {
	mu      sync.Mutex
	results map[string]RetainedResult
}

func NewResultLedger() *ResultLedger { return &ResultLedger{results: make(map[string]RetainedResult)} }

func (l *ResultLedger) Put(key string, result RetainedResult) error {
	if l == nil || strings.TrimSpace(key) == "" || result.ExpiresAt.IsZero() {
		return refuse(RefusalLedger, "ledger", "key and expiry are required")
	}
	value, err := cloneJSON(result.Value)
	if err != nil {
		return refuse(RefusalLedger, "ledger.result", "result value is not safely serializable")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.results[key] = cloneRetainedResult(result, value)
	return nil
}

// Get deletes expired material before returning, so an expired connector
// result cannot remain available in this bounded memory projection.
func (l *ResultLedger) Get(key string, now time.Time) (RetainedResult, bool) {
	if l == nil || strings.TrimSpace(key) == "" || now.IsZero() {
		return RetainedResult{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	result, ok := l.results[key]
	if !ok {
		return RetainedResult{}, false
	}
	if !now.Before(result.ExpiresAt) {
		delete(l.results, key)
		return RetainedResult{}, false
	}
	value, err := cloneJSON(result.Value)
	if err != nil {
		delete(l.results, key)
		return RetainedResult{}, false
	}
	return cloneRetainedResult(result, value), true
}

func validateRequest(taskID, tenant, principal, purpose, region string, now time.Time) error {
	for _, item := range []struct{ name, value string }{
		{"task_id", taskID}, {"tenant", tenant}, {"principal", principal}, {"purpose", purpose}, {"region", region},
	} {
		if strings.TrimSpace(item.value) == "" || strings.TrimSpace(item.value) != item.value {
			return refuse(RefusalInvalidRequest, item.name, "value is required and may not be padded")
		}
	}
	if now.IsZero() {
		return refuse(RefusalInvalidRequest, "now", "evaluation time is required")
	}
	return nil
}

func validateProfile(profile Profile) error {
	if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.ID) != profile.ID {
		return refuse(RefusalProfile, "profile.id", "profile id is required")
	}
	if profile.Kind != TargetConnection && profile.Kind != TargetModel {
		return refuse(RefusalProfile, "profile.kind", "profile kind is not supported")
	}
	if len(profile.AllowedRegions) == 0 || len(profile.AllowedClasses) == 0 {
		return refuse(RefusalProfile, "profile", "profile must declare regions and data classes")
	}
	if err := validateRegions(profile.AllowedRegions, "profile.allowed_regions", RefusalProfile); err != nil {
		return err
	}
	if err := validateRetention(profile.Retention, "profile.retention"); err != nil {
		return err
	}
	seen := make(map[trustdlp.DataClass]struct{}, len(profile.AllowedClasses))
	for _, class := range profile.AllowedClasses {
		if !class.Valid() {
			return refuse(RefusalProfile, "profile.allowed_classes", "profile contains an unknown data class")
		}
		if _, exists := seen[class]; exists {
			return refuse(RefusalProfile, "profile.allowed_classes", "profile contains duplicate data classes")
		}
		seen[class] = struct{}{}
	}
	return nil
}

func validateTask(task TaskPolicy) error {
	if len(task.AllowedRegions) == 0 || len(task.AllowedResultClasses) == 0 {
		return refuse(RefusalInvalidRequest, "task", "task must declare residency and result classes")
	}
	if err := validateRegions(task.AllowedRegions, "task.allowed_regions", RefusalInvalidRequest); err != nil {
		return err
	}
	if task.MaxExternalRetention < 0 || task.ResultRetention <= 0 {
		return refuse(RefusalInvalidRequest, "task.retention", "task retention bounds are invalid")
	}
	seen := make(map[trustdlp.DataClass]struct{}, len(task.AllowedResultClasses))
	for _, class := range task.AllowedResultClasses {
		if !class.Valid() {
			return refuse(RefusalInvalidRequest, "task.allowed_result_classes", "task contains an unknown data class")
		}
		if _, exists := seen[class]; exists {
			return refuse(RefusalInvalidRequest, "task.allowed_result_classes", "task contains duplicate data classes")
		}
		seen[class] = struct{}{}
	}
	return nil
}

func validateRegions(regions []string, field string, code RefusalCode) error {
	seen := make(map[string]struct{}, len(regions))
	for _, region := range regions {
		if strings.TrimSpace(region) == "" || strings.TrimSpace(region) != region {
			return refuse(code, field, "regions must be non-empty canonical values")
		}
		if _, exists := seen[region]; exists {
			return refuse(code, field, "regions must not contain duplicates")
		}
		seen[region] = struct{}{}
	}
	return nil
}

func validateRetention(policy RetentionPolicy, field string) error {
	switch policy.Mode {
	case RetentionNone:
		if policy.MaxAge != 0 {
			return refuse(RefusalProfile, field, "NONE retention must have zero age")
		}
	case RetentionEphemeral, RetentionBounded:
		if policy.MaxAge <= 0 {
			return refuse(RefusalProfile, field, "retention mode requires a positive maximum age")
		}
	default:
		return refuse(RefusalProfile, field, "retention mode is not declared")
	}
	return nil
}

func minimize(declared []string, fields []Field) ([]Field, []trustdlp.DataClass, error) {
	if len(declared) == 0 {
		return nil, nil, refuse(RefusalMinimumNecessary, "declared_fields", "skill must declare a non-empty minimum field set")
	}
	wanted := make(map[string]struct{}, len(declared))
	for _, name := range declared {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name {
			return nil, nil, refuse(RefusalMinimumNecessary, "declared_fields", "field names must be canonical")
		}
		if _, exists := wanted[name]; exists {
			return nil, nil, refuse(RefusalMinimumNecessary, "declared_fields", "field set contains duplicates")
		}
		wanted[name] = struct{}{}
	}
	byName := make(map[string]Field, len(fields))
	for _, field := range fields {
		if err := validateField(field); err != nil {
			return nil, nil, err
		}
		if _, exists := byName[field.Name]; exists {
			return nil, nil, refuse(RefusalClassification, field.Name, "field is declared more than once")
		}
		byName[field.Name] = field
	}
	selected := make([]Field, 0, len(declared))
	classes := make([]trustdlp.DataClass, 0, len(declared))
	for _, name := range declared {
		field, ok := byName[name]
		if !ok {
			return nil, nil, refuse(RefusalMinimumNecessary, name, "declared field is not present")
		}
		selected = append(selected, field)
		classes = appendClass(classes, field.Class)
	}
	return selected, classes, nil
}

func classifyInbound(fields []Field) ([]Field, []trustdlp.DataClass, error) {
	if len(fields) == 0 {
		return nil, nil, refuse(RefusalClassification, "fields", "every inbound result must carry field classifications")
	}
	selected := make([]Field, 0, len(fields))
	classes := make([]trustdlp.DataClass, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if err := validateField(field); err != nil {
			return nil, nil, err
		}
		if _, exists := seen[field.Name]; exists {
			return nil, nil, refuse(RefusalClassification, field.Name, "field is classified more than once")
		}
		seen[field.Name] = struct{}{}
		selected = append(selected, field)
		classes = appendClass(classes, field.Class)
	}
	return selected, classes, nil
}

func validateField(field Field) error {
	if strings.TrimSpace(field.Name) == "" || strings.TrimSpace(field.Name) != field.Name {
		return refuse(RefusalClassification, "field.name", "field name is required and may not be padded")
	}
	if !field.Class.Valid() {
		return refuse(RefusalClassification, field.Name, "field data class is not declared")
	}
	if len(field.Taint) == 0 || len(field.Provenance) == 0 {
		return refuse(RefusalClassification, field.Name, "taint and provenance labels are required")
	}
	for _, label := range append(append([]string(nil), field.Taint...), field.Provenance...) {
		if strings.TrimSpace(label) == "" || strings.TrimSpace(label) != label {
			return refuse(RefusalClassification, field.Name, "taint and provenance labels must be canonical")
		}
	}
	return nil
}

func ensureAllowedClasses(classes, allowed []trustdlp.DataClass, code RefusalCode) error {
	for _, class := range classes {
		if !containsClass(allowed, class) {
			return refuse(code, "data_class", "data class is outside the declared egress or ledger allowance")
		}
	}
	return nil
}

func appendInspectionClasses(classes []trustdlp.DataClass, inspection trustdlp.Inspection) []trustdlp.DataClass {
	for _, finding := range inspection.Findings {
		classes = appendClass(classes, finding.Class)
	}
	return classes
}

func fieldsJSON(fields []Field) ([]byte, error) {
	values := make(map[string]any, len(fields))
	for _, field := range fields {
		values[field.Name] = field.Value
	}
	return json.Marshal(values)
}

func (e *Evaluator) receipt(taskID, tenant string, profile Profile, region, purpose string, fields []string, classes []trustdlp.DataClass, decision trustdlp.Decision, reason string, payload []byte, inspection trustdlp.Inspection) (EgressReceipt, error) {
	dlpReceipt, err := e.receipts.Append(trustdlp.ReceiptInput{
		Destination: profile.ID, Purpose: purpose, Principal: tenant + ":" + taskID,
		Payload: payload, Inspection: inspection, Decision: decision,
	})
	if err != nil {
		return EgressReceipt{}, refuse(RefusalDLP, "receipt", "DLP receipt could not be appended")
	}
	receipt := EgressReceipt{TaskID: taskID, Tenant: tenant, ProfileID: profile.ID, Kind: profile.Kind, Region: region, Purpose: purpose, Fields: append([]string(nil), fields...), Classes: append([]trustdlp.DataClass(nil), classes...), Decision: func() Decision {
		if decision == trustdlp.Allow {
			return DecisionAllow
		}
		return DecisionRefuse
	}(), Reason: reason, DLPDecision: decision, DLPReceipt: dlpReceipt}
	receipt.Digest = DigestReceipt(receipt)
	return receipt, nil
}

func refuse(code RefusalCode, field, detail string) error {
	return &Refusal{Code: code, Field: field, Detail: detail}
}

func appendClass(classes []trustdlp.DataClass, class trustdlp.DataClass) []trustdlp.DataClass {
	if !containsClass(classes, class) {
		return append(classes, class)
	}
	return classes
}

func containsClass(classes []trustdlp.DataClass, want trustdlp.DataClass) bool {
	for _, class := range classes {
		if class == want {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func classStrings(classes []trustdlp.DataClass) []string {
	values := make([]string, len(classes))
	for i, class := range classes {
		values[i] = string(class)
	}
	return values
}

func fieldNames(fields []Field) []string {
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = field.Name
	}
	return names
}

func cloneFields(fields []Field) []Field {
	cloned := make([]Field, len(fields))
	for i, field := range fields {
		cloned[i] = field
		cloned[i].Taint = append([]string(nil), field.Taint...)
		cloned[i].Provenance = append([]string(nil), field.Provenance...)
	}
	return cloned
}

func cloneJSON(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var cloned any
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func validateResultFields(value any, fields []Field) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return refuse(RefusalClassification, "result", "result value is not safely serializable")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil || object == nil {
		if len(fields) != 1 {
			return refuse(RefusalClassification, "result", "non-object results require exactly one classified field")
		}
		return nil
	}
	declared := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		declared[field.Name] = struct{}{}
	}
	for name := range object {
		if _, ok := declared[name]; !ok {
			return refuse(RefusalClassification, name, "result contains an unclassified field")
		}
	}
	for name := range declared {
		if _, ok := object[name]; !ok {
			return refuse(RefusalClassification, name, "classified field is absent from the result")
		}
	}
	return nil
}

func cloneRetainedResult(result RetainedResult, value any) RetainedResult {
	result.Value = value
	result.Fields = cloneFields(result.Fields)
	result.Classes = append([]trustdlp.DataClass(nil), result.Classes...)
	result.Taint = append([]string(nil), result.Taint...)
	result.Provenance = append([]string(nil), result.Provenance...)
	result.Receipt.Fields = append([]string(nil), result.Receipt.Fields...)
	result.Receipt.Classes = append([]trustdlp.DataClass(nil), result.Receipt.Classes...)
	return result
}
