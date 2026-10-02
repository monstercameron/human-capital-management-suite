package agenticon

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var fixedAgents = []Input{
	{Name: "Policy Helper", Instructions: "Read the policy handbook and guide employees."},
	{Name: "Assistant", Instructions: "Assist with everyday questions."},
	{Name: "Birthday agent", Instructions: "Celebrate every birthday and anniversary."},
	{Name: "Support agent", Instructions: "Resolve customer support tickets."},
	{Name: "Reminder agent", Instructions: "Remind people before a deadline."},
	{Name: "To-do agent", Instructions: "Track tasks and checklists."},
}

func TestAgentUXIcon_Generated(t *testing.T) {
	for i, want := range []string{"book", "spark", "cake", "headset", "bell", "checklist"} {
		if got := Generate(fixedAgents[i]); got.Glyph != want {
			t.Errorf("%s: %+v, want %s", fixedAgents[i].Name, got, want)
		}
	}
	if len(Glyphs()) < 61 {
		t.Fatal("need sixty semantic glyphs and a neutral default")
	}
	if Generate(Input{}).Glyph != "neutral" {
		t.Fatal("empty input needs neutral default")
	}
	for _, g := range Glyphs()[1:] {
		for _, vocabulary := range g.Keywords {
			if vocabulary == "" {
				t.Fatal(g.ID, "missing localized keywords")
			}
			keyword := strings.Split(vocabulary, ",")[0]
			if got := Generate(Input{Name: keyword}); got.Glyph != g.ID {
				t.Errorf("keyword %s selected %s instead of %s", keyword, got.Glyph, g.ID)
			}
		}
	}
	if got := Generate(Input{Name: "birthday", Instructions: strings.Repeat("policy ", 200)}); got.Glyph != "cake" {
		t.Fatal("name must outweigh instructions", got)
	}
}

func TestAgentUXIcon_Generated_Property(t *testing.T) {
	random := rand.New(rand.NewSource(69))
	inputs := []string{"", "\xff\x00", strings.Repeat("مرحبا 世界 🐱 ", 100000)}
	for i := 0; i < 200; i++ {
		b := make([]byte, random.Intn(1024))
		_, _ = random.Read(b)
		inputs = append(inputs, string(b))
	}
	for _, text := range inputs {
		input := Input{Name: text, Description: text, Instructions: text, Skills: []string{text}}
		value := Generate(input)
		if value != Generate(input) || !value.Valid() {
			t.Fatal("not deterministic/valid", value)
		}
		svg := SVG(value)
		if len(svg) > 1024 || strings.Contains(svg, "<title") || strings.Contains(svg, "<script") {
			t.Fatal("unbounded/unsafe drawing")
		}
		if !strings.Contains(svg, "var("+value.Foreground+")") || !strings.Contains(svg, "var("+value.Background+")") {
			t.Fatal("non-token colour")
		}
	}
	// Agents in one workspace never share a glyph while an unused one remains.
	var used []Value
	glyphsSeen := map[string]bool{}
	for i := 0; i < len(Glyphs()); i++ {
		value, ok := Unique(fixedAgents[0], used, 0)
		if !ok || glyphsSeen[value.Glyph] {
			t.Fatal("agents share a glyph while unused glyphs remain", i, value)
		}
		glyphsSeen[value.Glyph] = true
		used = append(used, value)
	}
	// With every glyph taken, the preferred glyph is reused on another shape
	// and colour until those run out too; only then is exhaustion reported.
	for i := 0; i < Variations; i++ {
		used = append(used, GenerateVariation(fixedAgents[0], uint32(i)))
	}
	if _, ok := Unique(fixedAgents[0], used, 0); ok {
		t.Fatal("exhaustion unreported")
	}
	current := Generate(fixedAgents[0])
	next, ok := Shuffle(current, nil)
	if !ok || next.Glyph != current.Glyph || next == current || next.Variation != 1 {
		t.Fatal("shuffle changed identity meaning", current, next, ok)
	}
	if _, ok := Shuffle(current, used); ok {
		t.Fatal("shuffle exhaustion unreported")
	}
	if value, ok := Shuffle(Value{}, nil); !ok || !value.Valid() {
		t.Fatal("invalid shuffle input", value, ok)
	}
	for _, g := range Glyphs() {
		for v := uint32(0); v < Variations; v++ {
			value := GenerateVariation(Input{Name: g.Keywords[0]}, v)
			if !value.Valid() || len(SVG(value)) > 1024 {
				t.Fatal("catalog drawing invalid", g.ID, value)
			}
		}
	}
	if Generate(Input{Name: " A  B ", Instructions: " X\n Y"}) != Generate(Input{Name: "a b", Instructions: "x y"}) {
		t.Fatal("normalization differs")
	}
}

func TestAgentUXIcon_Generated_Security(t *testing.T) {
	attack := `<script>alert("instruction-secret")</script><svg onload="steal()">&"'`
	for _, value := range []Value{Generate(Input{Name: attack, Instructions: attack}), {Glyph: attack, Shape: attack, Foreground: attack, Background: attack, Variation: 999}} {
		for _, svg := range []string{SVG(value), renderIcon(t, value)} {
			for _, forbidden := range []string{"script", "instruction-secret", "onload", "steal", "&quot;"} {
				if strings.Contains(svg, forbidden) {
					t.Fatal("source or forged data leaked", svg)
				}
			}
		}
	}
}

func renderIcon(t *testing.T, value Value) string {
	t.Helper()
	markup, err := ui.RenderToString(Node(value))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestAgentUXIcon_Generated_Accessibility(t *testing.T) {
	for _, agent := range fixedAgents {
		for _, svg := range []string{SVG(Generate(agent)), renderIcon(t, Generate(agent))} {
			if !strings.Contains(svg, `aria-hidden="true"`) || !strings.Contains(svg, `focusable="false"`) || strings.Contains(svg, "<title") {
				t.Fatal("decorative semantics missing", svg)
			}
		}
	}
	lightRaw, err := os.ReadFile("../productui/theme.go")
	if err != nil {
		t.Fatal(err)
	}
	darkRaw, err := os.ReadFile("../productui/uipolish005_theme.go")
	if err != nil {
		t.Fatal(err)
	}
	light, dark := map[string]string{}, map[string]string{}
	for _, match := range regexp.MustCompile(`Name: "([^"]+)", CSSVariable: "([^"]+)", Kind: ThemeColor, Default: "(#[0-9a-f]+)"`).FindAllStringSubmatch(string(lightRaw), -1) {
		light[match[2]] = match[3]
		for _, d := range regexp.MustCompile(`"`+regexp.QuoteMeta(match[1])+`":\s*"(#[0-9a-f]+)"`).FindAllStringSubmatch(string(darkRaw), -1) {
			dark[match[2]] = d[1]
		}
	}
	for mode, values := range map[string]map[string]string{"light": light, "dark": dark} {
		for _, pair := range ColourPairs() {
			fg, bg := values[pair.Foreground], values[pair.Background]
			if fg == "" || bg == "" {
				t.Fatal("missing theme token", mode, pair)
			}
			a, b := luminance(fg), luminance(bg)
			if a < b {
				a, b = b, a
			}
			if ratio := (a + .05) / (b + .05); ratio < 4.5 {
				t.Errorf("%s %v contrast %.2f < 4.5", mode, pair, ratio)
			}
		}
	}
}

func luminance(hex string) float64 {
	result := 0.0
	for i, weight := range []float64{.2126, .7152, .0722} {
		n, _ := strconv.ParseUint(hex[1+i*2:3+i*2], 16, 8)
		v := float64(n) / 255
		if v <= .04045 {
			v /= 12.92
		} else {
			v = math.Pow((v+.055)/1.055, 2.4)
		}
		result += v * weight
	}
	return result
}

func TestAgentUXIcon_Generated_Golden(t *testing.T) {
	wants := []string{"2dd87c166a240c17592c26ef37b9bad438b49c8644fca75c4f456126d47bda4b", "288586b50659b5767d387762940b695fd48eba25baeb197e6c79bc13c7b330de", "fcb431c7ade4269b1508556d85aeee7597d55d5f7ccc62520b75a2e30480e143", "7c3bd4131174ef66db6169fd17b3b03a9c19c1034c434387b2889f6ebfd156e3", "96f086f9dc3a750daea902b14c09cb5f65fea04b73d6167182bf5388f2a8d8ee", "ea04d716ab5a7f056c76f12099feae27e72ec06d130bade0f4ac79b8015e7462"}
	for i, agent := range fixedAgents {
		svg := SVG(Generate(agent))
		digest := sha256.Sum256([]byte(svg))
		got := hex.EncodeToString(digest[:])
		if got != wants[i] {
			t.Errorf("%s %+v SVG digest: %s", agent.Name, Generate(agent), got)
		}
	}
}

func TestAgentUXIcon_Generated_Browser(t *testing.T) {
	for _, g := range Glyphs() {
		value := Generate(Input{})
		value.Glyph = g.ID
		markup := renderIcon(t, value)
		if !strings.Contains(markup, `viewBox="0 0 24 24"`) || !strings.Contains(markup, g.Path) || strings.Contains(markup, "title=") {
			t.Fatal("component lost vector/semantics", g.ID)
		}
	}
}
