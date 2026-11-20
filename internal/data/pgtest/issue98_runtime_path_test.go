package pgtest

import (
	"strings"
	"testing"
)

// Issue #98: a cluster whose temporary fileset path passes MAX_PATH makes
// PostgreSQL fail with SQLSTATE 58P01 on Windows. The runtime directory must
// move to a short root when the temp directory is long.
func TestIssue98_RuntimeBaseStaysShortOnWindows(t *testing.T) {
	long := `C:\Users\someone\Desktop\a-very-long-candidate-checkout-name\.artifacts\verified-candidate-reruns\fresh-isolated-paths\postgres\tmp-0001\x`
	got := runtimeBase("windows", long)
	if got == long {
		t.Fatalf("long temp dir %d chars kept as runtime base", len(long))
	}
	fileset := got + `\hcmnext-pg-1234567890\data\base\pgsql_tmp\pgsql_tmp4194304.0.fileset`
	if len(fileset)+20 >= 260 {
		t.Fatalf("fileset path still %d chars: %s", len(fileset), fileset)
	}
	if !strings.HasPrefix(got, `C:\`) {
		t.Fatalf("short base left the volume: %s", got)
	}
}

func TestIssue98_RuntimeBaseKeepsShortTempAndOtherSystems(t *testing.T) {
	short := `C:\Users\me\AppData\Local\Temp`
	if got := runtimeBase("windows", short); got != short {
		t.Fatalf("short temp dir moved to %s", got)
	}
	long := "/" + strings.Repeat("deep/", 60)
	if got := runtimeBase("linux", long); got != long {
		t.Fatalf("non-windows base moved to %s", got)
	}
	unc := `\server\share\` + strings.Repeat("deep\\", 40)
	if got := runtimeBase("windows", unc); got != unc {
		t.Fatalf("UNC base moved to %s", got)
	}
}
