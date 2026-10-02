//go:build !(js && wasm)

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The size gate (performance_budget.go) refuses a bundle only once it is over
// its ceiling, which is too late: the change that crosses it is whichever one
// happens to land last, and it then blocks every writer after it. These floors
// fail the build of the current sources while there is still room, so the
// overrun is dealt with by the change that is eating the room.
const (
	// minClientRawHeadroom is how far journey.wasm must stay below
	// maxJourneyWasmBytes.
	minClientRawHeadroom int64 = 1 << 20
	// minClientGzipHeadroom is the same margin for the transferred size,
	// against maxJourneyWasmGzipBytes. It is smaller because the compressed
	// ceiling is 12 MiB and the module compresses about five to one.
	minClientGzipHeadroom int64 = 256 << 10
)

// clientBundleHeadroomProblems returns one sentence per ceiling that has less
// room than its floor, empty when both have enough.
func clientBundleHeadroomProblems(rawBytes, gzipBytes int64) []string {
	var problems []string
	if room := maxJourneyWasmBytes - rawBytes; room < minClientRawHeadroom {
		problems = append(problems, fmt.Sprintf("%s is %d bytes, leaving %d bytes (%.2f MiB) below its %d-byte ceiling; the floor is %d bytes (%.2f MiB)",
			wasmFile, rawBytes, room, mib(room), maxJourneyWasmBytes, minClientRawHeadroom, mib(minClientRawHeadroom)))
	}
	if room := maxJourneyWasmGzipBytes - gzipBytes; room < minClientGzipHeadroom {
		problems = append(problems, fmt.Sprintf("%s.gz is %d bytes, leaving %d bytes (%.2f MiB) below its %d-byte ceiling; the floor is %d bytes (%.2f MiB)",
			wasmFile, gzipBytes, room, mib(room), maxJourneyWasmGzipBytes, minClientGzipHeadroom, mib(minClientGzipHeadroom)))
	}
	return problems
}

func mib(bytes int64) float64 { return float64(bytes) / (1 << 20) }

// TestClientBundleHeadroomVerdict pins the arithmetic the headroom test below
// relies on, without compiling anything.
func TestClientBundleHeadroomVerdict(t *testing.T) {
	roomy := maxJourneyWasmBytes - minClientRawHeadroom
	roomyGzip := maxJourneyWasmGzipBytes - minClientGzipHeadroom
	if problems := clientBundleHeadroomProblems(roomy, roomyGzip); len(problems) != 0 {
		t.Fatalf("exactly the floor must pass, got %v", problems)
	}
	problems := clientBundleHeadroomProblems(roomy+1, roomyGzip+1)
	if len(problems) != 2 || !strings.Contains(problems[0], wasmFile+" is") || !strings.Contains(problems[1], wasmFile+".gz is") {
		t.Fatalf("one byte under the floor must name both files, got %v", problems)
	}
	if problems := clientBundleHeadroomProblems(maxJourneyWasmBytes+1, 1); len(problems) != 1 || !strings.Contains(problems[0], "leaving -1 bytes") {
		t.Fatalf("an over-ceiling bundle must report negative room, got %v", problems)
	}
}

// TestClientBundleHeadroom builds the client from the current sources exactly
// as the gate does, prints its sizes, and fails when either ceiling has less
// than its floor of room left. The ceilings themselves are not touched here:
// the answer to a failure is a smaller client, not a larger ceiling.
func TestClientBundleHeadroom(t *testing.T) {
	if maxJourneyWasmBytes != 60<<20 || maxJourneyWasmGzipBytes != 12<<20 {
		t.Fatalf("the size ceilings were changed: %d and %d", maxJourneyWasmBytes, maxJourneyWasmGzipBytes)
	}
	if testing.Short() {
		t.Skip("compiling a wasm module takes tens of seconds; skipped under -short")
	}
	if _, err := goBinary(); err != nil {
		t.Skipf("no go toolchain available to build with: %v", err)
	}
	out := t.TempDir()
	var stdout bytes.Buffer
	// build compresses and then checks the ceilings, so an over-ceiling bundle
	// still leaves both files behind to be measured and reported.
	buildErr := build(out, &stdout)
	sizes := map[string]int64{}
	for _, name := range []string{wasmFile, wasmFile + ".gz"} {
		info, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("cannot measure the client bundle, the build produced no %s: %v", name, buildErr)
		}
		sizes[name] = info.Size()
	}
	raw, gzip := sizes[wasmFile], sizes[wasmFile+".gz"]
	t.Logf("%s: %d bytes (%.2f MiB), %d bytes (%.2f MiB) below the %d-byte ceiling", wasmFile, raw, mib(raw), maxJourneyWasmBytes-raw, mib(maxJourneyWasmBytes-raw), maxJourneyWasmBytes)
	t.Logf("%s.gz: %d bytes (%.2f MiB), %d bytes (%.2f MiB) below the %d-byte ceiling", wasmFile, gzip, mib(gzip), maxJourneyWasmGzipBytes-gzip, mib(maxJourneyWasmGzipBytes-gzip), maxJourneyWasmGzipBytes)
	if problems := clientBundleHeadroomProblems(raw, gzip); len(problems) != 0 {
		t.Fatalf("the browser client is running out of room:\n  %s\n"+
			"Do not raise the ceilings in performance_budget.go. Make the client smaller: move large static tables of text or style out of compiled code into data parsed at use, and look for packages one call pulls in. "+
			"Measure by building with GOOS=js GOARCH=wasm without -s and reading the name section of the module by package and function-name prefix.",
			strings.Join(problems, "\n  "))
	}
}
