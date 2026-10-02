package journeyclient

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce/unitnames"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// unitLabel is an organization unit as a person reads it: the directory's own
// name when it sent one, else the seed's name for the code ("Safety &
// Quality"), else the code as words. A journey's placements carry only the
// unit code, so the table is what keeps an ampersand in a promotion's compare
// row and target organization.
func unitLabel(name, code string) string {
	if strings.TrimSpace(name) == "" {
		name = unitnames.Name(code)
	}
	return productui.UnitDisplayName(name, code)
}
