// Package unitnames is the one table from an organization unit's code to its
// display name ("safety-quality" is "Safety & Quality"). The demo companies'
// seed units (internal/data/demoworkforce) carry the same names, and a test
// there fails when the two drift. It imports nothing so the page and the
// product client can use it: a code turned into words by splitting at hyphens
// cannot recover an ampersand, so a client that has no unit name from the
// directory reads it here before it falls back to the code.
package unitnames

import "strings"

var names = map[string]string{
	// Ironridge Builders.
	"ironridge":                  "Ironridge Builders",
	"executive":                  "Executive",
	"operations":                 "Operations",
	"project-management":         "Project Management",
	"field-operations":           "Field Operations",
	"safety-quality":             "Safety & Quality",
	"warranty-service":           "Warranty & Service",
	"preconstruction":            "Preconstruction",
	"estimating-preconstruction": "Estimating & Preconstruction",
	"business-development":       "Business Development",
	"administration":             "Administration",
	"finance-admin":              "Finance & Admin",
	"people":                     "People",
	// HarborCare Health Services.
	"harborcare":           "HarborCare Health Services",
	"executive-office":     "Executive Office",
	"care-operations":      "Care Operations",
	"clinical-operations":  "Clinical Operations",
	"care-coordination":    "Care Coordination",
	"quality-safety":       "Quality & Safety",
	"product-technology":   "Product & Technology",
	"engineering-platform": "Engineering Platform",
	"product-management":   "Product Management",
	"data-analytics":       "Data & Analytics",
	"security-it":          "Security & IT",
	"growth-customer":      "Growth & Customer",
	"customer-success":     "Customer Success",
	"sales":                "Sales",
	"marketing":            "Marketing",
	"corporate-services":   "Corporate Services",
	"people-operations":    "People Operations",
	"finance":              "Finance",
	"legal-compliance":     "Legal & Compliance",
	"workplace-services":   "Workplace Services",
}

// aliases are the older codes of units that were renamed; they read as the
// unit they became.
var aliases = map[string]string{
	"eng-platform": "engineering-platform",
	"people-ops":   "people-operations",
}

// Name is the display name of a unit code, or "" when the code is not one the
// demo companies use.
func Name(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if canonical, ok := aliases[code]; ok {
		code = canonical
	}
	return names[code]
}

// Codes lists the codes the table names, for the test that holds it to the seed.
func Codes() []string {
	out := make([]string, 0, len(names))
	for code := range names {
		out = append(out, code)
	}
	return out
}
