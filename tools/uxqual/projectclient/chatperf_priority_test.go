package projectclient

import (
	"os"
	"strings"
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
)

// TestTodo_CHATBUG_014_PriorityOptions: the priority ids are the enum's own
// names, and they are not asked of protobuf reflection while the package
// initializes (which built the whole project descriptor before any page of the
// browser client could start).
func TestTodo_CHATBUG_014_PriorityOptions(t *testing.T) {
	want := []projectv1.TaskPriority{
		projectv1.TaskPriority_TASK_PRIORITY_LOW, projectv1.TaskPriority_TASK_PRIORITY_NORMAL,
		projectv1.TaskPriority_TASK_PRIORITY_HIGH, projectv1.TaskPriority_TASK_PRIORITY_URGENT,
	}
	if len(PriorityOptions) != len(want) {
		t.Fatalf("%d priorities, want %d", len(PriorityOptions), len(want))
	}
	for i, priority := range want {
		if PriorityOptions[i].ID == "" || PriorityOptions[i].ID != priority.String() {
			t.Errorf("priority %d has id %q, want the enum's own name %q", i, PriorityOptions[i].ID, priority.String())
		}
	}
	if got := priorityID(projectv1.TaskPriority(999)); got != "" {
		t.Errorf("an unknown priority was named %q", got)
	}

	raw, err := os.ReadFile("detail.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "var PriorityOptions = ")
	if start < 0 {
		t.Fatal("PriorityOptions moved")
	}
	table := source[start : start+strings.Index(source[start:], "\n}\n")]
	if strings.Contains(table, ".String()") {
		t.Fatal("PriorityOptions calls String() while the package initializes: every page pays for the project descriptor at start-up again")
	}
}
