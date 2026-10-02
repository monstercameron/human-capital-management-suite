package demoworkforce

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	directoryEmail = regexp.MustCompile(`^[a-z]+\.[a-z]+@[a-z]+\.example$`)
	directoryPhone = regexp.MustCompile(`^\((\d{3})\) 555-01\d\d$`)
)

// TestDemoSeed_EveryEmployeeComplete loads each demo company's seed and fails
// naming any employee with a missing field or a broken manager link. Photos
// are reported rather than required: a person without a headshot is drawn
// with initials.
func TestDemoSeed_EveryEmployeeComplete(t *testing.T) {
	for _, pack := range Packs() {
		t.Run(pack.Key, func(t *testing.T) {
			employees, err := pack.Plan(uuid.NewSHA1(demoNamespace, []byte("completeness")))
			if err != nil {
				t.Fatal(err)
			}
			directory, err := pack.Directory()
			if err != nil {
				t.Fatal(err)
			}
			if len(directory) != len(employees) {
				t.Fatalf("directory holds %d people, the plan %d", len(directory), len(employees))
			}
			byKey := make(map[string]Employee, len(employees))
			for _, employee := range employees {
				byKey[employee.Row.WorkerKey] = employee
			}
			emails, phones := map[string]string{}, map[string]string{}
			tops := 0
			for _, employee := range employees {
				row := employee.Row
				who := row.LegalName + " (" + row.WorkerKey + ")"
				missing := func(field string) { t.Errorf("%s: %s is missing", who, field) }
				for field, value := range map[string]string{
					"legal name": row.LegalName, "preferred name": row.PreferredName, "worker number": row.WorkerNumber,
					"job title": row.JobTitle, "org unit": row.OrgUnit, "location": row.Location, "pay zone": row.PayZone,
					"company": row.Company, "business unit": row.BusinessUnit, "cost center": row.CostCenter,
					"hire date": row.HireDate, "lifecycle status": row.LifecycleStatus, "employment type": row.EmploymentType,
					"time type": row.TimeType, "work arrangement": row.WorkArrangement, "manager": row.ManagerRelationshipRef,
				} {
					if strings.TrimSpace(value) == "" {
						missing(field)
					}
				}
				if hired, err := time.Parse("2006-01-02", row.HireDate); err != nil || hired.After(time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC)) {
					t.Errorf("%s: hire date %q is not a past civil date", who, row.HireDate)
				}
				if row.LifecycleStatus != "active" {
					t.Errorf("%s: lifecycle status %q, want active", who, row.LifecycleStatus)
				}

				entry, ok := directory[row.WorkerKey]
				if !ok {
					missing("directory entry")
					continue
				}
				if !directoryEmail.MatchString(entry.Email) {
					t.Errorf("%s: work email %q is not first.last@%s.example", who, entry.Email, strings.TrimSuffix(pack.Key, "-demo"))
				}
				if other, dup := emails[entry.Email]; dup {
					t.Errorf("%s: work email %q is also %s's", who, entry.Email, other)
				}
				emails[entry.Email] = who
				match := directoryPhone.FindStringSubmatch(entry.Phone)
				if match == nil {
					t.Errorf("%s: work phone %q is not (area) 555-01xx", who, entry.Phone)
				} else if want := areaCodes[strings.SplitN(row.Location, ",", 2)[0]]; match[1] != want {
					t.Errorf("%s: phone area code %s does not match %s (%s)", who, match[1], row.Location, want)
				}
				if other, dup := phones[entry.Phone]; dup {
					t.Errorf("%s: work phone %q is also %s's", who, entry.Phone, other)
				}
				phones[entry.Phone] = who
				if entry.Department == "" || entry.Department == row.OrgUnit || strings.Contains(entry.Department, "-") && !strings.Contains(entry.Department, " ") {
					t.Errorf("%s: department %q is not a display name for unit %q", who, entry.Department, row.OrgUnit)
				}

				// The manager chain reaches the top without a gap or a cycle.
				if employee.ManagerKey == pack.boardRef {
					tops++
					continue
				}
				seen := map[string]bool{row.WorkerKey: true}
				for key := employee.ManagerKey; key != pack.boardRef; key = byKey[key].ManagerKey {
					if _, ok := byKey[key]; !ok {
						t.Errorf("%s: manager %q is not an employee", who, key)
						break
					}
					if seen[key] {
						t.Errorf("%s: manager chain loops at %s", who, key)
						break
					}
					seen[key] = true
				}
			}
			if tops != 1 {
				t.Errorf("%d people report to the board, want exactly the head of the company", tops)
			}

			// Every headshot a person wears is a file the repository ships.
			// The photograph the directory serves is the plan's or the company's
			// spare headshot. Ironridge, the company the owner reviews, requires
			// one for every employee; HarborCare uses every shipped headshot on
			// its own people and the rest are drawn with initials.
			without := []string{}
			for _, employee := range employees {
				who := employee.Row.LegalName
				url := directory[employee.Row.WorkerKey].PhotoURL
				if url == "" {
					without = append(without, who)
					continue
				}
				served := filepath.Join("..", "..", "humanwork", "workspace", "assets", strings.TrimPrefix(url, "/workspace/assets/"))
				if _, err := os.Stat(served); err != nil {
					t.Errorf("%s: photo %s is not a file the server ships", who, url)
				}
				name := strings.TrimSuffix(strings.TrimPrefix(url, "/workspace/assets/person-"), "-small.jpg") + ".png"
				original := filepath.Join("..", "..", "..", "demo-assets", "profile-originals", name)
				if _, err := os.Stat(original); err != nil {
					t.Errorf("%s: photo %s has no original in demo-assets/profile-originals", who, url)
				}
			}
			if pack.Key == IronridgeKey && len(without) > 0 {
				t.Errorf("%s: every employee needs a photograph, none for: %s", pack.Key, strings.Join(without, ", "))
			}
			t.Logf("%s: %d of %d people have no photo and are drawn with initials: %s", pack.Key, len(without), len(employees), strings.Join(without, ", "))
		})
	}
}

func TestDirectoryEntryForIsDemoOnly(t *testing.T) {
	entry, ok := DirectoryEntryFor(" ironridge-demo ", "ir-005-greg-novak")
	if !ok || entry.Email != "greg.novak@ironridge.example" || entry.Department != "Project Management" || entry.Phone != "(303) 555-0105" {
		t.Fatalf("Greg Novak entry = %+v, %v", entry, ok)
	}
	if _, ok := DirectoryEntryFor("another-tenant", "ir-005-greg-novak"); ok {
		t.Fatal("a non-demo tenant resolved a directory entry")
	}
	if _, ok := DirectoryEntryFor("ironridge-demo", "nobody"); ok {
		t.Fatal("an unknown worker resolved a directory entry")
	}
	if entry, _ := DirectoryEntryFor("ironridge-demo", "ir-031-mei-lin-park"); entry.Email != "mei.park@ironridge.example" {
		t.Fatalf("a two-word given name = %q", entry.Email)
	}
}
