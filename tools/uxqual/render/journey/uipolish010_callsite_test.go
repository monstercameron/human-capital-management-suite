package journey

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_010_NoProductionStatusIconBypass(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	for _, name := range []string{"page_components.go", "components.go", "people.go"} {
		contents, err := os.ReadFile(filepath.Join(filepath.Dir(filename), name))
		if err != nil {
			t.Fatal(err)
		}
		for _, direct := range []string{"iconInfo(", "iconWarning(", "iconDanger(", "iconSuccess("} {
			if strings.Contains(string(contents), direct) {
				t.Errorf("%s retains direct production status icon %s", name, direct)
			}
		}
	}
}
