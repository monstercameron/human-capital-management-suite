package demoworkforce

import (
	"testing"

	"github.com/google/uuid"
)

func TestTodo_UXBLIND_038(t *testing.T) {
	for _, pack := range []*Pack{HarborCarePack, IronridgePack} {
		employees, err := pack.Plan(uuid.NewSHA1(uuid.NameSpaceURL, []byte(pack.Key)))
		if err != nil {
			t.Fatalf("%s plan: %v", pack.Key, err)
		}
		prefix, separator, digits, start, next, err := workerNumberRule(employees)
		if err != nil {
			t.Fatalf("%s rule: %v", pack.Key, err)
		}
		if prefix == "" || separator != "-" || digits != 5 || start <= 0 || next <= start {
			t.Fatalf("%s rule = %q %q %d start=%d next=%d", pack.Key, prefix, separator, digits, start, next)
		}
		if next-start != int64(len(employees)) {
			t.Fatalf("%s rule advances %d for %d employees", pack.Key, next-start, len(employees))
		}
	}
}

func TestTodo_UXBLIND_038_Property(t *testing.T) {
	seen := map[string]string{}
	for _, pack := range []*Pack{HarborCarePack, IronridgePack} {
		employees, err := pack.Plan(uuid.NewSHA1(uuid.NameSpaceURL, []byte(pack.Key)))
		if err != nil {
			t.Fatalf("%s plan: %v", pack.Key, err)
		}
		prefix, separator, digits, _, _, err := workerNumberRule(employees)
		if err != nil {
			t.Fatalf("%s rule: %v", pack.Key, err)
		}
		for _, employee := range employees {
			if len(employee.Row.WorkerNumber) != len(prefix)+len(separator)+digits {
				t.Fatalf("%s worker %s breaks the seeded width", pack.Key, employee.Row.WorkerNumber)
			}
			if employee.Row.WorkerNumber[:len(prefix)] != prefix || employee.Row.WorkerNumber[len(prefix):len(prefix)+len(separator)] != separator {
				t.Fatalf("%s worker %s breaks the seeded prefix", pack.Key, employee.Row.WorkerNumber)
			}
			if prior, exists := seen[employee.Row.WorkerNumber]; exists {
				t.Fatalf("worker number %s collides between %s and %s", employee.Row.WorkerNumber, prior, pack.Key)
			}
			seen[employee.Row.WorkerNumber] = pack.Key
		}
	}
}
