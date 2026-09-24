// Command hcmctl is the SVC-011/ADMIN-001 operator CLI's composition root.
// internal/transport/admin/hcmctl is the whole implementation - flag
// parsing, JIT token minting, bearer redaction, exactly one AdminService
// call per invocation and its evidence line - as a testable library. This
// file wires that library to the process boundary and routes the read-only
// recovery matrix command to its operator package. Business and store logic
// remain in those packages; this is only command composition.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/recovery/opcmd"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin/hcmctl"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, hcmctl.DialInsecure))
}

// run keeps recovery policy inspection in the real operator binary while
// exposing only its read-only matrix command. Restore and game-day actions
// retain their separately reviewed operational entry points.
func run(args []string, stdout, stderr io.Writer, dial hcmctl.Dialer) int {
	if len(args) > 0 && args[0] == "recovery" {
		if len(args) != 2 || args[1] != "matrix" {
			fmt.Fprintln(stderr, "hcmctl: supported recovery command: recovery matrix")
			return 2
		}
		return opcmd.Main([]string{"matrix"}, stdout, stderr)
	}
	return hcmctl.Main(args, stdout, stderr, dial)
}
