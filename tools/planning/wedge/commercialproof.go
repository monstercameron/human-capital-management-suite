package wedge

import (
	"fmt"
	"sort"
	"strings"
)

// Commercial-proof verdicts for REV-002-02: a repeatable-ICP claim needs
// more than one customer feasibility result.
type ProofVerdict string

const (
	// ProofMarketProof admits a repeatable-ICP claim: at least two distinct
	// independently signed PROCEED decisions share one problem class.
	ProofMarketProof ProofVerdict = "MARKET_PROOF"
	// ProofSingleCustomer blocks any repeatable-ICP claim: only one
	// qualifying partner is on record.
	ProofSingleCustomer ProofVerdict = "SINGLE_CUSTOMER_ONLY"
	// ProofInsufficient blocks any market claim: no qualifying partner, a
	// forged signature, a non-proceed decision or mismatched problem
	// classes.
	ProofInsufficient ProofVerdict = "INSUFFICIENT"
)

// PartnerProof is one partner manifest's verified decision fact as seen by
// the commercial-proof rule. SignatureValid must come from an actual
// signature verification (VerifyGateADecision/VerifyGateBDecision), never
// from the manifest's own claims.
type PartnerProof struct {
	ManifestDigest string
	KeyID          string
	Gate           string
	Decision       string
	ProblemClass   string
	SignatureValid bool
}

// proceedDecisions are the Gate A/B outcomes that count toward market proof.
// PROCEED_LIMITED is Gate B's only proceed verdict, so excluding it would
// make the "or B" half of the rule unusable.
var proceedDecisions = map[string]bool{
	"PROCEED":         true,
	"CONDITIONAL_GO":  true,
	"PROCEED_LIMITED": true,
}

// DecideCommercialProof folds partner proofs into a market-proof verdict. A
// duplicated (digest, key) pair counts once: replaying one partner's
// evidence never manufactures a second customer.
func DecideCommercialProof(partners []PartnerProof) (ProofVerdict, []string) {
	type identity struct {
		digest string
		key    string
	}
	seen := map[identity]PartnerProof{}
	var order []identity
	var reasons []string
	for i, p := range partners {
		tag := fmt.Sprintf("partner %d", i)
		switch {
		case !p.SignatureValid:
			reasons = append(reasons, tag+": signature invalid")
		case !proceedDecisions[p.Decision]:
			reasons = append(reasons, tag+": decision "+p.Decision+" is not a proceed verdict")
		case p.ManifestDigest == "" || p.KeyID == "" || p.ProblemClass == "":
			reasons = append(reasons, tag+": missing digest, key or problem class")
		default:
			id := identity{digest: p.ManifestDigest, key: p.KeyID}
			if _, dup := seen[id]; dup {
				reasons = append(reasons, tag+": duplicates an already-counted manifest")
				continue
			}
			seen[id] = p
			order = append(order, id)
		}
	}
	if len(order) == 0 {
		if len(reasons) == 0 {
			reasons = []string{"no partner proofs supplied"}
		}
		return ProofInsufficient, reasons
	}
	class := seen[order[0]].ProblemClass
	for _, id := range order[1:] {
		if seen[id].ProblemClass != class {
			return ProofInsufficient, append(reasons, "problem classes differ across partners")
		}
	}
	if len(order) == 1 {
		return ProofSingleCustomer, reasons
	}
	return ProofMarketProof, reasons
}

// RenderProofVerdict renders a verdict deterministically for golden pins.
func RenderProofVerdict(verdict ProofVerdict, reasons []string) string {
	sorted := append([]string(nil), reasons...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString("verdict: ")
	b.WriteString(string(verdict))
	b.WriteString("\n")
	if len(sorted) == 0 {
		b.WriteString("reasons: none\n")
		return b.String()
	}
	b.WriteString("reasons:\n")
	for _, r := range sorted {
		b.WriteString("- ")
		b.WriteString(r)
		b.WriteString("\n")
	}
	return b.String()
}
