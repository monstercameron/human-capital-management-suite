package demoworkforce

import (
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// DirectoryEntry is the business-directory detail the chat Person details
// panel and the directory show for one demo employee beyond what the
// journey_worker row carries: a work email and phone, and the display name
// of the person's department.
//
// journey_worker is append-only with one row per worker, so these facts cannot
// be added to people the review database already holds. They are derived from
// the roster instead, the same way DemoBirthday derives a birthday from the
// worker key, so every replay of the seed agrees and no migration is needed.
type DirectoryEntry struct {
	WorkerKey string
	// Email is first.last@<company>.example. The .example domain is reserved
	// (RFC 2606), so no demo address can reach a real mailbox.
	Email string
	// Phone is a fictional North American number: the area code of the
	// person's location and an exchange of 555-01xx, the range reserved for
	// fictional use.
	Phone string
	// Department is the display name of the person's organization unit.
	Department string
	// PhotoURL is the same-origin photograph of the person, or "" when the
	// company has no headshot left for them and they are drawn with initials.
	PhotoURL string
}

// areaCodes are the area codes of every demo location city, keyed by the text
// before the comma in the location's name.
var areaCodes = map[string]string{
	"Denver": "303", "Commerce City": "303", "Aurora": "303", "Lakewood": "303",
	"Littleton": "303", "Golden": "303",
	"Boston": "617", "Atlanta": "404", "Chicago": "312", "Dallas": "214",
	"San Francisco": "415", "Seattle": "206", "New York": "212",
}

var (
	directoryMu    sync.Mutex
	directoryCache = map[string]map[string]DirectoryEntry{}
)

// OrgUnitName is the display name of one of the company's organization units,
// or "" when the code is not one of them.
func (p *Pack) OrgUnitName(code string) string {
	code = strings.TrimSpace(code)
	for _, unit := range p.Company.Units {
		if unit.Code == code {
			return unit.Name
		}
	}
	return ""
}

// Directory returns the business-directory entry of every planned employee,
// keyed by worker key.
func (p *Pack) Directory() (map[string]DirectoryEntry, error) {
	directoryMu.Lock()
	defer directoryMu.Unlock()
	if cached, ok := directoryCache[p.Key]; ok {
		return cached, nil
	}
	// The plan is deterministic and the directory does not depend on the
	// tenant, so any non-nil tenant plans the same people.
	employees, err := p.Plan(uuid.NewSHA1(demoNamespace, []byte("directory\x00"+p.Key)))
	if err != nil {
		return nil, err
	}
	domain := strings.TrimSuffix(p.Key, "-demo") + ".example"
	out := make(map[string]DirectoryEntry, len(employees))
	photos := p.directoryPhotos(employees)
	for position, employee := range employees {
		row := employee.Row
		given := strings.Fields(row.PreferredName)
		family := strings.TrimSpace(strings.TrimPrefix(row.LegalName, row.PreferredName))
		if len(given) == 0 || family == "" {
			return nil, fmt.Errorf("demoworkforce: %s has no first and last name to derive an email from", row.WorkerKey)
		}
		city := strings.TrimSpace(strings.SplitN(row.Location, ",", 2)[0])
		area, ok := areaCodes[city]
		if !ok {
			return nil, fmt.Errorf("demoworkforce: no area code for location %q of %s", row.Location, row.WorkerKey)
		}
		out[row.WorkerKey] = DirectoryEntry{
			WorkerKey:  row.WorkerKey,
			Email:      slug(given[0]) + "." + strings.ReplaceAll(slug(family), "-", "") + "@" + domain,
			Phone:      fmt.Sprintf("(%s) 555-01%02d", area, (position+1)%100),
			Department: p.OrgUnitName(employee.Organization.Code),
			PhotoURL:   photos[row.WorkerKey],
		}
	}
	directoryCache[p.Key] = out
	return out, nil
}

// DirectoryEntryFor is the entry of the demo company served under tenantKey
// for one worker key, with ok false for any tenant that is not a demo company
// or any worker the company does not employ.
func DirectoryEntryFor(tenantKey, workerKey string) (DirectoryEntry, bool) {
	pack, ok := PackFor(tenantKey)
	if !ok {
		return DirectoryEntry{}, false
	}
	entries, err := pack.Directory()
	if err != nil {
		return DirectoryEntry{}, false
	}
	entry, ok := entries[strings.TrimSpace(workerKey)]
	return entry, ok
}
