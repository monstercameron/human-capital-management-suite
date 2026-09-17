// Package defaultproduct implements ALIGN-041 through ALIGN-047. It seeds
// the default product definitions the slice ships with: platform semantic
// tokens and floorplans, the authorization-resolved application shell, the
// governed work-loop pages, the accessible failure and recovery pages, the
// default routes bound to admitted capabilities, the zero-override product
// composition, and the domain-pack activation manifests.
//
// The package carries no tenant data and performs no persistence: it
// compiles versioned default definitions with content digests, and refuses
// anything unadmitted, unowned, or inconsistent before it can become
// product authority.
package defaultproduct

const schemaVersion = 1

// Version identifies this policy contract.
func Version() int { return schemaVersion }

// ContractExplain describes the policy without including definition values.
func ContractExplain() string {
	return "default product definitions seed tokens, shell, pages, routes, and manifests from admitted capabilities only"
}
