package evidence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	kernelevidence "github.com/monstercameron/human-capital-management-suite/internal/evidence"
	exportformat "github.com/monstercameron/human-capital-management-suite/internal/operations/export"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
)

// evidenceNamespace names every id this package mints deterministically, the
// same pattern internal/intent/app's artifactNamespace uses: the same
// (tenant, intent, purpose, idempotency key) tuple must always mint the same
// operation id, so a replay after a crash resolves to the one export that
// tuple already names rather than a fresh one.
var evidenceNamespace = uuid.NewSHA1(uuid.NameSpaceOID, []byte("hcmnext.evidence.v1/EP-EVID-001"))

func deterministicOperationID(tenant, intentID, purpose, idempotencyKey string) string {
	return uuid.NewSHA1(evidenceNamespace, []byte("export-operation|"+tenant+"|"+intentID+"|"+purpose+"|"+idempotencyKey)).String()
}

func deterministicArtifactID(operationID string) string {
	return uuid.NewSHA1(evidenceNamespace, []byte("export-artifact|"+operationID)).String()
}

// exportJob is the bounded, already-validated unit of work ExportIntentEvidence
// hands to its [Dispatcher]. It carries no context and no live dependency: a
// job is a value, so a fake dispatcher in a test can inspect, delay or
// replay it without reaching back into request-scoped state.
type exportJob struct {
	Tenant            string
	IntentID          string
	Purpose           string
	OrganizationScope string
	RequestorSubject  string
	IdempotencyKey    string
	OperationID       string
	ArtifactID        string
}

// ExportIntentEvidence starts one governed, idempotent, resumable export.
// The request path here does no lineage lookup, no receipt assembly, no
// rendering, no encryption and no signing: it creates exactly one PENDING
// operations.Record, hands the job to s.deps.Dispatcher, and returns. All of
// that bounded-but-nontrivial work happens in [server.runExport], which the
// dispatcher runs outside this call -- the request path can never become an
// unbounded synchronous archive build because it does not build the archive
// at all.
func (s *server) ExportIntentEvidence(ctx context.Context, req *evidencev1.ExportIntentEvidenceRequest) (*evidencev1.ExportIntentEvidenceResponse, error) {
	p, inv, ownedErr := trustedContext(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}

	idempotencyKey := strings.TrimSpace(req.GetIdempotencyKey())
	intentID := strings.TrimSpace(req.GetIntentId())
	purpose := strings.TrimSpace(req.GetPurpose())
	switch {
	case idempotencyKey == "":
		return nil, requireField("idempotency_key")
	case intentID == "":
		return nil, requireField("intent_id")
	case purpose == "":
		return nil, requireField("purpose")
	}
	if !p.AuthorizesPurpose(purpose) {
		return nil, purposeDenied(inv, p)
	}
	if _, ok := s.deps.Purposes.AllowedDimensions(purpose); !ok {
		return nil, purposeDenied(inv, p)
	}

	tenant := p.Tenant().String()
	operationID := deterministicOperationID(tenant, intentID, purpose, idempotencyKey)
	job := exportJob{
		Tenant: tenant, IntentID: intentID, Purpose: purpose,
		OrganizationScope: p.OrganizationScopeID(), RequestorSubject: p.Subject(),
		IdempotencyKey: idempotencyKey, OperationID: operationID,
		ArtifactID: deterministicArtifactID(operationID),
	}

	idemReq := endpoint.Request{
		Scope:      endpoint.Scope{Principal: p.Subject(), Tenant: tenant, Capability: capabilityExportIntentEvidence},
		MessageKey: idempotencyKey,
		Payload:    []byte(fmt.Sprintf("export|%s|%s", intentID, purpose)),
	}
	if _, doErr := s.deps.Idempotency.Do(ctx, idemReq, func(ctx context.Context) (endpoint.Outcome, error) {
		if _, err := s.deps.Operations.Create(ctx, tenant, operationID, p.Subject(), requestTypeExport); err != nil && !errors.Is(err, errOperationExists) {
			return endpoint.Outcome{}, err
		}
		s.deps.Dispatcher.Dispatch(ctx, job, s.runExport)
		return endpoint.Outcome{Status: "PENDING", ResultDigest: operationID}, nil
	}); doErr != nil {
		return nil, idempotencyError(doErr)
	}

	record, err := s.deps.Operations.Get(ctx, tenant, operationID)
	if err != nil {
		return nil, unavailable(inv, err)
	}
	return &evidencev1.ExportIntentEvidenceResponse{Operation: operations.Project(record, p)}, nil
}

// runExport performs the actual, bounded package assembly: read the frozen
// lineage (never current domain state), assemble and redact the receipt,
// render the two artifacts, seal and sign the package, persist exactly one
// artifact, and complete exactly one operation record. Any failure at any
// step fails the operation closed; it never leaves a PENDING/RUNNING record
// with no eventual terminal state, and it never falls back to a partial
// success.
func (s *server) runExport(ctx context.Context, job exportJob) {
	_ = s.deps.Operations.MarkRunning(ctx, job.Tenant, job.OperationID)

	snap, err := s.deps.Lineage.Snapshot(ctx, job.Tenant, job.IntentID)
	if err != nil {
		s.failOperation(ctx, job, err)
		return
	}
	receipt, err := assembleReceipt(snap)
	if err != nil {
		s.failOperation(ctx, job, err)
		return
	}
	if err := requireLineageHops(receipt); err != nil {
		s.failOperation(ctx, job, err)
		return
	}
	allowed, ok := s.deps.Purposes.AllowedDimensions(job.Purpose)
	if !ok {
		s.failOperation(ctx, job, ErrPurposeNotConfigured)
		return
	}
	redacted, redactedNames, err := redactReceipt(receipt, allowed, job.Purpose)
	if err != nil {
		s.failOperation(ctx, job, err)
		return
	}

	now := s.deps.Clock()
	expiresAt := now.Add(72 * time.Hour)
	human, machine, err := renderDimensionArtifacts(redacted, job, now, expiresAt)
	if err != nil {
		s.failOperation(ctx, job, err)
		return
	}

	content := Content{
		Manifest: Manifest{
			ContractVersion:      ContractVersion,
			Tenant:               job.Tenant,
			OrganizationScope:    job.OrganizationScope,
			IntentRef:            job.IntentID,
			RequestorSubject:     job.RequestorSubject,
			IdempotencyKey:       job.IdempotencyKey,
			Purpose:              job.Purpose,
			Format:               "hcmnext.evidence-export-artifacts/v1 (csv-inert-formula-cells/v1 + exact-typed-json/v1)",
			AllowedFields:        append([]string(nil), allowed...),
			RedactedFields:       redactedNames,
			RedactionReason:      redactionReason(job.Purpose),
			LineageDigest:        redacted.LineageDigest,
			ReceiptDigest:        redacted.Digest,
			HumanContentDigest:   digest(human.Content),
			MachineContentDigest: digest(machine.Content),
			IssuedAt:             now.UTC(),
			ExpiresAt:            expiresAt.UTC(),
		},
		HumanArtifact:   human.Content,
		MachineArtifact: machine.Content,
	}

	_, pkg, err := SealAndSign(content, s.deps.PackageKey, s.deps.Signer, s.deps.SigningPolicy)
	if err != nil {
		s.failOperation(ctx, job, err)
		return
	}
	packageBytes, err := MarshalPackage(pkg)
	if err != nil {
		s.failOperation(ctx, job, err)
		return
	}
	ref, err := s.deps.Artifacts.Put(ctx, job.Tenant, job.ArtifactID, packageBytes)
	if err != nil {
		s.failOperation(ctx, job, err)
		return
	}

	artifactRef, intentID := ref, job.IntentID
	result := &intentsv1.TypedPayload{
		Schema: &intentsv1.SchemaReference{SchemaId: ContractVersion},
		CanonicalDigest: &intentsv1.CanonicalDigestReference{
			ProfileId:                 ContractVersion,
			SchemaId:                  ContractVersion,
			AlgorithmId:               "sha256+aes-256-gcm+ed25519",
			Digest:                    content.Manifest.ManifestDigest,
			CanonicalBytesArtifactRef: &artifactRef,
			IntentId:                  &intentID,
		},
	}
	_ = s.deps.Operations.Complete(ctx, job.Tenant, job.OperationID, result)
}

func (s *server) failOperation(ctx context.Context, job exportJob, cause error) {
	reason := reasonUnavailable
	code := commonv1.ErrorCode_ERROR_CODE_UNAVAILABLE
	switch {
	case errors.Is(cause, ErrLineageNotFound):
		reason, code = reasonNotFound, commonv1.ErrorCode_ERROR_CODE_NOT_FOUND
	case errors.Is(cause, ErrLineageIncomplete):
		reason, code = reasonLineageIncomplete, commonv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION
	case errors.Is(cause, ErrPurposeNotConfigured):
		reason, code = reasonUnauthorizedPurpose, commonv1.ErrorCode_ERROR_CODE_PERMISSION_DENIED
	case errors.Is(cause, kernelevidence.ErrDimensionOmitted), errors.Is(cause, kernelevidence.ErrDimensionUndigested), errors.Is(cause, kernelevidence.ErrDimensionUnexplained):
		reason, code = reasonLineageIncomplete, commonv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION
	}
	_ = s.deps.Operations.Fail(ctx, job.Tenant, job.OperationID, &commonv1.ErrorDetail{Code: code, ReasonRef: reason})
}

// redactReceipt returns receipt with every dimension not named in allowed
// moved to REDACTED, and the sorted list of names it redacted. allowed
// dimensions not present in evidence.Dimensions are ignored rather than
// causing a spurious redaction target.
func redactReceipt(receipt kernelevidence.BusinessExecutionReceipt, allowed []string, purpose string) (kernelevidence.BusinessExecutionReceipt, []string, error) {
	allowSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowSet[name] = true
	}
	var toRedact []string
	for _, d := range receipt.Dimensions {
		if !allowSet[d.Name] {
			toRedact = append(toRedact, d.Name)
		}
	}
	if len(toRedact) == 0 {
		return receipt, nil, nil
	}
	redacted, err := receipt.Redacted(redactionReason(purpose), toRedact...)
	if err != nil {
		return kernelevidence.BusinessExecutionReceipt{}, nil, fmt.Errorf("evidence: redact for export: %w", err)
	}
	return redacted, toRedact, nil
}

func redactionReason(purpose string) string {
	return "purpose_scope:" + purpose
}

// renderDimensionArtifacts renders the redacted receipt's dimension rows
// through internal/operations/export's two profiles: HUMAN_SPREADSHEET
// (formula-injection-safe, EXPORT-001) and MACHINE_DATA (exact values).
func renderDimensionArtifacts(receipt kernelevidence.BusinessExecutionReceipt, job exportJob, now, expiresAt time.Time) (exportformat.Artifact, exportformat.Artifact, error) {
	records := make([]exportformat.Record, 0, len(receipt.Dimensions))
	for _, d := range receipt.Dimensions {
		records = append(records, exportformat.Record{ID: d.Name, Fields: []exportformat.Field{
			{Name: "status", Value: string(d.Status)},
			{Name: "digest", Value: d.Digest},
			{Name: "note", Value: d.Note},
		}})
	}
	scope := job.OrganizationScope
	if scope == "" {
		scope = job.Tenant
	}
	base := exportformat.Request{
		TenantID: job.Tenant, SubjectID: job.RequestorSubject, Scope: scope,
		Purpose: job.Purpose, SchemaDigest: receipt.Digest, Classification: "CONFIDENTIAL_HR",
		DLPPolicyRef: "evidence_export/v1", AllowedFields: []string{"status", "digest", "note"},
		ExpiresAt: expiresAt, Records: records,
	}
	human, err := exportformat.RenderHumanSpreadsheet(base, now)
	if err != nil {
		return exportformat.Artifact{}, exportformat.Artifact{}, fmt.Errorf("evidence: render human artifact: %w", err)
	}
	machine, err := exportformat.RenderMachineData(base, now)
	if err != nil {
		return exportformat.Artifact{}, exportformat.Artifact{}, fmt.Errorf("evidence: render machine artifact: %w", err)
	}
	return human, machine, nil
}
