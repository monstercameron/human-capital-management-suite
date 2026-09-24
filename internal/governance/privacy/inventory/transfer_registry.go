package inventory

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed transfer-policy.v2026-09.json
var transferPolicyJSON []byte

type TransferRegime string

const (
	TransferRegimeEU TransferRegime = "EU_GDPR"
	TransferRegimeUK TransferRegime = "UK_GDPR"
)

type TransferMechanism struct {
	ID      string         `json:"id"`
	Regime  TransferRegime `json:"regime"`
	Kind    string         `json:"kind"`
	Version string         `json:"version"`
	Module  string         `json:"module,omitempty"`
}

type TransferAdequacy struct {
	Regime          TransferRegime `json:"regime"`
	Region          string         `json:"region"`
	DecisionRef     string         `json:"decision_ref"`
	DecisionVersion string         `json:"decision_version"`
	Scope           string         `json:"scope"`
}

type TransferRegionGroup struct {
	ID      string   `json:"id"`
	Regions []string `json:"regions"`
}

// TransferMechanismDocument binds a governed, immutable contract document to
// the parties and transfer scope for which counsel approved its use.
type TransferMechanismDocument struct {
	MechanismID     string            `json:"mechanism_id"`
	DocumentRef     string            `json:"document_ref"`
	DocumentVersion string            `json:"document_version"`
	Exporter        string            `json:"exporter"`
	ExporterRole    TransferPartyRole `json:"exporter_role"`
	Importer        string            `json:"importer"`
	ImporterRole    TransferPartyRole `json:"importer_role"`
	Regions         []string          `json:"regions"`
}

// TransferRulePack is governed, versioned policy input. A populated adequacy
// list is not a claim of completeness; unknown destinations fail closed.
type TransferRulePack struct {
	Version                  string                      `json:"version"`
	EffectiveFrom            time.Time                   `json:"effective_from"`
	ReviewState              string                      `json:"review_state"`
	ApprovalRef              string                      `json:"approval_ref"`
	ApprovedBy               string                      `json:"approved_by"`
	ApprovedAt               time.Time                   `json:"approved_at"`
	AdequacyRegistryComplete bool                        `json:"adequacy_registry_complete"`
	Sources                  []string                    `json:"sources"`
	Adequacy                 []TransferAdequacy          `json:"adequacy"`
	Mechanisms               []TransferMechanism         `json:"mechanisms"`
	RegionGroups             []TransferRegionGroup       `json:"region_groups"`
	MechanismDocuments       []TransferMechanismDocument `json:"mechanism_documents,omitempty"`
}

type TransferAssessmentStatus string
type TransferAssessmentOutcome string
type TransferAssessmentKind string

const (
	TransferAssessmentApproved TransferAssessmentStatus  = "APPROVED"
	TransferOutcomePermitted   TransferAssessmentOutcome = "TRANSFER_PERMITTED"
	TransferAssessmentEUTIA    TransferAssessmentKind    = "EU_TIA"
	TransferAssessmentUKTest   TransferAssessmentKind    = "UK_DATA_PROTECTION_TEST"
)

// TransferImpactAssessment is evidence metadata. The referenced assessment
// itself must be maintained by the accountable privacy team; this record does
// not independently establish that a transfer is lawful.
type TransferImpactAssessment struct {
	ID              string                    `json:"id"`
	Version         string                    `json:"version"`
	RulePackVersion string                    `json:"rule_pack_version"`
	Regime          TransferRegime            `json:"regime"`
	AssessmentKind  TransferAssessmentKind    `json:"assessment_kind"`
	Processor       string                    `json:"processor"`
	Region          string                    `json:"region"`
	Status          TransferAssessmentStatus  `json:"status"`
	Outcome         TransferAssessmentOutcome `json:"outcome"`
	EffectiveFrom   time.Time                 `json:"effective_from"`
	EffectiveTo     time.Time                 `json:"effective_to,omitempty"`
}

// CurrentTransferRulePack loads the bundled, immutable default legal-rule
// snapshot. Operators should pin a version into each approved flow and update
// the snapshot only through the normal legal-pack governance process.
func CurrentTransferRulePack() (TransferRulePack, error) {
	var pack TransferRulePack
	if err := json.Unmarshal(transferPolicyJSON, &pack); err != nil {
		return TransferRulePack{}, fmt.Errorf("%w: transfer rule pack decode: %v", ErrInvalid, err)
	}
	if err := pack.Validate(); err != nil {
		return TransferRulePack{}, err
	}
	return pack, nil
}

func (p TransferRulePack) Validate() error {
	if strings.TrimSpace(p.Version) == "" || p.EffectiveFrom.IsZero() || strings.TrimSpace(p.ReviewState) == "" {
		return fmt.Errorf("%w: transfer rule pack needs version, effective date, and review state", ErrInvalid)
	}
	if p.ReviewState == "APPROVED" {
		if strings.TrimSpace(p.ApprovalRef) == "" || strings.TrimSpace(p.ApprovedBy) == "" || p.ApprovedAt.IsZero() || p.ApprovedAt.Before(p.EffectiveFrom) {
			return fmt.Errorf("%w: approved transfer rule pack needs dated approver and approval reference", ErrInvalid)
		}
	} else if p.ReviewState != "REFERENCE_SNAPSHOT_REQUIRES_COUNSEL_APPROVAL" || p.ApprovalRef != "" || p.ApprovedBy != "" || !p.ApprovedAt.IsZero() {
		return fmt.Errorf("%w: unsupported transfer rule pack review state %q", ErrInvalid, p.ReviewState)
	}
	if len(p.Sources) == 0 || len(p.Mechanisms) == 0 || len(p.RegionGroups) == 0 {
		return fmt.Errorf("%w: transfer rule pack needs sources, mechanisms, and region groups", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, m := range p.Mechanisms {
		if !recognizedMechanism(m) {
			return fmt.Errorf("%w: transfer rule pack has incomplete mechanism", ErrInvalid)
		}
		if seen[m.ID] {
			return fmt.Errorf("%w: duplicate transfer mechanism %q", ErrInvalid, m.ID)
		}
		seen[m.ID] = true
	}
	for _, a := range p.Adequacy {
		if a.Region == "" || a.DecisionRef == "" || a.DecisionVersion == "" || a.Scope == "" || (a.Regime != TransferRegimeEU && a.Regime != TransferRegimeUK) {
			return fmt.Errorf("%w: transfer rule pack has incomplete adequacy entry", ErrInvalid)
		}
	}
	groups := map[string]bool{}
	for _, group := range p.RegionGroups {
		if group.ID == "" || len(group.Regions) == 0 || groups[group.ID] {
			return fmt.Errorf("%w: transfer rule pack has invalid region group", ErrInvalid)
		}
		groups[group.ID] = true
		regions := map[string]bool{}
		for _, region := range group.Regions {
			if strings.TrimSpace(region) == "" || regions[strings.ToUpper(region)] {
				return fmt.Errorf("%w: transfer rule pack has invalid region group member", ErrInvalid)
			}
			regions[strings.ToUpper(region)] = true
		}
	}
	for _, entry := range p.Adequacy {
		if !groups[entry.Region] {
			return fmt.Errorf("%w: adequacy entry %q refers to unknown region group %q", ErrInvalid, entry.DecisionRef, entry.Region)
		}
	}
	documents := map[string]bool{}
	mechanisms := map[string]bool{}
	for _, mechanism := range p.Mechanisms {
		mechanisms[mechanism.ID] = true
	}
	for _, document := range p.MechanismDocuments {
		key := document.MechanismID + "\x00" + document.DocumentRef + "\x00" + document.DocumentVersion
		if document.MechanismID == "" || !mechanisms[document.MechanismID] || document.DocumentRef == "" || document.DocumentVersion == "" || document.Exporter == "" || document.Importer == "" || len(document.Regions) == 0 || documents[key] {
			return fmt.Errorf("%w: invalid or duplicate governed mechanism document", ErrInvalid)
		}
		if (document.ExporterRole != TransferPartyController && document.ExporterRole != TransferPartyProcessor) || (document.ImporterRole != TransferPartyController && document.ImporterRole != TransferPartyProcessor) {
			return fmt.Errorf("%w: governed mechanism document has invalid party roles", ErrInvalid)
		}
		documents[key] = true
	}
	return nil
}

// ValidateTransfers validates every flow subject to EU or UK transfer rules.
// The assessment date is explicit so callers can make reproducible decisions.
func (i Inventory) ValidateTransfers(pack TransferRulePack, asOf time.Time) error {
	if err := pack.Validate(); err != nil {
		return err
	}
	if err := validateTrustedTransferPack(pack); err != nil {
		return err
	}
	if asOf.IsZero() || asOf.Before(pack.EffectiveFrom) {
		return fmt.Errorf("%w: transfer rule pack is not effective at validation time", ErrInvalid)
	}
	if pack.ReviewState == "APPROVED" && asOf.Before(pack.ApprovedAt) {
		return fmt.Errorf("%w: transfer rule pack was not approved at validation time", ErrInvalid)
	}
	activities := make(map[string]ProcessingActivity, len(i.Activities))
	for _, activity := range i.Activities {
		activities[activity.ID+"@"+activity.Version] = activity
	}
	assessments := make(map[string]TransferImpactAssessment, len(i.TransferImpactAssessments))
	for _, assessment := range i.TransferImpactAssessments {
		if assessment.ID == "" || assessment.Version == "" || assessment.RulePackVersion == "" || assessment.Processor == "" || assessment.Region == "" || !validAssessmentKind(assessment.Regime, assessment.AssessmentKind) || (assessment.Status != TransferAssessmentApproved && assessment.Status != "RETIRED") || (assessment.Outcome != TransferOutcomePermitted && assessment.Outcome != "TRANSFER_BLOCKED") || assessment.EffectiveFrom.IsZero() || (!assessment.EffectiveTo.IsZero() && !assessment.EffectiveFrom.Before(assessment.EffectiveTo)) {
			return fmt.Errorf("%w: transfer impact assessment has incomplete identity or scope", ErrInvalid)
		}
		if _, exists := assessments[assessment.ID]; exists {
			return fmt.Errorf("%w: duplicate transfer impact assessment %q", ErrInvalid, assessment.ID)
		}
		assessments[assessment.ID] = assessment
	}
	for _, activity := range i.Activities {
		seen := map[string]bool{}
		for _, ref := range activity.TransferAssessmentRefs {
			if seen[ref] {
				return fmt.Errorf("%w: activity %q duplicates transfer assessment reference %q", ErrInvalid, activity.ID, ref)
			}
			seen[ref] = true
			if _, ok := assessments[ref]; !ok {
				return fmt.Errorf("%w: activity %q transfer assessment reference %q does not resolve", ErrInvalid, activity.ID, ref)
			}
		}
	}
	for _, flow := range i.Flows {
		activity, ok := activities[flow.ActivityID+"@"+flow.Version]
		if !ok {
			return fmt.Errorf("%w: flow %q has no versioned activity", ErrInvalid, flow.ID)
		}
		if err := flow.validateStructure(activity); err != nil {
			return err
		}
		if err := flow.ValidateTransfer(activity, i.TransferImpactAssessments, pack, asOf); err != nil {
			return err
		}
	}
	return nil
}

// ValidateTransfer performs transfer-specific checks after ordinary flow
// scope validation. SourceRegion EU/EEA and UK activate the corresponding
// regime; callers must list any additional applicable regime explicitly.
func (f ProcessingDataFlow) ValidateTransfer(a ProcessingActivity, assessments []TransferImpactAssessment, pack TransferRulePack, asOf time.Time) error {
	if err := pack.Validate(); err != nil {
		return err
	}
	if err := validateTrustedTransferPack(pack); err != nil {
		return err
	}
	if asOf.IsZero() || asOf.Before(pack.EffectiveFrom) || (pack.ReviewState == "APPROVED" && asOf.Before(pack.ApprovedAt)) {
		return fmt.Errorf("%w: transfer rule pack is not approved and effective at validation time", ErrInvalid)
	}
	if err := required("source_region", f.SourceRegion); err != nil {
		return err
	}
	if f.TransferPolicyVersion != pack.Version {
		return fmt.Errorf("%w: flow %q transfer policy version %q does not match current %q", ErrInvalid, f.ID, f.TransferPolicyVersion, pack.Version)
	}
	regimes := append([]TransferRegime(nil), f.TransferRegimes...)
	sourceRegime := sourceTransferRegime(pack, f.SourceRegion)
	if sourceRegime != "" && !containsRegime(regimes, sourceRegime) {
		regimes = append(regimes, sourceRegime)
	}
	if len(regimes) == 0 && hasDifferentTransferRegion(f.SourceRegion, f.TransferRegions) {
		// Unknown exporter scope does not silently exempt a cross-region flow.
		// Validate under both supported regimes until a governed applicability
		// decision adds a narrower explicit regime set to the flow.
		regimes = []TransferRegime{TransferRegimeEU, TransferRegimeUK}
	}
	if len(regimes) == 0 {
		return nil
	}
	mechanisms := make(map[string]TransferMechanism, len(pack.Mechanisms))
	for _, mechanism := range pack.Mechanisms {
		mechanisms[mechanism.ID] = mechanism
	}
	assessmentByID := make(map[string]TransferImpactAssessment, len(assessments))
	for _, assessment := range assessments {
		assessmentByID[assessment.ID] = assessment
	}
	for _, ref := range f.Safeguards {
		if _, ok := mechanisms[ref]; !ok {
			return fmt.Errorf("%w: flow %q safeguard %q is not in transfer mechanism registry %s", ErrInvalid, f.ID, ref, pack.Version)
		}
	}
	for _, regime := range regimes {
		if regime != TransferRegimeEU && regime != TransferRegimeUK {
			return fmt.Errorf("%w: flow %q has unsupported transfer regime %q", ErrInvalid, f.ID, regime)
		}
		for _, region := range f.TransferRegions {
			if sameTransferZone(pack, regime, f.SourceRegion, region) {
				continue
			}
			if isAdequate(pack, regime, region) {
				if pack.ReviewState != "APPROVED" {
					return fmt.Errorf("%w: flow %q relies on unapproved adequacy entry for region %q in transfer rule pack %s", ErrInvalid, f.ID, region, pack.Version)
				}
				continue
			}
			if pack.ReviewState != "APPROVED" {
				return fmt.Errorf("%w: flow %q to non-adequate region %q is blocked until transfer rule pack %s receives counsel approval", ErrInvalid, f.ID, region, pack.Version)
			}
			if err := validateTransferParties(a, f); err != nil {
				return err
			}
			mechanism, ok := findMechanism(mechanisms, f, regime)
			if !ok {
				return fmt.Errorf("%w: flow %q to non-adequate region %q has no role-compatible recognized %s mechanism", ErrInvalid, f.ID, region, regime)
			}
			if !hasMechanismEvidence(pack, f, mechanism, region) {
				return fmt.Errorf("%w: flow %q mechanism %q lacks a versioned contract document reference", ErrInvalid, f.ID, mechanism.ID)
			}
			processor := f.Processor
			if f.ImporterRole == TransferPartyProcessor {
				processor = f.Importer
			} else if strings.TrimSpace(f.Subprocessor) != "" {
				processor = f.Subprocessor
			}
			if !hasCurrentAssessment(assessmentByID, a.TransferAssessmentRefs, regime, processor, region, pack.Version, asOf) {
				return fmt.Errorf("%w: flow %q to non-adequate region %q lacks a current %s assessment for processor %q", ErrInvalid, f.ID, region, regime, processor)
			}
		}
	}
	return nil
}

func validateTrustedTransferPack(pack TransferRulePack) error {
	trusted, err := CurrentTransferRulePack()
	if err != nil {
		return err
	}
	providedJSON, err := json.Marshal(pack)
	if err != nil {
		return fmt.Errorf("%w: transfer policy serialization failed", ErrInvalid)
	}
	trustedJSON, err := json.Marshal(trusted)
	if err != nil {
		return fmt.Errorf("%w: trusted transfer policy serialization failed", ErrInvalid)
	}
	if !bytes.Equal(providedJSON, trustedJSON) {
		return fmt.Errorf("%w: transfer policy must match the embedded governed snapshot; caller-supplied approval metadata is not trusted", ErrInvalid)
	}
	return nil
}

func recognizedMechanism(m TransferMechanism) bool {
	if m.ID == "" || m.Version == "" || (m.Regime != TransferRegimeEU && m.Regime != TransferRegimeUK) {
		return false
	}
	switch m.Kind {
	case "SCC":
		return m.Regime == TransferRegimeEU && m.Version == "2021/914" && (m.Module == "1" || m.Module == "2" || m.Module == "3" || m.Module == "4")
	case "BCR", "ADEQUACY", "DEROGATION":
		return m.Module == ""
	case "IDTA":
		return m.Regime == TransferRegimeUK && m.Module == ""
	case "UK_ADDENDUM":
		return m.Regime == TransferRegimeUK && m.Version == "current/v1" && (m.Module == "1" || m.Module == "2" || m.Module == "3" || m.Module == "4")
	default:
		return false
	}
}

func containsRegime(values []TransferRegime, want TransferRegime) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func isAdequate(pack TransferRulePack, regime TransferRegime, region string) bool {
	for _, entry := range pack.Adequacy {
		if entry.Regime == regime && (strings.EqualFold(entry.Region, region) || inRegionGroup(pack, entry.Region, region)) {
			return true
		}
	}
	return false
}

func findMechanism(registry map[string]TransferMechanism, f ProcessingDataFlow, regime TransferRegime) (TransferMechanism, bool) {
	for _, ref := range f.Safeguards {
		mechanism, ok := registry[ref]
		if ok && mechanism.Regime == regime && mechanism.Kind != "ADEQUACY" && mechanismRolesMatch(mechanism, f.ExporterRole, f.ImporterRole) {
			return mechanism, true
		}
	}
	return TransferMechanism{}, false
}

func hasMechanism(registry map[string]TransferMechanism, refs []string, regime TransferRegime) bool {
	for _, ref := range refs {
		if mechanism, ok := registry[ref]; ok && mechanism.Regime == regime && mechanism.Kind != "ADEQUACY" {
			return true
		}
	}
	return false
}

func mechanismRolesMatch(m TransferMechanism, exporter, importer TransferPartyRole) bool {
	if m.Kind != "SCC" && m.Kind != "UK_ADDENDUM" {
		return true
	}
	switch m.Module {
	case "1":
		return exporter == TransferPartyController && importer == TransferPartyController
	case "2":
		return exporter == TransferPartyController && importer == TransferPartyProcessor
	case "3":
		return exporter == TransferPartyProcessor && importer == TransferPartyProcessor
	case "4":
		return exporter == TransferPartyProcessor && importer == TransferPartyController
	default:
		return false
	}
}

func validateTransferParties(a ProcessingActivity, f ProcessingDataFlow) error {
	if f.Exporter == "" || f.Importer == "" || f.Importer != f.Recipient {
		return fmt.Errorf("%w: flow %q needs exporter/importer identities and importer must match recipient", ErrInvalid, f.ID)
	}
	if f.ExporterRole != TransferPartyController && f.ExporterRole != TransferPartyProcessor || f.ImporterRole != TransferPartyController && f.ImporterRole != TransferPartyProcessor {
		return fmt.Errorf("%w: flow %q needs supported exporter and importer roles", ErrInvalid, f.ID)
	}
	if authoritativePartyRole(a, f.Exporter) != f.ExporterRole || authoritativePartyRole(a, f.Importer) != f.ImporterRole {
		return fmt.Errorf("%w: flow %q exporter/importer identities and roles do not match activity %q", ErrInvalid, f.ID, a.ID)
	}
	return nil
}

func authoritativePartyRole(a ProcessingActivity, party string) TransferPartyRole {
	if party == a.Controller {
		return TransferPartyController
	}
	if party == a.Processor || contains(a.Subprocessors, party) {
		return TransferPartyProcessor
	}
	return ""
}

func hasMechanismEvidence(pack TransferRulePack, f ProcessingDataFlow, mechanism TransferMechanism, region string) bool {
	for _, evidence := range f.MechanismEvidence {
		if evidence.MechanismID != mechanism.ID || evidence.DocumentRef == "" || evidence.DocumentVersion == "" || !contains(f.ContractRefs, evidence.DocumentRef) {
			continue
		}
		for _, governed := range pack.MechanismDocuments {
			if governed.MechanismID == evidence.MechanismID && governed.DocumentRef == evidence.DocumentRef && governed.DocumentVersion == evidence.DocumentVersion && governed.Exporter == f.Exporter && governed.ExporterRole == f.ExporterRole && governed.Importer == f.Importer && governed.ImporterRole == f.ImporterRole && contains(governed.Regions, region) {
				return true
			}
		}
	}
	return false
}

func sourceTransferRegime(pack TransferRulePack, source string) TransferRegime {
	if inRegionGroup(pack, "EU", source) || inRegionGroup(pack, "EEA", source) {
		return TransferRegimeEU
	}
	if inRegionGroup(pack, "UK", source) {
		return TransferRegimeUK
	}
	return ""
}

func hasDifferentTransferRegion(source string, destinations []string) bool {
	for _, destination := range destinations {
		if !strings.EqualFold(strings.TrimSpace(source), strings.TrimSpace(destination)) {
			return true
		}
	}
	return false
}

func sameTransferZone(pack TransferRulePack, regime TransferRegime, source, destination string) bool {
	if regime == TransferRegimeEU {
		return (inRegionGroup(pack, "EEA", source) || strings.EqualFold(source, "EU")) && (inRegionGroup(pack, "EEA", destination) || strings.EqualFold(destination, "EU"))
	}
	if regime == TransferRegimeUK {
		return inRegionGroup(pack, "UK", source) && inRegionGroup(pack, "UK", destination)
	}
	return false
}

func inRegionGroup(pack TransferRulePack, groupID, region string) bool {
	for _, group := range pack.RegionGroups {
		if group.ID != groupID {
			continue
		}
		for _, member := range group.Regions {
			if strings.EqualFold(member, region) {
				return true
			}
		}
	}
	return false
}

func hasCurrentAssessment(records map[string]TransferImpactAssessment, refs []string, regime TransferRegime, processor, region, rulePack string, asOf time.Time) bool {
	wantKind := TransferAssessmentEUTIA
	if regime == TransferRegimeUK {
		wantKind = TransferAssessmentUKTest
	}
	for _, ref := range refs {
		record, ok := records[ref]
		if !ok || record.Regime != regime || record.AssessmentKind != wantKind || record.Processor != processor || !strings.EqualFold(record.Region, region) || record.RulePackVersion != rulePack || record.Status != TransferAssessmentApproved || record.Outcome != TransferOutcomePermitted || record.EffectiveFrom.IsZero() || asOf.Before(record.EffectiveFrom) || (!record.EffectiveTo.IsZero() && !asOf.Before(record.EffectiveTo)) {
			continue
		}
		return true
	}
	return false
}

func validAssessmentKind(regime TransferRegime, kind TransferAssessmentKind) bool {
	return (regime == TransferRegimeEU && kind == TransferAssessmentEUTIA) || (regime == TransferRegimeUK && kind == TransferAssessmentUKTest)
}
