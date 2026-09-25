package toolinventory_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/toolinventory"
)

// TestTodo_TOOL_025_Mutation proves the manifest's tamper-detection
// oracles actually fire on the two mutation classes RED describes that
// TestToolchainSupplyChainManifestRejectsUntrackedToolInput's own
// missing-field mutations do not cover: a digest that no longer matches
// what regenerating from source produces (VerifyDigests), and a
// duplicated tool name (Validate). A test suite that could never observe
// these mutations would be passing vacuously.
func TestTodo_TOOL_025_Mutation(t *testing.T) {
	root := repoRoot(t)
	m, err := toolinventory.Generate(root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := toolinventory.VerifyDigests(root, m); err != nil {
		t.Fatalf("expected the freshly generated manifest's own digests to verify clean: %v", err)
	}

	t.Run("a_tampered_digest_is_caught", func(t *testing.T) {
		tampered := m
		tampered.Tools = append([]toolinventory.Entry{}, m.Tools...)
		i := digestedEntryIndex(t, tampered)
		tampered.Tools[i].Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		if err := toolinventory.VerifyDigests(root, tampered); err == nil {
			t.Fatalf("expected VerifyDigests to reject a tampered digest on %q", tampered.Tools[i].Name)
		}
	})

	t.Run("a_tampered_platform_binary_digest_is_caught", func(t *testing.T) {
		tampered := m
		tampered.Tools = append([]toolinventory.Entry{}, m.Tools...)
		for i := range tampered.Tools {
			if tampered.Tools[i].Name != "buf" {
				continue
			}
			digests := make(map[string]string, len(tampered.Tools[i].PlatformDigests))
			for platform, digest := range tampered.Tools[i].PlatformDigests {
				digests[platform] = digest
			}
			digests["windows/amd64"] = "0000000000000000000000000000000000000000000000000000000000000000"
			tampered.Tools[i].PlatformDigests = digests
		}
		if err := toolinventory.VerifyDigests(root, tampered); err == nil {
			t.Fatal("expected VerifyDigests to reject a tampered Buf platform digest")
		}
	})

	t.Run("a_renamed_entry_is_caught", func(t *testing.T) {
		tampered := m
		tampered.Tools = append([]toolinventory.Entry{}, m.Tools...)
		tampered.Tools[0].Name = "not-a-real-tool"
		if err := toolinventory.VerifyDigests(root, tampered); err == nil {
			t.Fatal("expected VerifyDigests to reject an entry the current generator does not produce")
		}
	})

	t.Run("a_duplicate_tool_name_is_rejected_by_validate", func(t *testing.T) {
		tampered := m
		tampered.Tools = append([]toolinventory.Entry{}, m.Tools...)
		tampered.Tools = append(tampered.Tools, tampered.Tools[0])
		if err := tampered.Validate(); err == nil {
			t.Fatal("expected Validate to reject a duplicate tool name")
		}
	})
}

// digestedEntryIndex returns the index of an entry that actually carries
// a digest (some npm-derived entries legitimately do not - see
// loadPackageLockEntries), failing the test outright if none do, since
// that would silently make the tampered-digest case above meaningless.
func digestedEntryIndex(t *testing.T, m toolinventory.Manifest) int {
	t.Helper()
	for i, e := range m.Tools {
		if e.Digest != "" {
			return i
		}
	}
	t.Fatal("no entry in the generated manifest carries a digest")
	return -1
}
