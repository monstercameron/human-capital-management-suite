package legal_test

// Shared harness for the LEGAL-ST-<STATE>-001 series: each todo asks that a
// state's promotion/base-pay obligation parameters be carried in a reviewed
// RulePack. "Reviewed" is the contract's section 7.1 pipeline — the checked-in
// draft under definitions/legal/packs/states is authored, reviewed to
// VENDOR_BASELINE, published and registered, and the spec's section 8.1
// golden-vector layout records the result under testdata/legal/us-<code>/:
//
//	release.json            the PackRelease as published (definition + review
//	                        status + digest + signatures), the review record
//	                        and the pipeline event chain
//	proposal.json           the canonical promotion + base-pay-change proposal
//	receipt.golden.json     the exact LegalEvaluationReceipt
//
// The proposal is one fixed scenario for every state (spec section 8.1): an
// internal promotion of a non-exempt worker to an exempt role, base pay
// rising across every non-compete threshold in the corpus, a fixed effective
// date, an existing covenant, a mandated leave balance, and no concurrent
// separation or reduction — so differences between states' receipts are
// attributable to the pack alone.
//
// These files sit in package legal_test because the pipeline package imports
// legal — an internal test file would close an import cycle.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal/extract"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal/pipeline"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The three pipeline principals the contract's section 7.1 separation of
// duties requires: rule author, vendor legal reviewer, release publisher.
// They are stable identifiers so signed goldens are reproducible.
const (
	legalStAuthorID    = "vendor:rule-author"
	legalStReviewerID  = "vendor:legal-reviewer"
	legalStPublisherID = "vendor:release-publisher"
)

// Fixed signer seeds, one per principal — deterministic ed25519 keys so every
// checked-in golden reproduces byte for byte.
const (
	legalStAuthorSeed    byte = 0x41
	legalStReviewerSeed  byte = 0x42
	legalStPublisherSeed byte = 0x43
	legalStReceiptSeed   byte = 0x44
)

// legalStNowUnix is the recorded-at instant for every golden receipt;
// KnownAt is one hour earlier. 1_783_267_200 is 2026-07-01T00:00:00Z.
const legalStNowUnix int64 = 1_783_267_200

func legalStSigner(t *testing.T, seed byte) *legal.Signer {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	signer, err := legal.NewSigner(priv)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return signer
}

func legalStInstant(t *testing.T, unixSec int64) values.Instant {
	t.Helper()
	instant, err := values.NewInstantFromUnix(unixSec, 0)
	if err != nil {
		t.Fatalf("NewInstantFromUnix: %v", err)
	}
	return instant
}

func legalStDate(t *testing.T, year int, month time.Month, day int) values.LocalDate {
	t.Helper()
	date, err := values.NewLocalDate(year, month, day)
	if err != nil {
		t.Fatalf("NewLocalDate: %v", err)
	}
	return date
}

func legalStDefinitionPath(t *testing.T, code string) string {
	t.Helper()
	path, err := legal.PackDefinitionPath("states", fmt.Sprintf("us-%s.json", strings.ToLower(code)))
	if err != nil {
		t.Fatalf("PackDefinitionPath: %v", err)
	}
	return path
}

// legalStTableA is the contract's section 5.1 column set: the obligation
// kinds the promotion/base-pay flow consumes. A '?' in any of these cells
// bars VENDOR_BASELINE for the state (spec section 5: a '?' is a blocking
// finding, not a footnote).
var legalStTableA = []legal.ObligationType{
	legal.ObligationTypeNotice,
	legal.ObligationTypePayTransparency,
	legal.ObligationTypeFieldRestriction,
	legal.ObligationTypeWageFloor,
	legal.ObligationTypePayFrequency,
	legal.ObligationTypePayStatement,
	legal.ObligationTypeLeaveInteraction,
	legal.ObligationTypeNonCompete,
	legal.ObligationTypeClassification,
	legal.ObligationTypePayEquityReview,
	legal.ObligationTypeRetention,
	legal.ObligationTypePersonnelFile,
}

// legalStRequiredFields mirrors [validateComplete] (obligation_complete.go):
// the fields a released pack must state per kind. The mirror exists because
// the completeness rule is unexported — the harness re-checks it to decide
// the honest review status and to enumerate gaps into findings.
var legalStRequiredFields = map[legal.ObligationType][]string{
	legal.ObligationTypeNotice:             {"timing_direction", "timing_days", "channel"},
	legal.ObligationTypeFieldRestriction:   {"restricted_fields"},
	legal.ObligationTypeRetention:          {"record_class", "duration_years"},
	legal.ObligationTypeLeaveInteraction:   {"leave_type", "interaction_rule"},
	legal.ObligationTypePayFrequency:       {"minimum_frequency"},
	legal.ObligationTypeWageFloor:          {"floor_amount", "basis", "indexation"},
	legal.ObligationTypePayEquityReview:    {"protected_bases", "comparator_standard"},
	legal.ObligationTypePayStatement:       {"required_fields", "delivery"},
	legal.ObligationTypeClassification:     {"dimension", "test_description"},
	legal.ObligationTypePersonnelFile:      {"response_days", "day_basis"},
	legal.ObligationTypeAntiRetaliation:    {"protected_activities", "lookback_days", "disposition"},
	legal.ObligationTypeJobSecurity:        {"standard_kind"},
	legal.ObligationTypeSeparationFiling:   {"form_name"},
	legal.ObligationTypeDrugTesting:        {"permitted_bases"},
	legal.ObligationTypeBreachNotification: {"subject_deadline_days", "day_basis"},
	legal.ObligationTypeAutomatedDecision:  {"covered_uses"},
	legal.ObligationTypeMonitoringConsent:  {"data_categories", "consent_form"},
}

// legalStGap is one obligation's unstated required fields.
type legalStGap struct {
	ObligationID string
	Missing      []string
}

// legalStObligationJSONs reads every obligation in the state's generated pack
// file as the wire JSON it is written in, tagged with its kind, in
// deterministic order (by id). Reading the file — rather than re-marshaling
// the typed slices — tests the artifact reviewers actually see and sidesteps
// typed fields (e.g. unset dates) that do not marshal cleanly.
type legalStObligation struct {
	Kind legal.ObligationType
	ID   string
	// Text is the obligation's complete JSON: id, kind, citation and body.
	Text string
	// Body is the obligation's wire body (snake_case keys).
	Body map[string]json.RawMessage
}

func legalStObligationJSONs(t *testing.T, code string) []legalStObligation {
	t.Helper()
	root, err := legal.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	path := filepath.Join(root, "definitions", "legal", "packs", "states", "us-"+strings.ToLower(code)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pack %s: %v", path, err)
	}
	var def struct {
		Obligations []struct {
			ID       string                     `json:"id"`
			Kind     string                     `json:"kind"`
			Citation map[string]json.RawMessage `json:"citation"`
			Body     map[string]json.RawMessage `json:"body"`
		} `json:"obligations"`
	}
	if err := json.Unmarshal(data, &def); err != nil {
		t.Fatalf("parse pack %s: %v", path, err)
	}
	out := make([]legalStObligation, 0, len(def.Obligations))
	for _, o := range def.Obligations {
		kind, err := legal.ParseObligationType(o.Kind)
		if err != nil {
			t.Fatalf("obligation %s: kind %q: %v", o.ID, o.Kind, err)
		}
		blob, err := json.Marshal(o)
		if err != nil {
			t.Fatalf("re-marshal obligation %s: %v", o.ID, err)
		}
		out = append(out, legalStObligation{Kind: kind, ID: o.ID, Text: string(blob), Body: o.Body})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// legalStField reads one wire field name ("timing_direction") from an
// obligation body.
func legalStField(m map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	raw, ok := m[key]
	return raw, ok
}

func legalStStringField(m map[string]json.RawMessage, key string) string {
	var v string
	if raw, ok := legalStField(m, key); ok {
		_ = json.Unmarshal(raw, &v)
	}
	return v
}

// legalStFieldStated reports whether one rendered obligation carries a
// non-empty value for a required field.
func legalStFieldStated(m map[string]json.RawMessage, key string) bool {
	raw, ok := legalStField(m, key)
	if !ok {
		return false
	}
	s := strings.TrimSpace(string(raw))
	switch s {
	case "", `0`, `""`, `null`, `[]`, `{}`:
		return false
	}
	return true
}

// legalStGaps returns every completeness gap in the pack: the fields
// [validateComplete] would refuse at a releasable status.
func legalStGaps(t *testing.T, code string) []legalStGap {
	t.Helper()
	var gaps []legalStGap
	for _, o := range legalStObligationJSONs(t, code) {
		required, ok := legalStRequiredFields[o.Kind]
		if !ok {
			continue
		}
		var missing []string
		for _, f := range required {
			if !legalStFieldStated(o.Body, f) {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			gaps = append(gaps, legalStGap{ObligationID: o.ID, Missing: missing})
		}
	}
	return gaps
}

// legalStUncertainCells returns the state's '?' cells in the contract's
// section 5 matrix for the flow-consumed (table A) kinds.
func legalStUncertainCells(t *testing.T, code string) []legal.ObligationType {
	t.Helper()
	root, err := legal.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	matrix, err := extract.LoadMatrix(root)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	var out []legal.ObligationType
	row := matrix.Cells[strings.ToUpper(code)]
	for _, kind := range legalStTableA {
		if row[kind].Value == extract.CellUncertain {
			out = append(out, kind)
		}
	}
	return out
}

// legalStReviewInputs derives the honest review status for the state pack
// and the findings that record why: VENDOR_BASELINE only when every emitted
// obligation is complete and no flow-consumed matrix cell is '?' — otherwise
// REQUIRES_CUSTOMER_COUNSEL_CONFIGURATION, the contract's deliberately
// unresolved published status (spec section 7.2).
func legalStReviewInputs(t *testing.T, code string, extra []legal.ReviewFinding) (legal.ReviewStatus, []legal.ReviewFinding) {
	t.Helper()
	findings := append([]legal.ReviewFinding{}, extra...)
	gaps := legalStGaps(t, code)
	uncertain := legalStUncertainCells(t, code)
	for _, g := range gaps {
		findings = append(findings, legal.ReviewFinding{
			ObligationID: g.ObligationID,
			Severity:     legal.FindingSeverityBlocking,
			Note: "required field(s) " + strings.Join(g.Missing, ", ") +
				" are unstated in the source research; published unresolved so the gap is recorded, not fabricated",
		})
	}
	for _, kind := range uncertain {
		findings = append(findings, legal.ReviewFinding{
			Severity: legal.FindingSeverityBlocking,
			Note: "contract section 5 matrix marks " + kind.String() + " '?' for " +
				strings.ToUpper(code) + "; a flow-consumed '?' is a blocking finding, not a footnote",
		})
	}
	for _, o := range legalStObligationJSONs(t, code) {
		if !strings.Contains(o.Text, `"VERIFY"`) {
			continue
		}
		findings = append(findings, legal.ReviewFinding{
			ObligationID: o.ID,
			Severity:     legal.FindingSeverityConcern,
			Note:         "obligation rests on VERIFY-marked evidence in the research corpus",
		})
	}
	status := legal.ReviewStatusVendorBaseline
	if len(gaps) > 0 || len(uncertain) > 0 {
		status = legal.ReviewStatusRequiresCustomerCounselConfiguration
	}
	return status, findings
}

// legalStPack publishes the state's checked-in draft through the full
// authoring/review pipeline at the status the pack honestly earns —
// VENDOR_BASELINE for a complete pack over a '?'-free consumed matrix row,
// REQUIRES_CUSTOMER_COUNSEL_CONFIGURATION otherwise — and returns the
// verifying pipeline, the published release, the registry holding it and the
// status it was reviewed to.
func legalStPack(t *testing.T, code string, extra []legal.ReviewFinding) (*pipeline.Pipeline, legal.PackRelease, *legal.Registry, legal.ReviewStatus) {
	t.Helper()
	data, err := os.ReadFile(legalStDefinitionPath(t, code))
	if err != nil {
		t.Fatalf("read definition: %v", err)
	}
	p, err := pipeline.Author(data, legalStAuthorID, legalStSigner(t, legalStAuthorSeed))
	if err != nil {
		t.Fatalf("Author: %v", err)
	}
	status, findings := legalStReviewInputs(t, code, extra)
	if err := p.Review(legalStReviewerID, status, findings, legalStSigner(t, legalStReviewerSeed)); err != nil {
		t.Fatalf("Review: %v", err)
	}
	registry := legal.NewRegistry()
	release, err := p.Publish(legalStPublisherID, legalStSigner(t, legalStPublisherSeed), "", nil, registry)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if release.ReviewStatus != status {
		t.Fatalf("release status %s != reviewed status %s", release.ReviewStatus, status)
	}
	return p, release, registry, status
}

// legalStGoldenDir is the section 8.1 directory for one state, relative to
// this package (tests run with the package directory as working directory).
func legalStGoldenDir(code string) string {
	return filepath.Join("testdata", "legal", "us-"+strings.ToLower(code))
}

// legalStCheckGolden compares got against the checked-in golden at path,
// regenerating it when HCMNEXT_UPDATE_GOLDEN is set (the repo's convention).
func legalStCheckGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s drifted from the checked-in golden (set HCMNEXT_UPDATE_GOLDEN=1 to regenerate)", path)
	}
}

// --- release.json ---------------------------------------------------------

type legalStSignatureJSON struct {
	Role      string `json:"role"`
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

type legalStEventJSON struct {
	Stage          string               `json:"stage"`
	PrincipalID    string               `json:"principal_id"`
	Role           string               `json:"role"`
	ArtifactDigest string               `json:"artifact_digest"`
	PrevDigest     string               `json:"prev_digest"`
	Digest         string               `json:"digest"`
	Signature      legalStSignatureJSON `json:"signature"`
}

type legalStReviewFindingJSON struct {
	ObligationID string `json:"obligation_id,omitempty"`
	Severity     string `json:"severity"`
	Note         string `json:"note"`
}

type legalStReviewRecordJSON struct {
	PackDigest string                     `json:"pack_digest"`
	AuthorID   string                     `json:"author_id"`
	ReviewerID string                     `json:"reviewer_id"`
	Status     string                     `json:"status"`
	Findings   []legalStReviewFindingJSON `json:"findings"`
}

// legalStReleaseJSON is the section 8.1 release.json: the published
// PackRelease (definition + review status + digest + role signatures), the
// review record that raised its status, and the pipeline event chain that
// proves how it got there.
type legalStReleaseJSON struct {
	Definition   json.RawMessage         `json:"definition"`
	ReviewStatus string                  `json:"review_status"`
	Digest       string                  `json:"digest"`
	Signatures   []legalStSignatureJSON  `json:"signatures"`
	ReviewRecord legalStReviewRecordJSON `json:"review_record"`
	Events       []legalStEventJSON      `json:"pipeline_events"`
}

func legalStMarshalRelease(t *testing.T, p *pipeline.Pipeline, release legal.PackRelease) []byte {
	t.Helper()
	out := legalStReleaseJSON{
		ReviewStatus: release.ReviewStatus.String(),
		Digest:       release.Digest,
	}
	p.Draft.Definition.ReviewStatus = release.ReviewStatus.String()
	defJSON, err := legal.MarshalPackDefinition(p.Draft.Definition)
	if err != nil {
		t.Fatalf("MarshalPackDefinition: %v", err)
	}
	out.Definition = defJSON
	for _, s := range release.Signatures {
		out.Signatures = append(out.Signatures, legalStSignatureJSON{
			Role:      string(s.Role),
			PublicKey: fmt.Sprintf("%x", s.Signature.PublicKey),
			Signature: fmt.Sprintf("%x", s.Signature.Bytes),
		})
	}
	out.ReviewRecord.PackDigest = p.ReviewRecord.PackDigest
	out.ReviewRecord.AuthorID = p.ReviewRecord.AuthorID
	out.ReviewRecord.ReviewerID = p.ReviewRecord.ReviewerID
	out.ReviewRecord.Status = p.ReviewRecord.Status.String()
	for _, f := range p.ReviewRecord.Findings {
		out.ReviewRecord.Findings = append(out.ReviewRecord.Findings, legalStReviewFindingJSON{
			ObligationID: f.ObligationID,
			Severity:     f.Severity.String(),
			Note:         f.Note,
		})
	}
	for _, e := range p.Events {
		out.Events = append(out.Events, legalStEventJSON{
			Stage:          string(e.Stage),
			PrincipalID:    e.PrincipalID,
			Role:           string(e.Role),
			ArtifactDigest: e.ArtifactDigest,
			PrevDigest:     e.PrevDigest,
			Digest:         e.Digest,
			Signature: legalStSignatureJSON{
				PublicKey: fmt.Sprintf("%x", e.Signature.PublicKey),
				Signature: fmt.Sprintf("%x", e.Signature.Bytes),
			},
		})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshal release: %v", err)
	}
	return append(b, '\n')
}

// --- proposal.json --------------------------------------------------------

// legalStCanonicalProposal is the fixed scenario section 8.1 prescribes: an
// internal promotion of a non-exempt worker into an exempt role, pay rising
// across every non-compete threshold the corpus uses ($45k non-solicit /
// $75k non-compete are the corpus's extreme employee thresholds), an existing
// covenant, a mandated leave balance, and no separation, demotion or
// reduction.
func legalStCanonicalProposal(t *testing.T) legal.PromotionProposalSnapshot {
	t.Helper()
	current, err := values.NewMoney("60000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("current pay: %v", err)
	}
	next, err := values.NewMoney("90000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("new pay: %v", err)
	}
	return legal.PromotionProposalSnapshot{
		WorkerID:              "worker-canonical",
		LegalEntityID:         "legal-entity-1",
		EffectiveDate:         legalStDate(t, 2026, time.July, 1),
		CurrentBasePay:        current,
		NewBasePay:            next,
		PayFrequency:          "SEMIMONTHLY",
		IsInternalPromotion:   true,
		HasExistingNonCompete: true,
		OnProtectedLeave:      true,
		LeaveProgramBalances:  []string{"accrued paid sick leave"},
		RoleChanged:           true,
	}
}

func legalStMarshalProposal(t *testing.T, proposal legal.PromotionProposalSnapshot) []byte {
	t.Helper()
	b, err := json.MarshalIndent(proposal, "", "  ")
	if err != nil {
		t.Fatalf("marshal proposal: %v", err)
	}
	return append(b, '\n')
}

// --- receipt.golden.json --------------------------------------------------

// legalStEvaluate resolves the state's registered release against the
// canonical proposal and returns the exact receipt the section 8.1 golden
// pins.
func legalStEvaluate(t *testing.T, code string, registry *legal.Registry) []byte {
	t.Helper()
	jurisdiction := legal.Jurisdiction{Country: "US", State: strings.ToUpper(code)}
	knownAt, err := values.NewKnownAt(legalStInstant(t, legalStNowUnix-3600))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	signer := legalStSigner(t, legalStReceiptSeed)
	ctx, err := legal.Resolve(legal.LegalContextInput{
		LegalEntityID:          "legal-entity-1",
		WorkLocation:           jurisdiction,
		EmploymentJurisdiction: jurisdiction,
		EffectiveDate:          legalStDate(t, 2026, time.July, 1),
		KnownAt:                knownAt,
	}, registry, signer, legalStInstant(t, legalStNowUnix))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	receipt, err := legal.EvaluateReceipt(ctx, legalStCanonicalProposal(t), registry, signer, legalStInstant(t, legalStNowUnix))
	if err != nil {
		t.Fatalf("EvaluateReceipt: %v", err)
	}
	b, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatalf("marshal receipt: %v", err)
	}
	return append(b, '\n')
}

// legalStGoldens regenerates all three section 8.1 artifacts for the state
// and compares them against the checked-in files.
func legalStGoldens(t *testing.T, code string, p *pipeline.Pipeline, release legal.PackRelease, registry *legal.Registry) {
	t.Helper()
	dir := legalStGoldenDir(code)
	legalStCheckGolden(t, filepath.Join(dir, "release.json"), legalStMarshalRelease(t, p, release))
	legalStCheckGolden(t, filepath.Join(dir, "proposal.json"), legalStMarshalProposal(t, legalStCanonicalProposal(t)))
	legalStCheckGolden(t, filepath.Join(dir, "receipt.golden.json"), legalStEvaluate(t, code, registry))
}

// --- conformance -----------------------------------------------------------

// legalStConformance re-runs the state's pipeline and proves the whole chain
// verifies: the event chain is untampered, the release verifies its own
// signatures, the review lands at the status the pack honestly earns, the
// review record names two distinct actors, and every citation still resolves
// to a real research file.
func legalStConformance(t *testing.T, code string, extra []legal.ReviewFinding, wantStatus legal.ReviewStatus) {
	t.Helper()
	p, release, _, status := legalStPack(t, code, extra)
	if err := p.VerifyChain(); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if err := release.Verify(); err != nil {
		t.Fatalf("release.Verify: %v", err)
	}
	if status != wantStatus {
		t.Fatalf("review status = %s, want %s", status, wantStatus)
	}
	if p.ReviewRecord.AuthorID == "" || p.ReviewRecord.ReviewerID == "" || p.ReviewRecord.AuthorID == p.ReviewRecord.ReviewerID {
		t.Fatalf("review record actors invalid: %+v", p.ReviewRecord)
	}
	if release.Digest == "" || len(release.Signatures) == 0 {
		t.Fatal("published release carries no digest or signatures")
	}
	root, err := legal.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	for _, o := range p.Draft.Definition.Obligations {
		src := o.Citation.SourceFile
		if src == "" {
			t.Errorf("obligation %s carries no source file", o.ID)
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(src))); err != nil {
			t.Errorf("obligation %s cites %s, which does not exist", o.ID, src)
		}
	}
	for _, a := range p.Draft.Definition.Preemptions {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(a.Citation.SourceFile))); err != nil {
			t.Errorf("preemption %s cites %s, which does not exist", a.Kind, a.Citation.SourceFile)
		}
	}
}

// --- mutation ----------------------------------------------------------------

// legalStMutation proves the published artifact is tamper-evident the way the
// contract's section 7.2 signature model intends: mutating any content field
// after signing must break the release's own digest check, and mutating a
// signature byte must break it the other way. The golden receipt pins the
// untampered bytes; this proves the pin has teeth.
func legalStMutation(t *testing.T, code string, extra []legal.ReviewFinding) {
	t.Helper()
	_, release, _, _ := legalStPack(t, code, extra)
	if err := release.Verify(); err != nil {
		t.Fatalf("pre-mutation release must verify: %v", err)
	}
	// Content mutation: the recomputed canonical digest no longer matches.
	mutated := release
	mutated.Version++
	if err := mutated.Verify(); err == nil {
		t.Fatal("mutated release still verifies: digest is not content-bound")
	}
	// Signature mutation: same content, corrupted signature bytes.
	forged := release
	forged.Signatures = append([]legal.RoleSignature(nil), release.Signatures...)
	forged.Signatures[0].Signature.Bytes[0] ^= 0xff
	if err := forged.Verify(); err == nil {
		t.Fatal("forged signature still verifies")
	}
	// Registry lookup must refuse a release identity it never saw.
	registry := legal.NewRegistry()
	mutatedRef := release.Release()
	mutatedRef.MinorVersion++
	if _, err := registry.GetExact(mutatedRef); err == nil {
		t.Fatal("registry returned a release it never registered")
	}
}

// --- expectation helpers ---------------------------------------------------

// legalStExpectation is one GREEN-contract line reduced to an assertion: the
// obligation kind must be present (or explicitly absent when Absent is set),
// and each token in Requires must appear in at least one of the kind's
// obligations' rendered bodies (which include the citation's source file,
// section and note).
type legalStExpectation struct {
	Kind     legal.ObligationType
	Absent   bool
	Requires []string
}

// legalStKindTexts renders every obligation of every kind in the pack into
// searchable text keyed by kind. fmt %v over a rule prints all exported
// fields, including the citation.
func legalStKindTexts(t *testing.T, code string) map[legal.ObligationType][]string {
	t.Helper()
	out := map[legal.ObligationType][]string{}
	for _, o := range legalStObligationJSONs(t, code) {
		out[o.Kind] = append(out[o.Kind], o.Text)
	}
	return out
}

func legalStAssertPack(t *testing.T, code string, expectations []legalStExpectation) {
	t.Helper()
	texts := legalStKindTexts(t, code)
	for _, exp := range expectations {
		blobs := texts[exp.Kind]
		if exp.Absent {
			if len(blobs) != 0 {
				t.Errorf("us-%s: kind %s must be absent (federal baseline / no state rule) but the pack carries %s", strings.ToLower(code), exp.Kind, strings.Join(blobs, " | "))
			}
			continue
		}
		if len(blobs) == 0 {
			t.Errorf("us-%s: kind %s required by the todo's GREEN contract but absent from the pack", strings.ToLower(code), exp.Kind)
			continue
		}
		joined := strings.Join(blobs, "\n")
		for _, want := range exp.Requires {
			if !strings.Contains(joined, want) {
				t.Errorf("us-%s: kind %s does not carry %q in its rendered obligations", strings.ToLower(code), exp.Kind, want)
			}
		}
	}
}
