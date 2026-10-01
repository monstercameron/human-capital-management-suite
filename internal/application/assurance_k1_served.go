package application

import assurance "github.com/monstercameron/human-capital-management-suite/internal/operations/assurance"

// ComponentAssuranceRegister names the independent-assurance register composed
// for the serve role. The register owns evidence validation and claim bounds;
// application owns only its per-process lifetime and graph reachability.
const ComponentAssuranceRegister = "assurance-register"

func composeAssuranceRegister() *assurance.Register {
	return assurance.NewRegister()
}

// AssuranceRegister returns the register for this composed serve process.
// A nil App has no composed process and therefore no register.
func (a *App) AssuranceRegister() *assurance.Register {
	if a == nil {
		return nil
	}
	return a.assuranceRegister
}
