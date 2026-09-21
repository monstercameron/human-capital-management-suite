package preferences

import (
	"math/rand"
	"testing"
)

// TestTodo_REV_092_01_Property: whatever a client sends, a stored personal
// density is either one of the three admitted presets or "" (inherit), the
// normalization is idempotent, and NormalizeUser applies it without touching
// the organization theme's density.
func TestTodo_REV_092_01_Property(t *testing.T) {
	admitted := map[string]bool{"": true, DensityCompact: true, DensityComfortable: true, DensitySpacious: true}
	alphabet := []rune("compactCOMFORTABLEspaciou -_\t9é")
	rng := rand.New(rand.NewSource(92011))
	inputs := []string{"compact", " Compact ", "SPACIOUS", "comfortable", "", "ultra-compact", "compact;", "0.5", "dense"}
	for i := 0; i < 3000; i++ {
		runes := make([]rune, rng.Intn(14))
		for j := range runes {
			runes[j] = alphabet[rng.Intn(len(alphabet))]
		}
		inputs = append(inputs, string(runes))
	}
	for _, input := range inputs {
		got := NormalizeUserDensity(input)
		if !admitted[got] {
			t.Fatalf("NormalizeUserDensity(%q) = %q, outside the admitted set", input, got)
		}
		if again := NormalizeUserDensity(got); again != got {
			t.Fatalf("NormalizeUserDensity is not idempotent for %q: %q then %q", input, got, again)
		}
		user := NormalizeUser(User{Density: input})
		if user.Density != got {
			t.Fatalf("NormalizeUser kept density %q for input %q, want %q", user.Density, input, got)
		}
	}
	for input, want := range map[string]string{" Compact ": "compact", "SPACIOUS": "spacious", "ultra-compact": "", "": ""} {
		if got := NormalizeUserDensity(input); got != want {
			t.Fatalf("NormalizeUserDensity(%q) = %q, want %q", input, got, want)
		}
	}
	// The organization default is untouched by a personal choice.
	snapshot := DefaultSnapshot()
	snapshot.User = NormalizeUser(User{Density: DensityCompact})
	if snapshot.Theme.Density != DensityComfortable {
		t.Fatalf("organization density changed to %q", snapshot.Theme.Density)
	}
}
