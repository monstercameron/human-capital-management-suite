package storeprivacy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

// This file owns the WF-REV-014 field-classification policy: which fields
// written to a PERMANENT table count as personal, and therefore must move
// to payload-vault references under per-subject keys (WF-REV-015) instead
// of sitting inline where no erasure request can reach them.
//
// Classification precedence for one column:
//  1. Declared vault reference (vaultRefs): personal, stored as vault_ref.
//     Honored only when the registry records FIELD_LEVEL encryption;
//     otherwise Assemble reports VAULT_UNPROVEN.
//  2. Explicit non-personal override (global column name or table.column).
//  3. Subject-link pattern: a reference that resolves to a subject record
//     (grievant_ref, membership_id) stays personal despite its suffix.
//  4. Reference suffix (_id, _ref, _digest, ...): an opaque pointer to
//     another record carries no subject content by itself.
//  5. Reviewed payload verdict (payloadVerdicts, any column).
//  6. Personal pattern (global substring or exact table.column).
//  7. Opaque envelope or payload-capable type with no rule: UNREVIEWED,
//     which fails closed as PAYLOAD_UNDECLARED.
//  8. Anything else: NON_PERSONAL scalar.
//
// Deliberate scope: amount and rate columns (salary figures, accrual
// balances, tax amounts) are business facts under legal retention and are
// not flagged; the flagged surface is identity, contact, subject content
// and account/destination identifiers. Presentation-only fields (locale,
// timezone, currency, theme) are never personal. Cryptographic digests are
// not recoverable content and are non-personal; the finding surface is
// what erasure must reach, not what merely links to it.
//
// Decision provenance (approved_by, decided_by, reviewer, approver,
// authority, actor, produced_by) is the minimally necessary tombstone the
// secure-deletion contract keeps: who authorized what is evidence, not
// subject content. The subject side of the same rows (worker_ref,
// subject_ref, participants, payloads) is flagged, so no finding is lost,
// only attributed to the right field.

// subjectLinkSubstrings match references that resolve to a subject
// record. They stay personal despite a reference suffix: the link itself
// identifies whose data the immutable row is about.
var subjectLinkSubstrings = []struct {
	substr string
	reason string
}{
	{"participant", "named participant"},
	{"member", "named member"},
	{"candidate", "named candidate"},
	{"attester", "named attester"},
	{"signer", "named signatory"},
	{"assignee", "named assignee"},
	{"grievant", "named grievant"},
	{"respondent", "named respondent"},
	{"witness", "named witness"},
	{"interviewee", "named interviewee"},
	{"claimant", "named claimant"},
	{"subject", "named subject"},
	{"emergency", "emergency contact"},
	{"dependent", "family member"},
	{"beneficiar", "beneficiary"},
	{"household", "family member"},
	{"voter", "named voter"},
	{"bank", "financial account reference"},
	{"contact", "contact detail"},
	{"consent", "consent record"},
	{"address", "postal address"},
	{"email", "contact address"},
	{"phone", "contact number"},
	{"identity", "identity record"},
	{"person", "person record"},
	{"worker", "worker record"},
	{"patient", "patient record"},
}

// referenceSuffixes mark opaque pointers to other records. A UUID, digest,
// version pin or status code carries no subject content by itself, so it is
// non-personal unless a subject-link pattern above already matched.
var referenceSuffixes = []string{
	"_id", "_ids", "_ref", "_refs", "_digest", "_version", "_code",
	"_index", "_key", "_keys", "_seq", "_no", "_uuid", "_revision",
}

// sensitiveSubstrings match credential and account material that must beat
// the reference-suffix rule: a stored secret or account number is
// recoverable content, not an opaque pointer.
var sensitiveSubstrings = []struct {
	substr string
	reason string
}{
	{"password", "credential material"},
	{"secret", "credential material"},
	{"credential", "credential material"},
	{"token", "authentication token"},
	{"private_key", "credential material"},
	{"api_key", "credential material"},
	{"pin_code", "credential material"},
	{"security_answer", "credential material"},
	{"account_number", "financial account reference"},
	{"card_number", "financial account reference"},
	{"routing_number", "financial account reference"},
	{"national_number", "government identifier"},
	{"identity_number", "government identifier"},
	{"national_id", "government identifier"},
	{"tax_id", "government identifier"},
	{"taxpayer", "government identifier"},
	{"driver_licen", "government identifier"},
	{"iban", "financial account reference"},
	{"ssn", "government identifier"},
	{"passport", "government identifier"},
}

// personalSubstrings matches personal content by column-name substring.
// Each entry carries the audit reason recorded on the finding.
var personalSubstrings = []struct {
	substr string
	reason string
}{
	// Direct identity.
	{"first_name", "given name"},
	{"last_name", "family name"},
	{"middle_name", "given name"},
	{"given_name", "given name"},
	{"family_name", "family name"},
	{"full_name", "person name"},
	{"maiden_name", "person name"},
	{"surname", "person name"},
	{"preferred_name", "person name"},
	{"legal_name", "person name"},
	{"registered_name", "registered name"},
	{"display_name", "person name"},
	{"nickname", "person name"},
	{"screen_name", "person name"},
	{"username", "account identifier"},
	// Contact and address.
	{"email", "contact address"},
	{"phone", "contact number"},
	{"fax", "contact number"},
	{"address", "postal address"},
	{"postal", "postal address"},
	{"zip_code", "postal address"},
	{"zipcode", "postal address"},
	{"contact", "contact detail"},
	// Demographic and sensitive.
	{"birth", "date of birth"},
	{"dob", "date of birth"},
	{"gender", "demographic attribute"},
	{"nationality", "demographic attribute"},
	{"citizenship", "demographic attribute"},
	{"photo", "image of subject"},
	{"biometric", "biometric identifier"},
	{"disability", "health attribute"},
	{"emergency", "emergency contact"},
	{"marital", "marital status"},
	{"dependent", "family member"},
	{"beneficiar", "beneficiary"},
	{"household", "family member"},
	// Credentials, account numbers and government identifiers beat the
	// reference-suffix rule via sensitiveSubstrings above; only the
	// non-suffixed forms remain here.
	{"bank", "financial account reference"},
	// Free-text prose in an immutable row routinely names subjects or
	// describes their circumstances; erasure cannot reach it inline.
	{"narrative", "free-text account"},
	{"transcript", "free-text account"},
	{"testimony", "free-text account"},
	{"allegation", "free-text account"},
	{"grievance", "free-text account"},
	{"statement", "free-text account"},
	{"justification", "free-text rationale"},
	{"explanation", "free-text rationale"},
	{"wording", "free-text content"},
	{"comment", "free-text content"},
	{"remark", "free-text content"},
	{"feedback", "free-text content"},
	{"finding", "free-text finding"},
	{"observed", "free-text observation"},
	{"evidence", "evidentiary content"},
	{"summary", "free-text content"},
	{"message", "message content"},
	{"answer", "subject-provided answer"},
	{"memo", "free-text content"},
	{"detail", "free-text detail"},
	{"reason", "free-text rationale"},
	{"note", "free-text note"},
	{"description", "free-text description"},
	// Subject-content envelopes: the column name itself says the value is
	// about people, even when the bytes are opaque.
	{"participant", "named participant"},
	{"member", "named member"},
	{"claim", "subject claim"},
	{"attester", "named attester"},
	{"signer", "named signatory"},
	{"audience", "named audience"},
	{"candidate", "named candidate"},
	{"field_value", "subject-supplied values"},
	{"variable_value", "workflow subject data"},
	{"snapshot", "subject data snapshot"},
	{"profile", "subject profile"},
	{"preference", "subject preference"},
	{"relocation", "relocation detail"},
	{"immigration", "immigration detail"},
	{"assignee", "named assignee"},
	{"claimant", "named claimant"},
	{"grievant", "named grievant"},
	{"respondent", "named respondent"},
	{"witness", "named witness"},
	{"interviewee", "named interviewee"},
	{"vote", "ballot content"},
	{"ballot", "ballot content"},
	{"declaration", "declared subject content"},
	{"consent", "consent record"},
	{"ciphertext", "encrypted subject data"},
}

// nonPersonalTableColumns are table-scoped exceptions: the column name is
// personal elsewhere but this table's column provably is not.
var nonPersonalTableColumns = map[string]string{
	// Concurrency fence tokens, not authentication material.
	"connector_operation_attempt.fence_token": "concurrency fence token",
	"connector_operation_journal.fence_token": "concurrency fence token",
	// Opaque pointers to credential leases held elsewhere.
	"connector_operation_credential_lease.credential_lease_ref": "credential lease pointer",
	"connector_operation_journal.credential_lease_ref":          "credential lease pointer",
	// Claim type labels (EMAIL_VERIFIED and the like), not subject claims.
	"identity_claim.claim_type": "claim type label",
	// The evidentiary standard applied (PREPONDERANCE and the like).
	"er_finding_revision.evidence_standard": "evidentiary standard label",
	// Issuer claim mappings are connector configuration.
	"issuer_profile.claim_mappings": "issuer claim mapping configuration",
	// OIDC audience: the relying-party service, not people.
	"issuer_profile.audience": "relying-party audience",
	// Survey question-bank identifier: content, not a bank account.
	"survey_question_bank_revision.bank_id": "survey question bank identifier",
	// Concurrency and idempotency tokens, not authentication material.
	"transaction_plan.conflict_fence_token": "concurrency fence token",
	"workflow_signal.dedupe_token":          "signal dedupe token",
	// Index into the secret store: selects a credential, is not one.
	"integration_provider_receipt.secret_index": "secret store index",
	// Claim timestamp, not claim content; the claimant link beside it is
	// still flagged.
	"work_item_claim.claimed_at": "claim timestamp",
	// Reference dataset members are dataset records, not subjects.
	"reference_dataset_release.members": "reference dataset members",
	// Disclaimer prose governs the scenario; it states no subject claim.
	"scenario_revision.authority_disclaimer": "scenario disclaimer text",
	// Kind labels (PERSON/ORGANIZATION), not the subject.
	"search_projection_event.subject_kind": "subject kind label",
	"proposal_write_item.subject_kind":     "subject kind label",
	// Reference to a job profile revision: an opaque version pin.
	"target_role_revision.job_profile_revision": "job profile revision reference",
	// Boolean protection flags, not member links.
	"survey_campaign_sample.membership_protected": "sample protection flag",
	"survey_launch_record.membership_protected":   "sample protection flag",
	// Snapshot bookkeeping: why the snapshot was taken and its order.
	"intent_input_snapshot.snapshot_purpose":  "snapshot purpose label",
	"intent_input_snapshot.snapshot_sequence": "snapshot order",
	// Assurance level (IAL2 and the like), not an identity record.
	"signature.identity_assurance": "identity assurance level",
	// Evidence kind labels, not evidence content.
	"jit_evidence_record.evidence_kind":   "evidence kind label",
	"worker_skill_evidence.evidence_kind": "evidence kind label",
	// Tenant authorization evidence is control documentation, not
	// subject data.
	"government_authorization_profile.evidence": "authorization evidence package",
	// Article audience scopes name roles and departments, not people.
	"knowledge_article_revision.audience_scope": "article audience scope",
}

// nonPersonalColumns are global column names (or table.column pairs) that
// look personal but provably are not. Each carries the reason so the
// exception is reviewable.
var nonPersonalColumns = map[string]string{
	// Operational vocabulary that merely contains a flagged substring.
	"namespace":         "tenant/object namespace, not a person name",
	"health":            "endpoint health state, not a health attribute",
	"last_error":        "operational error text, not a subject",
	"migration_name":    "schema migration label",
	"table_name":        "catalog table label",
	"filename":          "artifact file label",
	"hostname":          "machine label",
	"question_bank_ref": "survey content reference, not a bank account",
	// Bare entity labels: benefit plans, bargaining units, accumulators
	// and the like are named configurations, not people. Person-domain
	// names match the dedicated given/family/legal/preferred patterns.
	"name": "entity label",
	// Observation timestamps record when evidence was taken, not whom it
	// is about; the observed_state content beside them is flagged.
	"observed_at": "observation timestamp",
	// Public-key and signature material identifies no subject.
	"issuer_key":       "issuer public key material",
	"signature":        "cryptographic signature bytes",
	"jwks_pinned_keys": "pinned issuer public keys",
	// Policy, rule and reference content governs processing; it is not
	// about an identifiable subject.
	"questions":                    "survey question text, not responses",
	"dimensions":                   "aggregation dimensions",
	"cap_policy":                   "accumulator policy",
	"floor_policy":                 "accumulator policy",
	"expiry_policy":                "accumulator policy",
	"rollover_policy":              "accumulator policy",
	"clock":                        "reportability clock rules",
	"graph":                        "calibration graph structure",
	"theme":                        "presentation theme",
	"rules":                        "jurisdiction rule content",
	"counts":                       "reconciliation counts",
	"manifest":                     "structural manifest",
	"filter":                       "subscription filter expression",
	"mappings":                     "field mapping rules",
	"lineage":                      "version lineage references",
	"validator_results":            "schema validation results",
	"parameter_schema":             "template parameter schema",
	"field_rules":                  "mapping rules",
	"guidelines":                   "cycle guidelines",
	"contents":                     "pack contents listing",
	"packs":                        "pack references",
	"entries":                      "schedule entries",
	"criteria":                     "pool selection criteria",
	"removal_policy":               "pool removal policy",
	"review":                       "review workflow state",
	"source_refs":                  "source references",
	"supersession":                 "version supersession links",
	"policies":                     "policy documents",
	"scope":                        "authorization scope expression",
	"scope_predicate":              "legal hold predicate",
	"purpose_scope":                "endpoint purpose scope",
	"field_scopes":                 "partner field scopes",
	"version_binding":              "version pinning",
	"data_classes":                 "data class labels",
	"declared_fields":              "declared field labels",
	"event_kinds":                  "event kind labels",
	"capabilities":                 "capability labels",
	"supported_objects":            "connector object labels",
	"instrument_kinds":             "equity instrument kinds",
	"vesting":                      "vesting schedule terms",
	"options":                      "plan option terms",
	"coverage_tiers":               "benefit tier terms",
	"period":                       "payroll period bounds",
	"limits":                       "input limits",
	"taxability":                   "taxability rules",
	"calculation_basis":            "input calculation basis",
	"roles":                        "role references",
	"locations":                    "preference location references",
	"timing_constraints":           "scheduling constraints",
	"work_arrangements":            "arrangement codes",
	"skill_refs":                   "skill references",
	"families":                     "job family structure",
	"grades":                       "job grade structure",
	"levels":                       "job level structure",
	"profiles":                     "job profile structure",
	"aliases":                      "skill alias labels",
	"parent_refs":                  "hierarchy references",
	"proficiency_scale":            "scale definition",
	"job_codes":                    "covered job codes",
	"location_ids":                 "covered location references",
	"signal_refs":                  "signal references",
	"supply_refs":                  "supply references",
	"assumptions":                  "scenario assumptions",
	"channel_refs":                 "campaign channel references",
	"rule":                         "sampling rule",
	"reminder_policy":              "reminder policy",
	"form_definition":              "response form definition",
	"fixture_refs":                 "release fixture references",
	"quorum":                       "approval quorum rules",
	"separation":                   "separation-of-duties rules",
	"escalation":                   "escalation rules",
	"invalidators":                 "approval invalidator rules",
	"authority_floor":              "authority floor rules",
	"excluded":                     "excluded approver references",
	"trusted_time":                 "trusted timestamp evidence",
	"revocation_link":              "revocation linkage",
	"evidence_refs":                "evidence references",
	"evidence_bindings":            "evidence bindings",
	"affected_obligations":         "obligation references",
	"contradictions":               "composition diagnostics",
	"jurisdictions":                "jurisdiction labels",
	"traces":                       "composition traces",
	"inputs":                       "composition input digests",
	"presence_diagnostics":         "mapping diagnostics",
	"field_results":                "mapping results",
	"counts_by":                    "diagnostic counts",
	"old_rest":                     "schema snapshot diff",
	"new_rest":                     "schema snapshot diff",
	"system_boundary":              "authorization boundary description",
	"inherited_controls":           "control references",
	"procurement_answers":          "procurement questionnaire answers",
	"applicable_programs":          "program labels",
	"cms":                          "compliance control mapping",
	"fedramp":                      "compliance control mapping",
	"source":                       "dataset source reference",
	"applicability":                "release applicability",
	"consumer_refs":                "dataset consumer references",
	"impact_refs":                  "impact references",
	"overrides":                    "dataset overrides",
	"affected_intents":             "affected intent references",
	"definition":                   "case definition structure",
	"binding":                      "binding references",
	"receipt":                      "signed receipt envelope",
	"digests":                      "content digests",
	"field_scope":                  "copy field-scope labels",
	"data_categories":              "processing category labels",
	"allowed_operations":           "purpose operation labels",
	"recipients":                   "processing recipient labels",
	"record":                       "governance record envelope",
	"findings_table":               "invariant finding codes",
	"invariant_versions":           "invariant version pins",
	"obligations":                  "obligation references",
	"plan":                         "intervention plan structure",
	"error_bytes":                  "operation error bytes",
	"result_bytes":                 "operation result bytes",
	"material_changes":             "redrive material diff",
	"effective_interval_canonical": "interval canonical bytes",
	"callback_endpoints":           "partner endpoint URLs",
	"redirect_endpoints":           "partner endpoint URLs",
	"declared_capabilities":        "partner capability labels",
	"approver_evidence":            "installation approval evidence",
	"processor_refs":               "processor references",
	"destination_refs":             "destination references",
	"residency_refs":               "residency references",
	"scope_refs":                   "scope references",
	"items":                        "review item structures",
	"claim_mappings":               "issuer claim mappings",
	"certificate_policy":           "device certificate policy",
	"clock_trust_policy":           "device clock policy",
	"firmware_policy":              "device firmware policy",
	"offline_policy":               "device offline policy",
	"replay_policy":                "device replay policy",
	"retention_policy":             "device retention policy",
	"signature_policy":             "device signature policy",
	"policies_by":                  "policy index",
	"assignment":                   "work assignment structure",
	"request":                      "reservation request structure",
	"adjustments":                  "calibration adjustments",
	"legs":                         "mobility leg structures",
	"home_assignment":              "assignment references",
	"host_assignment":              "assignment references",
	"obligation":                   "bypass obligation structure",
	"metadata":                     "operational metadata envelope",
	"properties":                   "property envelope",
	"attributes":                   "attribute envelope",
	"configuration":                "connector configuration",
	"dependencies":                 "content dependencies",
	"fields":                       "field label listings",
}

// personalExactColumns are table.column pairs known to carry personal data
// whose names match no global pattern. Empty today: every personal column
// found so far matches a reviewed pattern, and an empty table keeps the
// seam for the next audit without an unverified entry.
var personalExactColumns = map[string]string{}

// opaqueEnvelopes are envelope column names whose bytes the checker cannot
// see inside. They are UNREVIEWED unless payloadVerdicts says otherwise.
var opaqueEnvelopes = map[string]bool{
	"payload": true, "body": true, "content": true,
}

// payloadVerdicts are reviewed classifications for opaque envelope columns
// in PERMANENT tables: table.column -> class with the audit reason.
var payloadVerdicts = map[string]struct {
	class  string
	reason string
}{
	// The ledger carries typed DOMAIN_FACT, EXTERNAL_OBSERVATION, CLAIM
	// and CORRECTION assertions over workforce subjects as inline
	// protobuf bytes (internal/data/ledger append.go: Payload; DDL
	// assertion-class check). A reversed hire's candidate record lives
	// here verbatim.
	"ledger_event.payload": {ClassPersonal, "typed domain facts over workforce subjects"},
	// Proposal material for the intent lifecycle (hires, promotions and
	// other subject-affecting decisions) is stored inline next to the
	// revision (proposal_revision payload/artifact xor).
	"proposal_revision.payload": {ClassPersonal, "intent proposal material over subject-affecting decisions"},
	// Connectivity observations record observed external state about
	// subjects (employment, payroll and provider facts).
	"external_observation.payload": {ClassPersonal, "observed external state about subjects"},
	// Definition bodies carry the seeded reference person/employment
	// entries the registry notes describe (storage-disposition.yaml:
	// definition_version notes cite person/employment/position/
	// compensation entries).
	"definition_version.body": {ClassPersonal, "seeded reference person and employment entries"},
	// Workflow signals carry subject-keyed event data into runs.
	"workflow_signal.payload": {ClassPersonal, "subject-keyed workflow event data"},
	// Merit emission payloads name the population members under review.
	"merit_compensation_intent_emission.payload": {ClassPersonal, "named review population"},
	// Integration receipts echo provider records about subjects.
	"integration_provider_receipt.payload": {ClassPersonal, "provider records about subjects"},
	// Pseudonym escrow ciphertext is subject data by construction:
	// destroying the key must destroy recoverability (WF-REV-015 input).
	"pseudonym_escrow_record.ciphertext": {ClassPersonal, "escrowed subject pseudonym"},
	// Contact endpoint verification evidence is recorded against the
	// subject's own channel (contactstore); it carries the address under
	// verification.
	"contact_endpoint_revision.verification": {ClassPersonal, "verification evidence for a subject channel"},
	// Accumulator policy envelopes are balance-computation terms.
	"accumulator_definition.correction_policy": {ClassNonPersonal, "accumulator correction policy terms"},
	"accumulator_definition.entry_types":       {ClassNonPersonal, "accumulator entry type labels"},
	// Demand planning intervals are operational bounds, not subject data.
	"demand_signal.work_interval": {ClassNonPersonal, "planning interval structure"},
	// Issuer receipts attest to custody handoff; the subject link is the
	// assignee receipt beside them.
	"asset_custody_event.issuer_receipt": {ClassNonPersonal, "issuer custody receipt envelope"},
	// Intent execution results record decisions over subject-affecting
	// intents; the outcomes name the affected population.
	"intent_result.result_body":            {ClassPersonal, "intent execution results over subject-affecting decisions"},
	"intent_simulation_result.result_body": {ClassPersonal, "intent execution results over subject-affecting decisions"},
	// Journey notes are free-text accounts of a subject's journey.
	"journey_note.body": {ClassPersonal, "journey note content about the subject"},
	// Source balance entries are referenced amounts and entry pointers.
	"balance_lifecycle_entry.source_entries": {ClassNonPersonal, "source balance entry references and amounts"},
	// Jurisdiction attributions on a worker tax profile say where the
	// subject resides and works.
	"worker_tax_profile_revision.residence_jurisdictions": {ClassPersonal, "worker residence jurisdiction attribution"},
	"worker_tax_profile_revision.work_jurisdictions":      {ClassPersonal, "worker work jurisdiction attribution"},
	// Config and reference envelopes carry no subject content.
	"config_object.body":                 {ClassNonPersonal, "configuration content, no subject data"},
	"job_definition.body":                {ClassNonPersonal, "job definition content, no subject data"},
	"legal_rule_pack.body":               {ClassNonPersonal, "rule pack content, no subject data"},
	"authorization_policy_snapshot.body": {ClassNonPersonal, "policy snapshot content, no subject data"},
}

// vaultRefs declares personal fields already moved to payload-vault
// references: table -> columns. Empty today: no vault exists and every
// registry row is PLATFORM_MANAGED, so any entry here without a matching
// FIELD_LEVEL row is reported as VAULT_UNPROVEN until WF-REV-015 lands.
var vaultRefs = map[string]map[string]bool{}

// checkDeclarations proves every reviewed verdict and vault reference for
// a table in this registry names a real scanned column. A declaration that
// names a missing column of a registered PERMANENT table is a forged or
// stale review: it must fail the check instead of silently covering
// nothing. Declarations for tables outside this registry are out of scope
// for the call (synthetic test registries); the live registry path
// validates the full declaration set because every verdict table is
// registered PERMANENT there.
func checkDeclarations(permanent map[string]storagedisposition.TableEntry, columns []Column) error {
	known := map[string]bool{}
	for _, col := range columns {
		known[strings.ToLower(col.Table)+"."+strings.ToLower(col.Name)] = true
	}
	var bad []string
	for key := range payloadVerdicts {
		table := key[:strings.Index(key, ".")]
		if _, inScope := permanent[table]; !inScope {
			continue
		}
		if !known[key] {
			bad = append(bad, "verdict "+key)
		}
	}
	for table, cols := range vaultRefs {
		if _, inScope := permanent[strings.ToLower(table)]; !inScope {
			continue
		}
		for col := range cols {
			if !known[strings.ToLower(table)+"."+col] {
				bad = append(bad, "vault_ref "+strings.ToLower(table)+"."+col)
			}
		}
	}
	if len(bad) != 0 {
		sort.Strings(bad)
		return fmt.Errorf("storeprivacy: declarations without a PERMANENT column: %s", strings.Join(bad, ", "))
	}
	return nil
}

// Classify returns the FieldRecord for one DDL column of a PERMANENT
// table, following the precedence documented above.
func Classify(col Column, entry storagedisposition.TableEntry) FieldRecord {
	name := strings.ToLower(col.Name)
	location := LocationColumn
	if isPayloadCapableType(col.Type) {
		location = LocationPayload
	}
	if vaultRefs[strings.ToLower(col.Table)] != nil && vaultRefs[strings.ToLower(col.Table)][name] {
		return FieldRecord{
			Table: col.Table, Field: name, Location: location,
			Class: ClassPersonal, Storage: StorageVaultRef,
			Owner:  entry.OwnerPackage,
			Reason: "declared payload-vault reference (WF-REV-015)",
		}
	}
	if reason, ok := nonPersonalColumns[name]; ok {
		return FieldRecord{
			Table: col.Table, Field: name, Location: location,
			Class: ClassNonPersonal, Storage: StorageInline,
			Owner: entry.OwnerPackage, Reason: "non-personal override: " + reason,
		}
	}
	if reason, ok := nonPersonalTableColumns[strings.ToLower(col.Table)+"."+name]; ok {
		return FieldRecord{
			Table: col.Table, Field: name, Location: location,
			Class: ClassNonPersonal, Storage: StorageInline,
			Owner: entry.OwnerPackage, Reason: "non-personal override: " + reason,
		}
	}
	if reason, ok := personalExactColumns[strings.ToLower(col.Table)+"."+name]; ok {
		return FieldRecord{
			Table: col.Table, Field: name, Location: location,
			Class: ClassPersonal, Storage: StorageInline,
			Owner: entry.OwnerPackage, Reason: "personal column: " + reason,
		}
	}
	for _, p := range subjectLinkSubstrings {
		if strings.Contains(name, p.substr) {
			return personalRecord(col, location, entry, p.reason)
		}
	}
	for _, p := range sensitiveSubstrings {
		if strings.Contains(name, p.substr) {
			return personalRecord(col, location, entry, p.reason)
		}
	}
	for _, suffix := range referenceSuffixes {
		if strings.HasSuffix(name, suffix) {
			return FieldRecord{
				Table: col.Table, Field: name, Location: location,
				Class: ClassNonPersonal, Storage: StorageInline,
				Owner: entry.OwnerPackage, Reason: "opaque record reference, no subject content",
			}
		}
	}
	if verdict, ok := payloadVerdicts[strings.ToLower(col.Table)+"."+name]; ok {
		return FieldRecord{
			Table: col.Table, Field: name, Location: location,
			Class: verdict.class, Storage: StorageInline,
			Owner: entry.OwnerPackage, Reason: "reviewed payload envelope: " + verdict.reason,
		}
	}
	for _, p := range personalSubstrings {
		if strings.Contains(name, p.substr) {
			return personalRecord(col, location, entry, p.reason)
		}
	}
	if opaqueEnvelopes[name] || isPayloadCapableType(col.Type) {
		return FieldRecord{
			Table: col.Table, Field: name, Location: location,
			Class: ClassUnreviewed, Storage: StorageInline,
			Owner: entry.OwnerPackage, Reason: "payload-capable column without reviewed rule",
		}
	}
	return FieldRecord{
		Table: col.Table, Field: name, Location: location,
		Class: ClassNonPersonal, Storage: StorageInline,
		Owner: entry.OwnerPackage, Reason: "scalar operational field",
	}
}

// personalRecord builds the inline-personal FieldRecord: the WF-REV-014
// violation shape once vault references exist to compare against.
func personalRecord(col Column, location string, entry storagedisposition.TableEntry, reason string) FieldRecord {
	return FieldRecord{
		Table: col.Table, Field: strings.ToLower(col.Name), Location: location,
		Class: ClassPersonal, Storage: StorageInline,
		Owner: entry.OwnerPackage, Reason: "personal column: " + reason,
	}
}
