package defaultproduct

import (
	"strings"
	"testing"
)

var alignAllCapabilities = []string{"product.view", "promotion.view", "promotion.execute", "operations.repair"}

// TestTodo_ALIGN_042 proves the application shell resolves against
// authorization: admitted entries render, unadmitted entries never do.
func TestTodo_ALIGN_042(t *testing.T) {
	entries := DefaultShellEntries()
	full, err := ResolveShell(entries, alignAllCapabilities)
	if err != nil {
		t.Fatalf("ResolveShell(full): %v", err)
	}
	if len(full.Entries) != len(entries) {
		t.Fatalf("resolved %d of %d entries for full grants", len(full.Entries), len(entries))
	}
	// One grant renders exactly its entry.
	narrow, err := ResolveShell(entries, []string{"product.view"})
	if err != nil {
		t.Fatalf("ResolveShell(narrow): %v", err)
	}
	if len(narrow.Entries) != 1 || narrow.Entries[0].ID != "shell.nav.home" {
		t.Fatalf("narrow shell = %+v", narrow.Entries)
	}
}

func TestTodo_ALIGN_042_Property(t *testing.T) {
	entries := DefaultShellEntries()
	first, err := ResolveShell(entries, alignAllCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	// Grant order never changes the resolved shell.
	reversed := []string{alignAllCapabilities[3], alignAllCapabilities[2], alignAllCapabilities[1], alignAllCapabilities[0]}
	second, err := ResolveShell(entries, reversed)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("shell resolution is not order-independent: %s != %s", first.Digest, second.Digest)
	}
}

func TestTodo_ALIGN_042_Golden(t *testing.T) {
	shell, err := ResolveShell(DefaultShellEntries(), alignAllCapabilities)
	if err != nil {
		t.Fatalf("ResolveShell: %v", err)
	}
	const wantDigest = "sha256:d08ec24f41184eb1231d260ddf5035c97524014a6117765cb701023dc882ecfe"
	if shell.Digest != wantDigest {
		t.Fatalf("shell digest=%q want=%q", shell.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_042_Security(t *testing.T) {
	entries := DefaultShellEntries()
	// The execute entry requires its capability: without the grant it
	// never renders, and no grant string leaks into the shell.
	resolved, err := ResolveShell(entries, []string{"product.view", "promotion.view"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range resolved.Entries {
		if entry.RequiredCapability == "promotion.execute" || entry.RequiredCapability == "operations.repair" {
			t.Fatalf("unadmitted entry rendered: %+v", entry)
		}
	}
	// No grants render nothing, and the empty shell still digests.
	empty, err := ResolveShell(entries, nil)
	if err != nil {
		t.Fatalf("ResolveShell(nil): %v", err)
	}
	if len(empty.Entries) != 0 || empty.Digest == "" {
		t.Fatalf("empty shell = %+v", empty)
	}
	// A forged capability grant cannot conjure an entry.
	forged, err := ResolveShell(entries, []string{"attacker.superuser"})
	if err != nil {
		t.Fatal(err)
	}
	if len(forged.Entries) != 0 {
		t.Fatalf("forged grant rendered %+v", forged.Entries)
	}
}

func TestTodo_ALIGN_042_Conformance(t *testing.T) {
	entries := DefaultShellEntries()
	// The seed requires exactly the capabilities the product contract
	// grants: no more, no fewer.
	capabilities := ShellCapabilities(entries)
	want := []string{"operations.repair", "product.view", "promotion.execute", "promotion.view"}
	if len(capabilities) != len(want) {
		t.Fatalf("shell capabilities = %v", capabilities)
	}
	for i, capability := range capabilities {
		if capability != want[i] {
			t.Fatalf("shell capabilities = %v, want %v", capabilities, want)
		}
	}
	// Every resolved entry is renderable: labeled with an absolute route.
	resolved, err := ResolveShell(entries, alignAllCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range resolved.Entries {
		if entry.Label == "" || !strings.HasPrefix(entry.Route, "/") {
			t.Fatalf("entry %+v is not renderable", entry)
		}
	}
}

func FuzzTodo_ALIGN_042_Fuzz(f *testing.F) {
	f.Add("promotion.execute")
	f.Fuzz(func(t *testing.T, capability string) {
		first, firstErr := ResolveShell(DefaultShellEntries(), []string{capability})
		second, secondErr := ResolveShell(DefaultShellEntries(), []string{capability})
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("resolution is not deterministic for %q", capability)
		}
		if firstErr == nil && first.Digest != second.Digest {
			t.Fatalf("shell digest is not deterministic for %q", capability)
		}
	})
}
