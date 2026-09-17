// Governed print and postal fulfillment (FULFILL-001) for required
// physical notices.
//
// A fulfillment instruction pins the minimized artifact hash, the exact
// artifact version, the authorized address revision, the envelope and
// privacy profile and the vendor operation behind one digest. The vendor
// sees only that minimized view: hashes and profiles, never tenant
// internals or notice references. Custody receipts chain without gaps
// from vendor receipt to carrier tender; delivery outcomes follow a
// governed lifecycle where carrier acceptance is custody evidence and
// never recipient acknowledgement. Returned mail creates exactly one
// governed fallback work item, never a duplicate notice.
//
// This file governs the instruction a vendor executes; it never prints,
// mails or contacts a provider itself.
package messaging

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Fulfillment errors.
var (
	ErrInvalidFulfillment = errors.New("messaging: invalid physical fulfillment")
	ErrFulfillmentState   = errors.New("messaging: fulfillment state forbids the recording")
)

// EnvelopeProfile is the closed envelope vocabulary.
type EnvelopeProfile string

const (
	EnvelopeWindowed      EnvelopeProfile = "WINDOWED"
	EnvelopeFlatCertified EnvelopeProfile = "FLAT_CERTIFIED"
)

func (p EnvelopeProfile) valid() bool {
	return p == EnvelopeWindowed || p == EnvelopeFlatCertified
}

// PrivacyProfile is the closed privacy vocabulary for physical handling.
type PrivacyProfile string

const (
	PrivacySealed     PrivacyProfile = "SEALED"
	PrivacyRegistered PrivacyProfile = "REGISTERED"
)

func (p PrivacyProfile) valid() bool {
	return p == PrivacySealed || p == PrivacyRegistered
}

// VendorOperation is the closed vendor-operation vocabulary.
type VendorOperation string

const (
	OpPrintOnly    VendorOperation = "PRINT_ONLY"
	OpMailOnly     VendorOperation = "MAIL_ONLY"
	OpPrintAndMail VendorOperation = "PRINT_AND_MAIL"
)

func (o VendorOperation) valid() bool {
	return o == OpPrintOnly || o == OpMailOnly || o == OpPrintAndMail
}

// AddressRevision is one authorized recipient-address revision. The
// authorizer names who approved the address; an instruction without one
// never enters a batch.
type AddressRevision struct {
	Revision     uint64
	Digest       string
	AuthorizedBy string
}

// FulfillmentRequest asks for one governed instruction.
type FulfillmentRequest struct {
	InstructionID   string
	TenantID        string
	NoticeRef       string
	ArtifactHash    string
	ArtifactVersion string
	Address         AddressRevision
	EnvelopeProfile EnvelopeProfile
	PrivacyProfile  PrivacyProfile
	VendorOperation VendorOperation
	VendorID        string
}

// FulfillmentInstruction is the pinned instruction a vendor executes.
type FulfillmentInstruction struct {
	InstructionID   string
	TenantID        string
	NoticeRef       string
	ArtifactHash    string
	ArtifactVersion string
	Address         AddressRevision
	EnvelopeProfile EnvelopeProfile
	PrivacyProfile  PrivacyProfile
	VendorOperation VendorOperation
	VendorID        string
	Digest          string
}

// Validate checks the instruction contract.
func (in FulfillmentInstruction) Validate() error {
	if strings.TrimSpace(in.InstructionID) == "" || strings.TrimSpace(in.TenantID) == "" ||
		strings.TrimSpace(in.NoticeRef) == "" || strings.TrimSpace(in.VendorID) == "" {
		return fmt.Errorf("%w: instruction, tenant, notice and vendor are required", ErrInvalidFulfillment)
	}
	if strings.TrimSpace(in.ArtifactHash) == "" || strings.TrimSpace(in.ArtifactVersion) == "" {
		return fmt.Errorf("%w: minimized artifact hash and version are required", ErrInvalidFulfillment)
	}
	if in.Address.Revision == 0 || strings.TrimSpace(in.Address.Digest) == "" ||
		strings.TrimSpace(in.Address.AuthorizedBy) == "" {
		return fmt.Errorf("%w: authorized address revision, digest and authorizer are required", ErrInvalidFulfillment)
	}
	if !in.EnvelopeProfile.valid() {
		return fmt.Errorf("%w: unsupported envelope profile %q", ErrInvalidFulfillment, in.EnvelopeProfile)
	}
	if !in.PrivacyProfile.valid() {
		return fmt.Errorf("%w: unsupported privacy profile %q", ErrInvalidFulfillment, in.PrivacyProfile)
	}
	if !in.VendorOperation.valid() {
		return fmt.Errorf("%w: unsupported vendor operation %q", ErrInvalidFulfillment, in.VendorOperation)
	}
	if in.Digest != "" && in.Digest != in.computedDigest() {
		return fmt.Errorf("%w: instruction digest mismatch", ErrInvalidFulfillment)
	}
	return nil
}

func (in FulfillmentInstruction) computedDigest() string {
	sum := sha256.New()
	fmt.Fprintf(sum, "%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		in.InstructionID, in.TenantID, in.NoticeRef, in.ArtifactHash, in.ArtifactVersion,
		in.Address.Revision, in.Address.Digest, in.Address.AuthorizedBy,
		in.EnvelopeProfile, in.PrivacyProfile, in.VendorOperation, in.VendorID)
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

// IssueFulfillment validates and pins one instruction.
func IssueFulfillment(req FulfillmentRequest) (FulfillmentInstruction, error) {
	instruction := FulfillmentInstruction{
		InstructionID: req.InstructionID, TenantID: req.TenantID, NoticeRef: req.NoticeRef,
		ArtifactHash: req.ArtifactHash, ArtifactVersion: req.ArtifactVersion, Address: req.Address,
		EnvelopeProfile: req.EnvelopeProfile, PrivacyProfile: req.PrivacyProfile,
		VendorOperation: req.VendorOperation, VendorID: req.VendorID,
	}
	instruction.Digest = instruction.computedDigest()
	if err := instruction.Validate(); err != nil {
		return FulfillmentInstruction{}, err
	}
	return instruction, nil
}

// VendorInstruction is the minimized view a vendor may see. It carries
// hashes and handling profiles only; tenant, notice and instruction
// identities never leave this package.
type VendorInstruction struct {
	InstructionDigest string
	ArtifactHash      string
	AddressDigest     string
	AddressRevision   uint64
	EnvelopeProfile   EnvelopeProfile
	PrivacyProfile    PrivacyProfile
	VendorOperation   VendorOperation
	VendorID          string
}

// VendorView returns the minimized vendor view of an instruction.
func (in FulfillmentInstruction) VendorView() VendorInstruction {
	return VendorInstruction{
		InstructionDigest: in.Digest, ArtifactHash: in.ArtifactHash,
		AddressDigest: in.Address.Digest, AddressRevision: in.Address.Revision,
		EnvelopeProfile: in.EnvelopeProfile, PrivacyProfile: in.PrivacyProfile,
		VendorOperation: in.VendorOperation, VendorID: in.VendorID,
	}
}

// CustodyStage is the closed chain-of-custody vocabulary. Tender to the
// carrier is custody evidence; it is never a delivery outcome.
type CustodyStage string

const (
	StageReceived          CustodyStage = "RECEIVED_AT_VENDOR"
	StagePrinted           CustodyStage = "PRINTED"
	StageTenderedToCarrier CustodyStage = "TENDERED_TO_CARRIER"
)

func (s CustodyStage) valid() bool {
	return s == StageReceived || s == StagePrinted || s == StageTenderedToCarrier
}

// CustodyReceipt is one link in the chain of custody. Sequences start at
// one and must stay contiguous: a gap or replay breaks the chain and is
// refused.
type CustodyReceipt struct {
	InstructionDigest string
	Sequence          uint64
	Stage             CustodyStage
	VendorID          string
	At                time.Time
}

// FulfillmentOutcome is the closed delivery-outcome vocabulary.
type FulfillmentOutcome string

const (
	OutcomePrinted   FulfillmentOutcome = "PRINTED"
	OutcomeMailed    FulfillmentOutcome = "MAILED"
	OutcomeInTransit FulfillmentOutcome = "IN_TRANSIT"
	OutcomeDelivered FulfillmentOutcome = "DELIVERED"
	OutcomeReturned  FulfillmentOutcome = "RETURNED"
	OutcomeUnknown   FulfillmentOutcome = "UNKNOWN"
)

func (o FulfillmentOutcome) valid() bool {
	switch o {
	case OutcomePrinted, OutcomeMailed, OutcomeInTransit,
		OutcomeDelivered, OutcomeReturned, OutcomeUnknown:
		return true
	default:
		return false
	}
}

// terminal reports whether no further outcome may follow.
func (o FulfillmentOutcome) terminal() bool {
	return o == OutcomeDelivered || o == OutcomeReturned
}

// ProofKind is the closed evidence vocabulary for outcomes. Carrier scans
// prove transit; only a recipient signature proves acknowledgement.
type ProofKind string

const (
	ProofPrintLog           ProofKind = "PRINT_LOG"
	ProofPostalManifest     ProofKind = "POSTAL_MANIFEST"
	ProofCarrierScan        ProofKind = "CARRIER_SCAN"
	ProofRecipientSignature ProofKind = "RECIPIENT_SIGNATURE"
	ProofReturnScan         ProofKind = "RETURN_SCAN"
	ProofLossAttestation    ProofKind = "LOSS_ATTESTATION"
)

// proofFor binds each outcome to the exact proof that may establish it.
func proofFor(outcome FulfillmentOutcome) ProofKind {
	switch outcome {
	case OutcomePrinted:
		return ProofPrintLog
	case OutcomeMailed:
		return ProofPostalManifest
	case OutcomeInTransit:
		return ProofCarrierScan
	case OutcomeDelivered:
		return ProofRecipientSignature
	case OutcomeReturned:
		return ProofReturnScan
	default:
		return ProofLossAttestation
	}
}

// OutcomeRequest records one vendor delivery event. VendorEventID is the
// vendor's own event identity: replaying it is idempotent, while reusing
// it for a different outcome is a conflict and is refused.
type OutcomeRequest struct {
	InstructionDigest string
	VendorEventID     string
	Outcome           FulfillmentOutcome
	Proof             ProofKind
	At                time.Time
}

// FallbackWork is the single governed follow-up returned mail creates.
type FallbackWork struct {
	WorkID            string
	InstructionDigest string
	Reason            string
}

// DeliveryOutcome is the recorded result with its evidence digest and,
// for returned mail, its fallback work.
type DeliveryOutcome struct {
	InstructionDigest string
	VendorEventID     string
	Outcome           FulfillmentOutcome
	Proof             ProofKind
	At                time.Time
	Fallback          *FallbackWork
	Digest            string
}

// FulfillmentLedger is the governed, tenant-scoped record of instructions,
// custody chains, outcomes and fallbacks. It never prints, mails or calls
// a provider.
type FulfillmentLedger struct {
	mu           sync.Mutex
	tenantID     string
	instructions map[string]FulfillmentInstruction
	custody      map[string][]CustodyReceipt
	outcomes     map[string][]DeliveryOutcome
	events       map[string]DeliveryOutcome
	fallbacks    map[string]*FallbackWork
}

// NewFulfillmentLedger opens a ledger scoped to one tenant.
func NewFulfillmentLedger(tenantID string) *FulfillmentLedger {
	return &FulfillmentLedger{
		tenantID:     tenantID,
		instructions: make(map[string]FulfillmentInstruction),
		custody:      make(map[string][]CustodyReceipt),
		outcomes:     make(map[string][]DeliveryOutcome),
		events:       make(map[string]DeliveryOutcome),
		fallbacks:    make(map[string]*FallbackWork),
	}
}

// Register pins one instruction in this tenant's ledger.
func (l *FulfillmentLedger) Register(instruction FulfillmentInstruction) error {
	if err := instruction.Validate(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if instruction.TenantID != l.tenantID {
		return fmt.Errorf("%w: instruction tenant %q is outside ledger tenant %q",
			ErrInvalidFulfillment, instruction.TenantID, l.tenantID)
	}
	if _, exists := l.instructions[instruction.Digest]; exists {
		return fmt.Errorf("%w: instruction %q is already registered", ErrFulfillmentState, instruction.InstructionID)
	}
	l.instructions[instruction.Digest] = instruction
	return nil
}

// RecordCustody appends one chain link. The sequence must continue the
// chain exactly: a gap or replay breaks custody and is refused.
func (l *FulfillmentLedger) RecordCustody(receipt CustodyReceipt) error {
	if strings.TrimSpace(receipt.InstructionDigest) == "" || strings.TrimSpace(receipt.VendorID) == "" {
		return fmt.Errorf("%w: instruction digest and vendor are required", ErrInvalidFulfillment)
	}
	if !receipt.Stage.valid() {
		return fmt.Errorf("%w: unsupported custody stage %q", ErrInvalidFulfillment, receipt.Stage)
	}
	if receipt.At.IsZero() {
		return fmt.Errorf("%w: custody timestamp is required", ErrInvalidFulfillment)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.instructions[receipt.InstructionDigest]; !ok {
		return fmt.Errorf("%w: unknown instruction", ErrFulfillmentState)
	}
	chain := l.custody[receipt.InstructionDigest]
	if receipt.Sequence != uint64(len(chain)+1) {
		return fmt.Errorf("%w: custody sequence %d breaks a chain of %d",
			ErrFulfillmentState, receipt.Sequence, len(chain))
	}
	l.custody[receipt.InstructionDigest] = append(chain, receipt)
	return nil
}

// RecordOutcome records one vendor delivery event with idempotent replay:
// the same event id returns the recorded outcome without a second effect,
// while a conflicting reuse is refused.
func (l *FulfillmentLedger) RecordOutcome(req OutcomeRequest) (DeliveryOutcome, error) {
	if strings.TrimSpace(req.InstructionDigest) == "" || strings.TrimSpace(req.VendorEventID) == "" {
		return DeliveryOutcome{}, fmt.Errorf("%w: instruction digest and vendor event are required", ErrInvalidFulfillment)
	}
	if !req.Outcome.valid() {
		return DeliveryOutcome{}, fmt.Errorf("%w: unsupported outcome %q", ErrInvalidFulfillment, req.Outcome)
	}
	if req.Proof != proofFor(req.Outcome) {
		return DeliveryOutcome{}, fmt.Errorf("%w: outcome %q requires proof %q, not %q",
			ErrInvalidFulfillment, req.Outcome, proofFor(req.Outcome), req.Proof)
	}
	if req.At.IsZero() {
		return DeliveryOutcome{}, fmt.Errorf("%w: outcome timestamp is required", ErrInvalidFulfillment)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.instructions[req.InstructionDigest]; !ok {
		return DeliveryOutcome{}, fmt.Errorf("%w: unknown instruction", ErrFulfillmentState)
	}
	if recorded, ok := l.events[req.VendorEventID]; ok {
		if recorded.InstructionDigest != req.InstructionDigest || recorded.Outcome != req.Outcome || recorded.Proof != req.Proof {
			return DeliveryOutcome{}, fmt.Errorf("%w: vendor event %q conflicts with its recording",
				ErrFulfillmentState, req.VendorEventID)
		}
		return recorded, nil
	}
	history := l.outcomes[req.InstructionDigest]
	if len(history) > 0 && history[len(history)-1].Outcome.terminal() {
		return DeliveryOutcome{}, fmt.Errorf("%w: outcome %q follows terminal %q",
			ErrFulfillmentState, req.Outcome, history[len(history)-1].Outcome)
	}
	if !lifecycleAdmits(history, req.Outcome) {
		return DeliveryOutcome{}, fmt.Errorf("%w: outcome %q is out of lifecycle order", ErrFulfillmentState, req.Outcome)
	}
	outcome := DeliveryOutcome{
		InstructionDigest: req.InstructionDigest, VendorEventID: req.VendorEventID,
		Outcome: req.Outcome, Proof: req.Proof, At: req.At.UTC(),
	}
	if req.Outcome == OutcomeReturned {
		if existing, ok := l.fallbacks[req.InstructionDigest]; ok {
			outcome.Fallback = existing
		} else {
			fallback := &FallbackWork{
				WorkID:            "fallback:" + req.InstructionDigest,
				InstructionDigest: req.InstructionDigest,
				Reason:            "returned mail: governed redelivery review",
			}
			l.fallbacks[req.InstructionDigest] = fallback
			outcome.Fallback = fallback
		}
	} else if existing, ok := l.fallbacks[req.InstructionDigest]; ok {
		outcome.Fallback = existing
	}
	outcome.Digest = outcome.computeDigest()
	l.outcomes[req.InstructionDigest] = append(history, outcome)
	l.events[req.VendorEventID] = outcome
	return outcome, nil
}

// lifecycleAdmits enforces the governed order PRINTED, MAILED, IN_TRANSIT
// and then DELIVERED, with RETURNED admissible from MAILED or IN_TRANSIT
// and UNKNOWN admissible anywhere as an explicit unknown, never a guess.
func lifecycleAdmits(history []DeliveryOutcome, next FulfillmentOutcome) bool {
	if len(history) == 0 {
		return next == OutcomePrinted || next == OutcomeUnknown
	}
	last := history[len(history)-1].Outcome
	if next == OutcomeUnknown {
		return true
	}
	rank := map[FulfillmentOutcome]int{
		OutcomePrinted: 0, OutcomeMailed: 1, OutcomeInTransit: 2, OutcomeDelivered: 3,
	}
	if next == OutcomeReturned {
		return last == OutcomeMailed || last == OutcomeInTransit
	}
	current, ok := rank[last]
	target, known := rank[next]
	if !ok || !known {
		return false
	}
	return target == current+1
}

func (o DeliveryOutcome) computeDigest() string {
	sum := sha256.New()
	fmt.Fprintf(sum, "%s\x00%s\x00%s\x00%s\x00%s",
		o.InstructionDigest, o.VendorEventID, o.Outcome, o.Proof, o.At.UTC().Format(time.RFC3339))
	if o.Fallback != nil {
		sum.Write([]byte(o.Fallback.WorkID + "\x00"))
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

// Outcomes returns the recorded outcomes for one instruction in order.
func (l *FulfillmentLedger) Outcomes(instructionDigest string) []DeliveryOutcome {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]DeliveryOutcome(nil), l.outcomes[instructionDigest]...)
}

// InstructionDigests returns the registered instruction digests, sorted.
func (l *FulfillmentLedger) InstructionDigests() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	digests := make([]string, 0, len(l.instructions))
	for digest := range l.instructions {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	return digests
}
