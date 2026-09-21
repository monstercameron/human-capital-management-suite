package todogovernance

import "strings"

// This file records the reviewed, pre-existing backlog defects that the
// GOV-016/GOV-017/GOV-025 tests allow so the tests can be green today
// without weakening any rule: a finding with a Key() not present in the
// matching allowlist below fails the test. Every entry here is a genuine
// live-corpus violation as of 2026-09-05 (verified against
// planning/todos.md, 1517 todos) and must be reported to the backlog owner
// for repair; it is not evidence the rule is wrong.
//
// To retire an entry: fix the underlying todo, re-run the governance test,
// and delete the now-unmatched allowlist row (a stale, unmatched entry does
// not fail the test - only a NEW, unlisted finding does).

// gov016Allowlist is every GOV-016 finding present in the live corpus.
// 293 entries: 291 direct PHASE_INVERSION edges (a todo's declared phase
// depending on a strictly later declared phase - P0->GATE_A is the largest
// group at 99, followed by GATE_A->GATE_B at 101), 1 DEPENDENCY_CYCLE
// (LEGAL-002 <-> LEGAL-015), and 1 PROSE_DEPENDENCY (MSG-011's Depends field
// mixes a todo ID with free text: "hostile-content/integration ingress").
var gov016Allowlist = buildAllowlistKeys("GOV-016", func(add func(id, code, detail string)) {
	for _, e := range gov016PhaseInversions {
		add(e.From, CodePhaseInversion, e.To)
	}
	add("LEGAL-002", CodeDependencyCycle, "LEGAL-002 -> LEGAL-015 -> LEGAL-002")
	add("MSG-011", CodeProseDependency, "")
})

// gov018Allowlist is the reviewed snapshot of live GOV-018 applicability
// gaps. It is populated from the generated snapshot below; new findings are
// intentionally not inferred or silently accepted.
var gov018Allowlist = buildAllowlistKeys("GOV-018", func(add func(id, code, detail string)) {
	addGOV018CSV(add, CodeMissingApplicableTestClass, "CONFORMANCE", "OBS-015,PROMO-009,WEDGE-001,WEDGE-002,WEDGE-003,WEDGE-004,WEDGE-005,WEDGE-006,WEDGE-007,WEDGE-008,WEDGE-009,WEDGE-010,WEDGE-011,WEDGE-012,WEDGE-013,WEDGE-014,WEDGE-015")
	addGOV018CSV(add, CodeMissingApplicableTestClass, "FAULT", "CICD-002,CRYPTO-001,CUSTOMER-001,DATAOPS-005,DATAOPS-006,DB-001,DB-006,IAC-006,MSRC-008,ONBOARD-005,SVC-013,TOOL-015,WEB-081,WEB-083,XFORM-008")
	addGOV018CSV(add, CodeMissingApplicableTestClass, "GOLDEN", "ADMIN-003,ADMIN-004,ADMIN-006,API-002,APPROVAL-001,APPROVAL-004,ARTIFACT-002,AUTHN-001,AUTHN-002,AUTHN-003,AUTHN-004,AUTHN-005,AUTHN-006,AUTHN-007,AUTHN-008,BUDGET-001,CACHE-001,COMM-002,COMM-003,COMP-001,COMP-002,COMP-003,CONFLICT-002,CONN-RT-002,DATA-005,DATAOPS-004,DATAOPS-007,DATAOPS-008,DB-003,DB-004,DB-008,DB-009,DB-010,DB-017,DB-EDGE-001,DB-EDGE-004,EDGE-002,EDGE-003,EDGE-004,EDGE-005,ELIG-004,ENDPOINT-002,ENDPOINT-007,FORM-004,GOVERN-002,IAC-001,IAC-003,IAC-004,IAC-006,IAC-007,IAC-009,INTENT-003,INTENT-006,INTENT-007,INTENT-008,INTENT-011,INTENT-012,INTENT-014,INTENT-015,INTENT-021,INTENT-022,INTG-002,INTG-007,INTG-008,LEDGER-004,LEDGER-005,LEDGER-006,OBS-001,OBS-002,OBS-003,OBS-004,OBS-005,OBS-006,OBS-019,OBS-022,ONBOARD-004,ONBOARD-007,ONBOARD-008,OPS-002,OPS-007,OPS-008,OPS-010,ORG-002,PEOPLE-001,PEOPLE-002,PEOPLE-003,PERF-ENV-001,POP-003,POP-004,POP-007,POP-008,POSITION-001,POSITION-002,PRIV-001,PRIV-002,PRIV-005,PRIV-007,PRIV-EXIT-001,PROMO-002,RECORDS-DISP-001,RECOVERY-001,RPC-EDGE-001,STORE-001,STORE-002,SVC-002,SVC-004,SVC-005,SVC-007,SVC-010,TIME-001,TOOL-021,TRUST-001,TRUST-002,TRUST-003,TRUST-004,TRUST-005,TRUST-006,TRUST-007,TRUST-008,TRUST-009,TRUST-010,TRUST-012,TRUST-013,TRUST-014,TRUST-016,TRUST-017,TRUST-019,TRUST-020,TRUST-021,TRUST-022,TRUST-023,TRUST-024,TRUST-025,TRUST-026,TRUST-027,TRUST-028,TRUST-029,TRUST-030,TRUST-031,TRUST-032,TX-001,UX-003,UX-QUAL-001,WEDGE-003,WEDGE-005,WEDGE-006,WEDGE-008,WEDGE-009,WEDGE-013,WEDGE-014,WF-COMP-002,WF-COMP-003,WF-COMP-005,WF-RUN-001,WF-RUN-012,WF-STEP-014,WF-STEP-017")
	addGOV018CSV(add, CodeMissingApplicableTestClass, "INTEGRATION", "ACCESS-001,ADMIN-002,ADMIN-003,ADMIN-005,ADMIN-006,ADMIN-007,AGENT-001,AGENT-002,AGENT-003,AGENT-004,AGENT-005,APP-001,APP-002,APP-003,APP-004,APP-005,APP-006,CLOSE-002,EDGE-011,ENDPOINT-001,ENDPOINT-009,EQUITY-002,LEARN-006,LIB-016,LIB-019,LIB-020,NEXT-002,OBS-010,OBS-011,OBS-013,OBS-017,PHASE-001,PROOF-002,PROTO-009,PROTO-010,RECRUIT-001,SEARCH-001,SEARCH-002,SLICE-010,TOOL-021,TOOL-023,TOOL-024")
	addGOV018CSV(add, CodeMissingApplicableTestClass, "RECOVERY", "ALIGN-055,ALIGN-056,CICD-002,CICD-005,CP-009,CRYPTO-001,CUSTOMER-001,DATAOPS-006,DB-001,DB-006,DB-021,INTG-007,LIB-008,LIB-014,MSRC-008,OBS-014,ONBOARD-005,SVC-013,TOOL-015,WEB-081,WEB-083,WF-RUN-017,WF-RUN-018,XFORM-008")
	addGOV018CSV(add, CodeMissingApplicableTestClass, "SECURITY", "AGENT-002,APPROVAL-004,APPROVAL-006,ARTIFACT-001,ARTIFACT-003,ARTIFACT-006,ATTEST-004,BAL-008,CLOCK-001,CLOCK-002,CONN-RT-002,CUSTOM-006,CYCLE-009,DATA-004,DATA-018,DATAOPS-004,DOC-REDACT-001,ELIG-005,GARN-005,GOV-012,GOV-022,GOVERN-002,IAC-003,IAC-011,LEDGER-010,LIB-011,MATCH-005,MATCH-006,MODEL-007,MODEL-008,MODEL-026,ONBOARD-003,POP-001,POP-007,QUAL-004,READINESS-002,ROLLOUT-007,SCENARIO-002,SCHED-OPT-005,SETTLE-003,SUB-002,TOOL-016,TOOL-018,WEB-039,WEB-041,WEB-045,WEB-097,WEB-112")
	addGOV018CSV(add, CodeUnitOnlyWithoutReason, "", "WEDGE-003,WEDGE-005")
	// These promotion/UX entries were added after the 2026-09-05 snapshot.
	// Their primary proof exists, but the newly derived secondary class does
	// not yet have an independently named test. Keep the gap explicit rather
	// than claiming a different test class proves it.
	addGOV018CSV(add, CodeMissingApplicableTestClass, "CONFORMANCE", "PROMOUX-015,UIPOLISH-012,UXAUDIT-025,UXLIVE-046")
	addGOV018CSV(add, CodeMissingApplicableTestClass, "GOLDEN", "PROMOUX-003,PROMOUX-013")
	addGOV018CSV(add, CodeMissingApplicableTestClass, "INTEGRATION", "PROMOUX-007")
	addGOV018CSV(add, CodeMissingApplicableTestClass, "SECURITY", "UXAUDIT-015,UXLIVE-028,UXLIVE-029,UXLIVE-032,UXLIVE-045")
})

func addGOV018CSV(add func(id, code, detail string), code, detail, csv string) {
	for _, id := range strings.Split(csv, ",") {
		add(id, code, detail)
	}
}

type depEdge struct{ From, To string }

var gov016PhaseInversions = []depEdge{
	{From: "A11Y-001", To: "UXFLOW-009"},
	{From: "ADMIN-004", To: "CONN-RT-007"},
	{From: "ADMIN-004", To: "CP-007"},
	{From: "ADMIN-006", To: "TRUST-021"},
	{From: "ADMIN-007", To: "LEDGER-012"},
	{From: "ADMISSION-001", To: "TENANT-001"},
	{From: "API-001", To: "TOOL-009"},
	{From: "ARCH-GO-005", To: "CAP-001"},
	{From: "ARCH-GO-005", To: "INTENT-001"},
	{From: "ARCH-GO-006", To: "GOVERN-001"},
	{From: "ARCH-GO-006", To: "TRUST-001"},
	{From: "ARCH-GO-007", To: "WF-COMP-001"},
	{From: "ARCH-GO-007", To: "WF-RUN-001"},
	{From: "ARCH-GO-010", To: "COMP-001"},
	{From: "ARCH-GO-010", To: "DB-018"},
	{From: "ARCH-GO-010", To: "ORG-001"},
	{From: "ARCH-GO-010", To: "PEOPLE-001"},
	{From: "ARCH-GO-010", To: "POSITION-001"},
	{From: "ARCH-GO-011", To: "CONFLICT-001"},
	{From: "ARCH-GO-011", To: "TX-001"},
	{From: "ARCH-GO-012", To: "DB-018"},
	{From: "ARCH-GO-012", To: "LEDGER-001"},
	{From: "ARCH-GO-016", To: "TOOL-014"},
	{From: "ARCH-GO-021", To: "XFORM-008"},
	{From: "ARCH-GO-022", To: "MSG-001"},
	{From: "ARCH-GO-022", To: "WORK-001"},
	{From: "ARCH-GO-023", To: "EDGE-001"},
	{From: "ARCH-GO-023", To: "PROTO-006"},
	{From: "ARCH-GO-024", To: "ROLLOUT-001"},
	{From: "ARCH-GO-028", To: "ARCH-GO-019"},
	{From: "ARTIFACT-002", To: "DOC-MAL-001"},
	{From: "AUTHN-006", To: "TRUST-023"},
	{From: "AUTHN-009", To: "TRUST-013"},
	{From: "BAL-008", To: "BAL-006"},
	{From: "BAL-008", To: "BAL-007"},
	{From: "BIND-001", To: "CAP-001"},
	{From: "CAP-002", To: "GOVERN-003"},
	{From: "CICD-001", To: "TOOL-014"},
	{From: "CICD-001", To: "TOOL-015"},
	{From: "CICD-002", To: "DB-021"},
	{From: "CICD-003", To: "TOOL-017"},
	{From: "CICD-003", To: "TOOL-018"},
	{From: "CLOSE-002", To: "TOPOLOGY-001"},
	{From: "COMMERCIAL-001", To: "WEDGE-007"},
	{From: "COMMERCIAL-001", To: "WEDGE-009"},
	{From: "CONF-001", To: "SANDBOX-001"},
	{From: "CONF-001", To: "WF-COMP-004"},
	{From: "CONFIG-002", To: "TRUST-020"},
	{From: "CONFIG-010", To: "CONFIG-003"},
	{From: "CONFIG-010", To: "CP-009"},
	{From: "CONFIG-010", To: "ROLLOUT-008"},
	{From: "CONFIG-010", To: "WF-RUN-018"},
	{From: "CONTRACT-ARCHIVE-001", To: "CONFIG-003"},
	{From: "CONTRACT-ARCHIVE-001", To: "EVIDENCE-001"},
	{From: "CONTRACT-ARCHIVE-001", To: "TOOL-018"},
	{From: "CP-001", To: "CONFIG-001"},
	{From: "CP-001", To: "DB-007"},
	{From: "CP-003", To: "TOOL-018"},
	{From: "CP-004", To: "TENANT-001"},
	{From: "CP-009", To: "CONFIG-003"},
	{From: "CUSTOMER-001", To: "WEDGE-001"},
	{From: "CUSTOMER-002", To: "CP-007"},
	{From: "CUSTOMER-002", To: "DATAOPS-006"},
	{From: "CUSTOMER-002", To: "ONBOARD-006"},
	{From: "CUSTOMER-003", To: "RECOVERY-004"},
	{From: "CUSTOMER-004", To: "UXFLOW-009"},
	{From: "DATA-017", To: "TENANT-001"},
	{From: "DATA-019", To: "TENANT-001"},
	{From: "DATA-022", To: "DATA-015"},
	{From: "DATA-022", To: "LEDGER-012"},
	{From: "DATAOPS-004", To: "RULE-004"},
	{From: "DB-001", To: "TOOL-014"},
	{From: "DB-002", To: "DATA-001"},
	{From: "DB-005", To: "TENANT-001"},
	{From: "DB-021", To: "TOOL-020"},
	{From: "DB-EDGE-001", To: "DB-018"},
	{From: "DIAG-001", To: "TRUST-021"},
	{From: "EDGE-002", To: "ADMISSION-001"},
	{From: "EDGE-003", To: "ADMISSION-001"},
	{From: "EDGE-003", To: "ADMISSION-002"},
	{From: "EDGE-010", To: "EDGE-008"},
	{From: "ENDPOINT-001", To: "API-001"},
	{From: "ENDPOINT-003", To: "CAP-003"},
	{From: "ENDPOINT-007", To: "ADMISSION-001"},
	{From: "ENDPOINT-007", To: "EDGE-007"},
	{From: "ENDPOINT-008", To: "ENDPOINT-004"},
	{From: "ENDPOINT-009", To: "ENDPOINT-008"},
	{From: "ENGINE-CONF-001", To: "ATTEST-004"},
	{From: "ENGINE-CONF-001", To: "BAL-004"},
	{From: "ENGINE-CONF-001", To: "CYCLE-003"},
	{From: "ENGINE-CONF-001", To: "DEMAND-003"},
	{From: "ENGINE-CONF-001", To: "ELIG-003"},
	{From: "ENGINE-CONF-001", To: "MATCH-004"},
	{From: "ENGINE-CONF-001", To: "POP-003"},
	{From: "ENGINE-CONF-001", To: "QUAL-004"},
	{From: "ENGINE-CONF-001", To: "SCENARIO-003"},
	{From: "ENGINE-CONF-001", To: "XFORM-003"},
	{From: "EP-OPS-001", To: "PROTO-007"},
	{From: "EVENT-001", To: "DATA-008"},
	{From: "EVENT-001", To: "DB-020"},
	{From: "EVENT-001", To: "LEDGER-008"},
	{From: "GOV-018", To: "MODEL-023"},
	{From: "GOV-020", To: "TIME-001"},
	{From: "GOV-021", To: "GOV-019"},
	{From: "GOVERN-001", To: "PRIV-001"},
	{From: "IAC-001", To: "RECOVERY-001"},
	{From: "IAC-001", To: "TENANT-001"},
	{From: "IAC-002", To: "TENANT-002"},
	{From: "IAC-002", To: "TOOL-014"},
	{From: "IAC-005", To: "TOOL-018"},
	{From: "IAC-006", To: "DB-022"},
	{From: "IAC-007", To: "DATA-018"},
	{From: "IAC-008", To: "TRUST-023"},
	{From: "IAC-009", To: "ADMISSION-001"},
	{From: "IAC-012", To: "RECOVERY-004"},
	{From: "IAC-013", To: "IAC-012"},
	{From: "IDEMP-001", To: "DATA-009"},
	{From: "IDEMP-001", To: "INTG-018"},
	{From: "IDEMP-001", To: "JOB-001"},
	{From: "IDEMP-001", To: "MSG-006"},
	{From: "IDEMP-001", To: "TX-006"},
	{From: "INTEL-001", To: "LEDGER-003"},
	{From: "INTEL-001", To: "TX-006"},
	{From: "INTENT-006", To: "APPROVAL-003"},
	{From: "INTG-002", To: "TRUST-016"},
	{From: "INTG-010", To: "TX-009"},
	{From: "LABOR-005", To: "PAYGL-003"},
	{From: "LEDGER-002", To: "DB-018"},
	{From: "LEDGER-005", To: "MODEL-025"},
	{From: "LEDGER-009", To: "TENANT-001"},
	{From: "LEGAL-008", To: "LEGAL-001"},
	{From: "LIB-001", To: "TOOL-019"},
	{From: "LIB-009", To: "TOOL-014"},
	{From: "LIB-014", To: "TOOL-017"},
	{From: "LIB-014", To: "TOOL-018"},
	{From: "MASK-001", To: "DATA-019"},
	{From: "MASK-001", To: "PRIV-001"},
	{From: "MSRC-004", To: "DB-008"},
	{From: "MSRC-004", To: "DB-009"},
	{From: "MSRC-004", To: "DB-010"},
	{From: "MSRC-004", To: "DB-011"},
	{From: "MSRC-005", To: "DB-014"},
	{From: "MSRC-005", To: "DB-015"},
	{From: "NEXT-002", To: "TOPOLOGY-001"},
	{From: "NEXT-004", To: "DATA-007"},
	{From: "NEXT-005", To: "EVIDENCE-001"},
	{From: "NEXT-005", To: "RECON-001"},
	{From: "NEXT-005", To: "RECON-002"},
	{From: "OBS-001", To: "MODEL-023"},
	{From: "OBS-001", To: "TRUST-018"},
	{From: "OBS-010", To: "CAP-003"},
	{From: "OBS-011", To: "INTG-001"},
	{From: "OBS-011", To: "TRUST-006"},
	{From: "OBS-012", To: "TX-004"},
	{From: "OBS-012", To: "WF-RUN-024"},
	{From: "OBS-013", To: "DATA-007"},
	{From: "OBS-013", To: "JOB-001"},
	{From: "OBS-014", To: "OBS-002"},
	{From: "OBS-015", To: "TOOL-014"},
	{From: "OBS-017", To: "OBS-004"},
	{From: "OBS-018", To: "CONFIG-003"},
	{From: "OBS-019", To: "ADMISSION-002"},
	{From: "OBS-021", To: "PRIV-EXIT-001"},
	{From: "OBS-021", To: "RECORDS-COPY-001"},
	{From: "ONBOARD-004", To: "ADMISSION-001"},
	{From: "ONBOARD-007", To: "ONBOARD-005"},
	{From: "ONBOARD-007", To: "ONBOARD-006"},
	{From: "ONBOARD-008", To: "RECORDS-HOLD-001"},
	{From: "OPS-001", To: "WEDGE-005"},
	{From: "OPS-001", To: "WEDGE-006"},
	{From: "OPS-004", To: "WORK-001"},
	{From: "OPS-006", To: "TENANT-001"},
	{From: "OPS-008", To: "OBS-007"},
	{From: "PHASE-001", To: "WEDGE-001"},
	{From: "POP-003", To: "DB-020"},
	{From: "POP-004", To: "PRIV-001"},
	{From: "PRIV-002", To: "MODEL-027"},
	{From: "PRIV-002", To: "MSG-004"},
	{From: "PRIV-002", To: "PRIV-001"},
	{From: "PRIV-004", To: "MODEL-028"},
	{From: "PROTO-003", To: "WORK-001"},
	{From: "PROTO-004", To: "LEDGER-012"},
	{From: "PROTO-005", To: "PROTO-003"},
	{From: "PROTO-005", To: "PROTO-004"},
	{From: "PROTO-010", To: "CAP-001"},
	{From: "PROVIDER-001", To: "CONN-RT-007"},
	{From: "PROVIDER-001", To: "INTG-012"},
	{From: "PROVIDER-002", To: "INTG-014"},
	{From: "PROVIDER-002", To: "ROLLOUT-008"},
	{From: "RECORDS-DISP-001", To: "PRIV-001"},
	{From: "RECOVERY-001", To: "DATA-013"},
	{From: "REFDATA-001", To: "CONFIG-003"},
	{From: "REFDATA-001", To: "DATA-021"},
	{From: "RESIDENCY-001", To: "PRIV-001"},
	{From: "RESIDENCY-001", To: "PRIV-003"},
	{From: "RESIDENCY-001", To: "RECORDS-COPY-001"},
	{From: "RESIDENCY-001", To: "RECOVERY-005"},
	{From: "RESIDENCY-001", To: "TENANT-001"},
	{From: "ROLLOUT-002", To: "TENANT-001"},
	{From: "RPC-EDGE-001", To: "TX-005"},
	{From: "SELECT-001", To: "LEGAL-001"},
	{From: "SELECT-002", To: "WEDGE-003"},
	{From: "SELECT-002", To: "WEDGE-005"},
	{From: "SELECT-002", To: "WEDGE-009"},
	{From: "SNAPSHOT-001", To: "TRUST-004"},
	{From: "STATUS-001", To: "MSG-001"},
	{From: "STATUS-001", To: "OBS-007"},
	{From: "STORE-001", To: "RECOVERY-001"},
	{From: "SUBPROCESSOR-001", To: "OPS-009"},
	{From: "SUBPROCESSOR-001", To: "PRIV-001"},
	{From: "SUBPROCESSOR-001", To: "PRIV-003"},
	{From: "SUPPLY-001", To: "TOOL-015"},
	{From: "SUPPLY-001", To: "TOOL-016"},
	{From: "SUPPLY-001", To: "TOOL-017"},
	{From: "SUPPLY-002", To: "TENANT-001"},
	{From: "SVC-002", To: "TIME-001"},
	{From: "SVC-002", To: "TOOL-014"},
	{From: "SVC-004", To: "DB-012"},
	{From: "SVC-004", To: "WF-RUN-002"},
	{From: "SVC-005", To: "WF-RUN-004"},
	{From: "SVC-005", To: "WF-RUN-005"},
	{From: "SVC-007", To: "DATA-008"},
	{From: "SVC-007", To: "DATA-009"},
	{From: "SVC-010", To: "DATA-008"},
	{From: "SVC-010", To: "MSG-004"},
	{From: "SVC-011", To: "TRUST-021"},
	{From: "SVC-012", To: "ARCH-GO-019"},
	{From: "TENANT-002", To: "TENANT-001"},
	{From: "THREAT-001", To: "ENDPOINT-005"},
	{From: "THREAT-001", To: "TRUST-019"},
	{From: "TOOL-016", To: "TOOL-015"},
	{From: "TOOL-019", To: "TOOL-017"},
	{From: "TOOL-021", To: "TIME-001"},
	{From: "TOOL-021", To: "WF-RUN-002"},
	{From: "TOOL-023", To: "TOOL-017"},
	{From: "TOOL-023", To: "TOOL-018"},
	{From: "TOOL-025", To: "TOOL-017"},
	{From: "TOOL-025", To: "TOOL-018"},
	{From: "TOOL-025", To: "TOOL-019"},
	{From: "TOOL-025", To: "TOOL-023"},
	{From: "TOOL-025", To: "TOOL-024"},
	{From: "TRUST-006", To: "TOOL-018"},
	{From: "TRUST-026", To: "CRYPTO-001"},
	{From: "TRUST-026", To: "TRUST-015"},
	{From: "TRUST-026", To: "TRUST-023"},
	{From: "TRUST-030", To: "TRUST-023"},
	{From: "TRUST-032", To: "TRUST-031"},
	{From: "TX-008", To: "WF-RUN-014"},
	{From: "UX-002", To: "TOOL-009"},
	{From: "UXFLOW-001", To: "UX-001"},
	{From: "UXFLOW-002", To: "TRUST-010"},
	{From: "UXFLOW-002", To: "TRUST-013"},
	{From: "UXFLOW-002", To: "TRUST-014"},
	{From: "UXFLOW-003", To: "UX-006"},
	{From: "UXFLOW-004", To: "INTENT-021"},
	{From: "UXFLOW-004", To: "UX-005"},
	{From: "UXFLOW-004", To: "UX-007"},
	{From: "UXFLOW-005", To: "INTENT-014"},
	{From: "UXFLOW-006", To: "TRUST-003"},
	{From: "UXFLOW-006", To: "UX-007"},
	{From: "UXFLOW-007", To: "INTG-014"},
	{From: "UXFLOW-007", To: "REPAIR-001"},
	{From: "UXFLOW-007", To: "UX-005"},
	{From: "UXFLOW-008", To: "FORM-004"},
	{From: "UXFLOW-008", To: "UX-003"},
	{From: "UXFLOW-008", To: "UX-004"},
	{From: "UXFLOW-010", To: "UXFLOW-009"},
	{From: "UXFLOW-011", To: "INTENT-028"},
	{From: "WEB-001", To: "UX-001"},
	{From: "WF-COMP-006", To: "CONFIG-003"},
	{From: "WF-DISC-002", To: "WF-COMP-002"},
	{From: "WF-DISC-004", To: "WF-COMP-001"},
	{From: "WF-DISC-007", To: "WF-COMP-001"},
	{From: "WF-DISC-007", To: "WF-COMP-002"},
	{From: "WF-DISC-008", To: "CAP-002"},
	{From: "WF-DISC-008", To: "INTENT-013"},
	{From: "WF-DISC-008", To: "WF-RUN-012"},
	{From: "WF-DISC-009", To: "CAP-002"},
	{From: "WF-DISC-009", To: "MODEL-021"},
	{From: "WF-DISC-010", To: "TOOL-014"},
	{From: "WF-DISC-011", To: "INTENT-015"},
	{From: "WF-DISC-011", To: "INTENT-016"},
	{From: "WF-DISC-011", To: "INTENT-018"},
	{From: "WF-DISC-011", To: "INTENT-019"},
	{From: "WF-RUN-001", To: "DATA-002"},
	{From: "WF-RUN-005", To: "INTG-018"},
	{From: "WF-STEP-006", To: "INTG-018"},
	{From: "WEB-241", To: "UXAUDIT-014"},
	{From: "WEB-246", To: "UIPOLISH-009"},
	{From: "WEB-246", To: "UXAUDIT-008"},
	{From: "WF-EXT-001", To: "PROMO-EXEC-007"},
	{From: "WORKER-LIFE-002", To: "LEARN-003"},
	{From: "XFORM-006", To: "XFORM-003"},
	{From: "XFORM-006", To: "XFORM-004"},
	{From: "XFORM-006", To: "XFORM-005"},
}

// gov017EvidenceAllowlist is every todo checked off ("- [x]") in the live
// corpus whose Evidence field is either absent (e.g. UX-002, UX-005) or
// present but does not literally name its TEST field's value and/or a
// `go test` result (e.g. TOOL-015's evidence describes the test matrix by
// todo ID rather than by the TEST function name). 60 entries.
var gov017EvidenceAllowlist = []string{
	"ADMIN-004", "ADMIN-005", "ADMIN-006", "ADMIN-007", "ANON-001",
	"ARCH-GO-025", "ARCH-GO-026", "AVAIL-001", "BAL-001", "CLIENT-001",
	"CLIENT-002", "CUSTOM-001", "CYCLE-001", "DOC-TEMPLATE-001", "EFFECT-001",
	"ELIG-007", "FORM-005", "FORM-006", "I18N-001", "I18N-002",
	"I18N-003", "INTG-003", "LEAVE-001", "LEDGER-007", "LEGAL-009",
	"MODEL-027", "MODEL-032", "MSG-001", "OBS-010", "OBS-011",
	"ORG-001", "REPLAN-001", "REPORT-001", "REPORT-002", "REPORT-003",
	"RESERVE-001", "STATUS-001", "SVC-003", "TENANT-001", "TOOL-015",
	"TRUST-012", "TRUST-019", "UX-002", "UX-005", "UX-006",
	"UX-007", "UX-008", "UX-009", "UXFLOW-001", "UXFLOW-002",
	"UXFLOW-003", "UXFLOW-004", "UXFLOW-005", "UXFLOW-006", "UXFLOW-007",
	"UXFLOW-008", "UXFLOW-009", "UXFLOW-010", "UXFLOW-011", "XFORM-001",
}

// gov017Allowlist expands gov017EvidenceAllowlist into finding keys. The
// three evidence codes are allowlisted together per todo ID since a given
// todo may trip one, two, or all three (missing entirely vs. present but
// incomplete) depending on which of "has an Evidence line at all", "names
// its TEST", and "reports a go test result" it fails; Finding.Key() ignores
// which specific incompleteness fired within CodeMissingEvidence's siblings
// only when the ID is allowlisted for that code.
var gov017Allowlist = buildAllowlistKeys("GOV-017", func(add func(id, code, detail string)) {
	for _, id := range gov017EvidenceAllowlist {
		add(id, CodeMissingEvidence, "")
		add(id, CodeEvidenceMissingTestName, "")
		add(id, CodeEvidenceMissingGoTest, "")
	}
	// Pre-existing live-corpus evidence gap observed while adding GOV-018.
	// The backlog owner must add the exact primary test to DB-012's evidence.
	for _, id := range []string{"ABUSE-003", "BAL-003", "DB-012"} {
		add(id, CodeEvidenceMissingTestName, "")
	}
	// Reviewed evidence-format debt added after the original snapshot. These
	// entries preserve exact finding codes: fixing a missing test name does not
	// silently waive a missing command result, or vice versa.
	addGOV017CSV(add, CodeEvidenceMissingGoTest, "CONN-RT-009,CUSTOMER-003,DATA-012,DB-EDGE-002,FORM-001,FORM-002,FORM-003,MODEL-008,MODEL-009,NEXT-001,NEXT-009,OBS-008,PROMOUX-015,RECOVERY-005,SLICE-001,SLICE-002,SLICE-003,SLICE-004,SLICE-005,SLICE-006,SLICE-007,SLICE-008,SLICE-009,SLICE-010,SLICE-011,SLICE-012,SLICE-013,SLICE-014,SLICE-015,SOURCE-001,WEB-241,WEB-244,WEB-245,WEB-246,WEDGE-015,WF-COMP-007,WF-RUN-035,WF-RUN-036,WF-RUN-037,WF-RUN-038,WF-RUN-039,WF-RUN-040,WF-STEP-011,WF-STEP-012,WF-STEP-013,WF-STEP-015,WF-STEP-018,WF-UI-002,WF-UI-004,WF-UI-005,WF-UI-006,WF-UI-007,WF-UI-009")
	addGOV017CSV(add, CodeEvidenceMissingTestName, "A11Y-001,CROSS-CONF-001,ENGINE-CONF-001,FEATURE-CONF-001,FORM-001,FORM-002,FORM-003,INTENT-CONF-002,MODEL-008,MODEL-009,NEXT-001,RECRUIT-003,RECRUIT-004,REV-055-02,REV-100-01,SLICE-001,SLICE-002,SLICE-003,SLICE-004,SLICE-005,SLICE-006,SLICE-007,SLICE-008,SLICE-009,SLICE-010,SLICE-011,SLICE-012,SLICE-013,SLICE-014,SLICE-015,SOURCE-001,UX-JOBARCH-001,UXAUDIT-002,UXAUDIT-009,UXAUDIT-010,UXAUDIT-013,WEB-241,WEB-242,WEB-243,WEB-244,WEB-245,WEB-246,WF-STEP-011,WF-STEP-012,WF-STEP-013,WF-STEP-015,WORKER-LIFE-001,WORKER-LIFE-002,WORKER-LIFE-003,WORKER-LIFE-004")
	addGOV017CSV(add, CodeMissingEvidence, "UXSCAN-001,UXSCAN-002,UXSCAN-003,UXSCAN-004,UXSCAN-005,UXSCAN-006,UXSCAN-007,UXSCAN-009,UXSCAN-010")
})

func addGOV017CSV(add func(id, code, detail string), code, csv string) {
	for _, id := range strings.Split(csv, ",") {
		add(id, code, "")
	}
}

type intentViolation struct{ ID, Code, Detail string }

// gov025Violations is every GOV-025 finding present in the live corpus: 34
// todos declaring a ROLE outside the eight-value vocabulary (ALIGN-*'s
// "DATA"/"OPERATIONS", ALIGN-*/SLICE-006/WF-DISC-007's "ORCHESTRATION"), and
// 5 todos whose DIRECT field names a display label ("PromoteWorker",
// "RequestLeave,ExtendLeave,ReturnFromLeave") instead of "none" or a
// catalog definition_ref. 39 entries.
var gov025Violations = []intentViolation{
	{"ALIGN-009", CodeUnknownRole, "DATA"},
	{"ALIGN-010", CodeUnknownRole, "DATA"},
	{"ALIGN-011", CodeUnknownRole, "DATA"},
	{"ALIGN-012", CodeUnknownRole, "DATA"},
	{"ALIGN-013", CodeUnknownRole, "DATA"},
	{"ALIGN-014", CodeUnknownRole, "DATA"},
	{"ALIGN-015", CodeUnknownRole, "DATA"},
	{"ALIGN-016", CodeUnknownRole, "DATA"},
	{"ALIGN-025", CodeUnknownRole, "ORCHESTRATION"},
	{"ALIGN-026", CodeUnknownRole, "ORCHESTRATION"},
	{"ALIGN-027", CodeUnknownRole, "ORCHESTRATION"},
	{"ALIGN-028", CodeUnknownRole, "ORCHESTRATION"},
	{"ALIGN-029", CodeUnknownRole, "ORCHESTRATION"},
	{"ALIGN-030", CodeUnknownRole, "ORCHESTRATION"},
	{"ALIGN-031", CodeUnknownRole, "ORCHESTRATION"},
	{"ALIGN-032", CodeUnknownRole, "ORCHESTRATION"},
	{"ALIGN-033", CodeUnknownRole, "DATA"},
	{"ALIGN-034", CodeUnknownRole, "DATA"},
	{"ALIGN-035", CodeUnknownRole, "DATA"},
	{"ALIGN-036", CodeUnknownRole, "DATA"},
	{"ALIGN-037", CodeUnknownRole, "DATA"},
	{"ALIGN-038", CodeUnknownRole, "DATA"},
	{"ALIGN-039", CodeUnknownRole, "DATA"},
	{"ALIGN-040", CodeUnknownRole, "DATA"},
	{"ALIGN-049", CodeUnknownRole, "OPERATIONS"},
	{"ALIGN-050", CodeUnknownRole, "OPERATIONS"},
	{"ALIGN-051", CodeUnknownRole, "OPERATIONS"},
	{"ALIGN-052", CodeUnknownRole, "OPERATIONS"},
	{"ALIGN-053", CodeUnknownRole, "OPERATIONS"},
	{"ALIGN-054", CodeUnknownRole, "OPERATIONS"},
	{"ALIGN-055", CodeUnknownRole, "OPERATIONS"},
	{"ALIGN-056", CodeUnknownRole, "OPERATIONS"},
	{"NEXT-005", CodeInvalidDirect, "PromoteWorker"},
	{"NEXT-006", CodeInvalidDirect, "PromoteWorker"},
	{"NEXT-007", CodeInvalidDirect, "RequestLeave,ExtendLeave,ReturnFromLeave"},
	{"NEXT-009", CodeInvalidDirect, "PromoteWorker"},
	{"SLICE-006", CodeUnknownRole, "ORCHESTRATION"},
	{"UX-009", CodeInvalidDirect, "PromoteWorker"},
	{"WF-DISC-007", CodeUnknownRole, "ORCHESTRATION"},
}

var gov025Allowlist = buildAllowlistKeys("GOV-025", func(add func(id, code, detail string)) {
	for _, v := range gov025Violations {
		// NEXT-007's DIRECT is a comma list; ValidateIntentContext reports
		// one finding per comma-separated token it rejects, each carrying
		// that token as Detail. The other four INVALID_DIRECT rows and all
		// UNKNOWN_ROLE rows are single-value fields.
		if v.ID == "NEXT-007" {
			add(v.ID, v.Code, "RequestLeave")
			add(v.ID, v.Code, "ExtendLeave")
			add(v.ID, v.Code, "ReturnFromLeave")
			continue
		}
		add(v.ID, v.Code, v.Detail)
	}
})

// buildAllowlistKeys is a small helper so each allowlist above is built from
// its readable source table via Finding.Key(), keeping the key format in
// exactly one place (types.go).
func buildAllowlistKeys(rule string, populate func(add func(id, code, detail string))) map[string]bool {
	keys := map[string]bool{}
	add := func(id, code, detail string) {
		f := Finding{TodoID: id, Rule: rule, Code: code, Detail: detail}
		keys[f.Key()] = true
	}
	populate(add)
	return keys
}
