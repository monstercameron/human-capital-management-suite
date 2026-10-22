package semanticversion_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/semanticversion"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestTodo_CHATBUG_014_SemanticVersionLeaf holds the leaf the browser client
// imports to the functions internal/workflow/version still exports.
func TestTodo_CHATBUG_014_SemanticVersionLeaf(t *testing.T) {
	for _, value := range []string{"0.1.0", "1.0.0-alpha.1", "1.0.0+build.9", "", "v1.0.0", "1", "1.0", "01.0.0", "1.0.0 ", "9.99.999"} {
		leaf, wrapped := semanticversion.Validate(value), version.ValidateSemanticVersion(value)
		if (leaf == nil) != (wrapped == nil) || leaf != nil && leaf.Error() != wrapped.Error() {
			t.Errorf("Validate(%q) = %v, version.ValidateSemanticVersion = %v", value, leaf, wrapped)
		}
		nextLeaf, errLeaf := semanticversion.NextPatch(value)
		nextWrapped, errWrapped := version.NextPatchVersion(value)
		if nextLeaf != nextWrapped || (errLeaf == nil) != (errWrapped == nil) {
			t.Errorf("NextPatch(%q) = %q, %v; version.NextPatchVersion = %q, %v", value, nextLeaf, errLeaf, nextWrapped, errWrapped)
		}
	}
	for _, test := range []struct{ value, want string }{{"1.4.0", "1.4.1"}, {"1.4.9", "1.4.10"}, {"1.0.99-rc.1+b", "1.0.100"}, {"2.0.999", "2.0.1000"}} {
		if got, err := semanticversion.NextPatch(test.value); err != nil || got != test.want {
			t.Errorf("NextPatch(%q) = %q, %v, want %q", test.value, got, err, test.want)
		}
	}
	for _, test := range []struct {
		left, right string
		want        int
	}{{"1.10.0", "1.9.9", 1}, {"2.0.0-alpha", "2.0.0", -1}, {"1.0.0+one", "1.0.0+two", 0}} {
		got, err := semanticversion.Compare(test.left, test.right)
		wrapped, _ := version.CompareSemanticVersions(test.left, test.right)
		if err != nil || got != test.want || got != wrapped {
			t.Errorf("Compare(%q, %q) = %d, %v, want %d (version: %d)", test.left, test.right, got, err, test.want, wrapped)
		}
	}
	if _, err := semanticversion.Compare("1.0", "1.0.0"); err == nil {
		t.Error("Compare accepted an invalid version")
	}
}
