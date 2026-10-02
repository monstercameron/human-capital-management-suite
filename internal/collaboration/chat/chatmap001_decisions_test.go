package chat

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestTodo_CHATMAP_001(t *testing.T) {
	ds := LocationDecisions()
	if len(ds) != 6 {
		t.Fatal("the research record answers six questions", len(ds))
	}
	for i, d := range ds {
		if d.Question != i+1 || len(d.Decision) < 200 {
			t.Fatal("question not answered", d.Question, d.Topic)
		}
	}
	for _, question := range []int{1, 3, 4, 5} {
		if len(ds[question-1].Sources) == 0 {
			t.Fatal("no source named for question", question)
		}
	}
	for _, question := range []int{1, 3} {
		if ds[question-1].NeedsOwner == "" {
			t.Fatal("a data source the owner must approve is not flagged", question)
		}
	}
	text := RenderLocationDecisions()
	for _, want := range []string{"own origin", "OpenStreetMap", "never evidence for time", "worker's own act", "no route history", "never lengthen"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Fatal("decision missing", want)
		}
	}
	if DefaultLocationPolicy().MaxRetention != LocationHardMaxDuration {
		t.Fatal("recorded retention ceiling is not the one enforced")
	}
}

func TestTodo_CHATMAP_001_Golden(t *testing.T) {
	const golden = "testdata/chatmap001_decisions.golden"
	got := RenderLocationDecisions()
	if os.Getenv("CHATMAP_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Fatal("decision record changed; review it and regenerate with CHATMAP_GOLDEN_UPDATE=1")
	}
}

func TestTodo_CHATMAP_001_Security(t *testing.T) {
	csp, err := os.ReadFile("../../humanwork/workspace/csp.go")
	if err != nil {
		t.Fatal(err)
	}
	policy := strings.ToLower(string(csp))
	if !strings.Contains(policy, "frame-src 'none'") {
		t.Fatal("the page may now frame another origin")
	}
	for _, host := range []string{"openstreetmap", "mapbox", "googleapis.com/maps", "maps.google", "tile.", "here.com", "arcgis", "bing.com/maps"} {
		if strings.Contains(policy, host) {
			t.Fatal("the page security policy names a map host", host)
		}
	}
	for _, d := range LocationDecisions() {
		if strings.Contains(d.Decision, "http://") || strings.Contains(d.Decision, "https://") {
			t.Fatal("a decision depends on an address outside the product", d.Topic)
		}
	}
}

func TestTodo_CHATMAP_001_Performance(t *testing.T) {
	// Fifty embeds' pictures: each under 60 KB and drawn well inside the 300 ms
	// budget at the 95th percentile. Tile composition is not measured because
	// the repository holds no tile data.
	var times []time.Duration
	place := chatmapPlace(time.Now())
	for i := 0; i < 200; i++ {
		start := time.Now()
		pic, err := (SchematicMap{}).Render(place, 16, MapSize{Width: 400, Height: 200}, MapTheme{Locale: "en-US", Dark: i%2 == 0})
		times = append(times, time.Since(start))
		if err != nil || len(pic.Image) > 60*1024 {
			t.Fatal("picture over budget", len(pic.Image), err)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	if p95 := times[len(times)*95/100]; p95 > 300*time.Millisecond {
		t.Fatal("95th percentile render time", p95)
	}
}
