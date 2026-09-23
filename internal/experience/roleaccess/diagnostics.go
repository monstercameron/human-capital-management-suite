package roleaccess

// Diagnostics disclosure is PROMOUX-008's authorized-disclosure decision:
// the raw entity refs, digests, workflow instance/node internals and
// evidence references an ordinary promotion review never carries. It is the
// one rule every enforcement point uses (RBAC-RT-005): the journey
// transport's serializer, the journey engine's own redaction flag, and the
// inspection gate that admits oversight reads. Two hand-rolled role lists
// used to answer it differently — the engine admitted auditors but not
// operators, the transport admitted operators but not auditors — and the
// token-role checks behind both are gone: the role set below is the
// server-resolved durable assignment, never credential claims.
//
// The rule, over the resolved set:
//
//   - an HCM administrator (hcm_admin, comp_admin) is disclosed to: operating
//     and administering the governed machinery is exactly the job diagnosing
//     it serves;
//   - an auditor is disclosed to: independent oversight reads the same
//     execution evidence. Business fields stay under the data policy — an
//     auditor's compensation grant is redacted-only, so a redacted viewer
//     receives diagnostics with pay masked, never raw pay;
//   - otherwise the tenant's own journey-diagnostics page grant decides, so a
//     deployment can extend disclosure (for example to promotion_operator,
//     which holds it by default) without code changes.
//
// Anything else is denied, including an absent role store, an empty
// permission table, or a page grant without its rows: diagnostics authority
// never existed before this decision point, so there is no prior behavior a
// permissive default would need to preserve.
func CanDiscloseDiagnostics(snapshot Snapshot, subject string, admitted []string) bool {
	roles := AssignedRoles(snapshot, subject, admitted)
	if HasAdministratorRole(roles) {
		return true
	}
	if ContainsRole(roles, "auditor") {
		return true
	}
	return CanPageAction(EffectivePagePermissions(snapshot, roles), PageJourneyDiagnostics, ActionView)
}
