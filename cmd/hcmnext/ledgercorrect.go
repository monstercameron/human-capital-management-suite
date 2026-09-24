package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/data/operatorjournal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncorrection "github.com/monstercameron/human-capital-management-suite/internal/transaction/correction"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

const ledgerCorrectionUsage = "usage: hcmnext ledger-correct -tenant <uuid> -stream <key> -target-sequence <n> -expected-head <n> -operator <principal> -reason <text> -authority <ref> -schema-ref <ref> (-payload <text>|-artifact-ref <ref>) -occurred-at <RFC3339> -effective-at <RFC3339> [-source-ref <ref>] [-idempotency-key <key>] [-database-url <url>]"

var openLedgerCorrectionDB = func(ctx context.Context, url string) (dbport.Beginner, func(), error) {
	pool, err := pgxadapter.NewPool(ctx, url, nil)
	if err != nil {
		return nil, nil, err
	}
	return pool, pool.Close, nil
}

type ledgerCorrectionInput struct {
	TenantID       uuid.UUID
	StreamKey      string
	TargetSequence int64
	ExpectedHead   int64
	Operator       string
	Reason         string
	Authority      string
	SchemaRef      string
	Payload        string
	ArtifactRef    string
	SourceRef      string
	IdempotencyKey string
	OccurredAt     time.Time
	EffectiveAt    time.Time
}

func runLedgerCorrect(args []string, stdout, stderr io.Writer, now func() time.Time, open func(context.Context, string) (dbport.Beginner, func(), error)) int {
	fs := flag.NewFlagSet("ledger-correct", flag.ContinueOnError)
	fs.SetOutput(stderr)
	databaseURL := fs.String("database-url", os.Getenv(EnvDatabaseURL), "PostgreSQL URL (env "+EnvDatabaseURL+")")
	tenantFlag := fs.String("tenant", "", "tenant UUID (required)")
	stream := fs.String("stream", "", "ledger stream key (required)")
	target := fs.Int64("target-sequence", 0, "recorded event sequence being corrected (required)")
	head := fs.Int64("expected-head", -1, "expected stream head (required)")
	operatorID := fs.String("operator", "", "requesting operator principal (required)")
	reason := fs.String("reason", "", "correction reason (required)")
	authority := fs.String("authority", "", "authority reference (required)")
	schema := fs.String("schema-ref", "", "registered payload schema reference (required)")
	payload := fs.String("payload", "", "inline correction payload")
	artifact := fs.String("artifact-ref", "", "governed artifact reference")
	source := fs.String("source-ref", "", "source reference (defaults to hcmnext:ledger-correct)")
	key := fs.String("idempotency-key", "", "idempotency key (defaults to a generated UUID)")
	occurred := fs.String("occurred-at", "", "source occurrence time in RFC3339 format")
	effective := fs.String("effective-at", "", "business effective time in RFC3339 format")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*databaseURL) == "" {
		fmt.Fprintf(stderr, "hcmnext ledger-correct: -database-url or %s is required\n", EnvDatabaseURL)
		return 2
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(*tenantFlag))
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext ledger-correct: invalid -tenant: %v\n", err)
		return 2
	}
	parseTime := func(name, raw string) (time.Time, bool) {
		v, e := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
		if e != nil {
			fmt.Fprintf(stderr, "hcmnext ledger-correct: invalid -%s: %v\n", name, e)
			return time.Time{}, false
		}
		return v.UTC(), true
	}
	occurredAt, ok := parseTime("occurred-at", *occurred)
	if !ok {
		return 2
	}
	effectiveAt, ok := parseTime("effective-at", *effective)
	if !ok {
		return 2
	}
	in := ledgerCorrectionInput{TenantID: tenantID, StreamKey: strings.TrimSpace(*stream), TargetSequence: *target,
		ExpectedHead: *head, Operator: strings.TrimSpace(*operatorID), Reason: strings.TrimSpace(*reason),
		Authority: strings.TrimSpace(*authority), SchemaRef: strings.TrimSpace(*schema), Payload: *payload,
		ArtifactRef: strings.TrimSpace(*artifact), SourceRef: strings.TrimSpace(*source), IdempotencyKey: strings.TrimSpace(*key),
		OccurredAt: occurredAt, EffectiveAt: effectiveAt}
	if err := validateLedgerCorrectionInput(in); err != nil {
		fmt.Fprintf(stderr, "hcmnext ledger-correct: %v\n%s\n", err, ledgerCorrectionUsage)
		return 2
	}
	if in.SourceRef == "" {
		in.SourceRef = "hcmnext:ledger-correct"
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = "ledger-correction-" + uuid.NewString()
	}
	ctx := context.Background()
	db, closeDB, err := open(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext ledger-correct: %v\n", err)
		return 1
	}
	defer closeDB()
	receipt, err := executeLedgerCorrection(ctx, db, in, now)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext ledger-correct: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "outcome: %s effect=%s\nreceipt: %s\n", receipt.Outcome, receipt.EffectRef, receipt.Digest)
	return 0
}

func validateLedgerCorrectionInput(in ledgerCorrectionInput) error {
	switch {
	case in.TenantID == uuid.Nil:
		return errors.New("-tenant is required")
	case in.StreamKey == "":
		return errors.New("-stream is required")
	case in.TargetSequence < 1:
		return errors.New("-target-sequence must be positive")
	case in.ExpectedHead < in.TargetSequence:
		return errors.New("-expected-head must be at least the target sequence")
	case in.Operator == "":
		return errors.New("-operator is required")
	case in.Reason == "":
		return errors.New("-reason is required")
	case in.Authority == "":
		return errors.New("-authority is required")
	case in.SchemaRef == "":
		return errors.New("-schema-ref is required")
	case (in.Payload == "") == (in.ArtifactRef == ""):
		return errors.New("exactly one of -payload and -artifact-ref is required")
	case in.OccurredAt.IsZero() || in.EffectiveAt.IsZero():
		return errors.New("-occurred-at and -effective-at are required")
	}
	return nil
}

func executeLedgerCorrection(ctx context.Context, db dbport.Beginner, in ledgerCorrectionInput, now func() time.Time) (operator.Receipt, error) {
	if err := validateLedgerCorrectionInput(in); err != nil {
		return operator.Receipt{}, err
	}
	if now == nil {
		now = time.Now
	}
	clock := func() time.Time { return now().UTC() }
	tenantKey, err := lookupTenantKey(ctx, db, in.TenantID)
	if err != nil {
		return operator.Receipt{}, err
	}
	ids := func(t values.TenantId) (uuid.UUID, error) {
		if t != tenantKey {
			return uuid.Nil, fmt.Errorf("ledger correction: tenant %s has no storage identity", t)
		}
		return in.TenantID, nil
	}
	var payload []byte
	if in.Payload != "" {
		payload = []byte(in.Payload)
	}
	request := transactioncorrection.Request{Tenant: in.TenantID, StreamKey: in.StreamKey, ExpectedHead: in.ExpectedHead,
		Target: ledger.EventRef{StreamKey: in.StreamKey, Sequence: in.TargetSequence}, Authority: in.Authority,
		SourceRef: in.SourceRef, SchemaRef: in.SchemaRef, Payload: payload, ArtifactRef: in.ArtifactRef,
		OccurredAt: in.OccurredAt, EffectiveAt: in.EffectiveAt, CorrelationID: ledgerCorrectionCorrelation(in), IdempotencyKey: in.IdempotencyKey,
		Reason: in.Reason, CorrectedBy: in.Operator}
	requestDigest := digestLedgerCorrection(request)
	policy, _ := operator.PolicyFor(operator.KindLedgerCorrection)
	grants, err := truststore.New(db).ActiveJITGrants(ctx, in.TenantID, tenantKey, in.Operator, string(operator.KindLedgerCorrection), clock())
	if err != nil {
		return operator.Receipt{}, fmt.Errorf("load active correction grants: %w", err)
	}
	var grant *jit.Grant
	targetField := ledgerCorrectionTargetField(in.StreamKey, in.TargetSequence)
	for _, candidate := range grants {
		if !slices.Contains(candidate.Fields, targetField) {
			continue
		}
		for _, role := range policy.Roles {
			if candidate.Role == role {
				grant = candidate
				break
			}
		}
		if grant != nil {
			break
		}
	}
	var simulation *operator.Simulation
	// Request validation requires a ticket before the gateway can perform its
	// authorization check. A placeholder lets an absent/insufficient grant fail
	// through the normal authorization path; an eligible grant supplies its real
	// ticket below.
	ticketRef := "ledger-correction"
	if grant != nil {
		simulationDigest, err := simulateLedgerCorrection(ctx, db, in, requestDigest)
		if err != nil {
			return operator.Receipt{}, err
		}
		scope := operator.Scope{Resource: "ledger_event", IDs: []string{in.StreamKey + "@" + strconv.FormatInt(in.TargetSequence, 10)}}
		simulation = &operator.Simulation{Digest: simulationDigest, Scope: scope, At: clock()}
		ticketRef = grant.TicketRef
	}
	journal := &operatorjournal.Journal{DB: db, TenantIDs: operatorjournal.TenantIDs(ids)}
	executor := operator.ExecutorFunc(func(ctx context.Context, auth operator.Authorization, req operator.Request) (string, error) {
		if err := auth.Require(operator.KindLedgerCorrection, tenantKey); err != nil {
			return "", err
		}
		if req.PayloadDigest != requestDigest || req.Scope.Resource != "ledger_event" || len(req.Scope.IDs) != 1 || req.Scope.IDs[0] != in.StreamKey+"@"+strconv.FormatInt(in.TargetSequence, 10) {
			return "", fmt.Errorf("%w: correction request escaped its authorized target or digest", operator.ErrOperator)
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			return "", fmt.Errorf("%w: begin correction: %v", operator.ErrNoEffect, err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, in.TenantID); err != nil {
			_ = tx.Rollback(ctx)
			return "", fmt.Errorf("%w: bind correction tenant: %v", operator.ErrNoEffect, err)
		}
		result, err := transactioncorrection.Append(ctx, tx, request, clock)
		if err != nil {
			_ = tx.Rollback(ctx)
			return "", fmt.Errorf("%w: append correction: %v", operator.ErrNoEffect, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return "", fmt.Errorf("commit correction transaction: %w", err)
		}
		return fmt.Sprintf("correction=%s@%d effective=%s@%d", result.Correction.StreamKey, result.Correction.Sequence,
			result.Effective.Ref.StreamKey, result.Effective.Ref.Sequence), nil
	})
	gw, err := operator.NewGateway(journal, map[operator.Kind]operator.Executor{operator.KindLedgerCorrection: executor}, now)
	if err != nil {
		return operator.Receipt{}, err
	}
	scope := operator.Scope{Resource: "ledger_event", IDs: []string{in.StreamKey + "@" + strconv.FormatInt(in.TargetSequence, 10)}}
	return gw.Submit(ctx, operator.Request{Kind: operator.KindLedgerCorrection, Tenant: tenantKey, Operator: in.Operator,
		Scope: scope, ExpectedVersion: strconv.FormatInt(in.ExpectedHead, 10), PayloadDigest: requestDigest,
		IdempotencyKey: in.IdempotencyKey, Reason: in.Reason, TicketRef: ticketRef, JIT: grant,
		Simulation: simulation})
}

func ledgerCorrectionTargetField(stream string, sequence int64) string {
	return "ledger_event:" + stream + "@" + strconv.FormatInt(sequence, 10)
}

func digestLedgerCorrection(req transactioncorrection.Request) string {
	body, _ := json.Marshal(req)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ledgerCorrectionCorrelation(in ledgerCorrectionInput) uuid.UUID {
	identity := fmt.Sprintf("hcmnext:ledger-correction:%s:%s:%d:%s", in.TenantID, in.StreamKey, in.TargetSequence, in.IdempotencyKey)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(identity))
}

func simulateLedgerCorrection(ctx context.Context, db dbport.Beginner, in ledgerCorrectionInput, requestDigest string) (string, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("ledger correction simulation: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, in.TenantID); err != nil {
		return "", fmt.Errorf("ledger correction simulation: bind tenant: %w", err)
	}
	target, err := ledger.NewReader().ReadEvent(ctx, tx, in.TenantID, in.StreamKey, in.TargetSequence)
	if err != nil {
		return "", fmt.Errorf("ledger correction simulation: read target: %w", err)
	}
	current, path, err := lineage.EffectiveCurrent(ctx, tx, in.TenantID, ledger.EventRef{StreamKey: in.StreamKey, Sequence: in.TargetSequence})
	if err != nil {
		return "", fmt.Errorf("ledger correction simulation: resolve current-effective target: %w", err)
	}
	if target.Tenant != in.TenantID || target.StreamKey != in.StreamKey || target.Sequence != in.TargetSequence {
		return "", fmt.Errorf("ledger correction simulation: target identity did not match the requested tenant and event")
	}
	state, err := json.Marshal(struct {
		RequestDigest  string
		TargetEventID  string
		CurrentEventID string
		PathLength     int
	}{requestDigest, target.EventID.String(), current.EventID.String(), len(path)})
	if err != nil {
		return "", fmt.Errorf("ledger correction simulation: encode state: %w", err)
	}
	sum := sha256.Sum256(state)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
