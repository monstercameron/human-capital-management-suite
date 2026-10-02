package chat

import (
	"strings"
	"testing"
	"time"
)

func TestTodo_CHATMAP_003(t *testing.T) {
	p := chatmapPlace(time.Now())
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, dark := range []bool{false, true} {
			pic, err := (SchematicMap{}).Render(p, 16, MapSize{400, 200}, MapTheme{Locale: locale, Dark: dark})
			if err != nil || len(pic.Image) > 20000 || pic.Attribution == "" || pic.ContentType != "image/svg+xml" {
				t.Fatal(pic.Attribution, len(pic.Image), err)
			}
			svg := string(pic.Image)
			for _, want := range []string{"<circle", "<pattern", "<path", " m</text>"} {
				if !strings.Contains(svg, want) {
					t.Fatal("missing schematic element", want)
				}
			}
			if strings.Contains(svg, "href=") || strings.Contains(svg, "42.123456") || strings.Contains(svg, "-71.123456") {
				t.Fatal("external request or position leaked in picture")
			}
		}
	}
}
func TestTodo_CHATMAP_003_Fault(t *testing.T) {
	if _, err := (SchematicMap{}).Render(LocationPlace{}, 16, MapSize{400, 200}, MapTheme{}); err != ErrInvalidArgument {
		t.Fatal(err)
	}
	if _, err := (SchematicMap{}).Render(chatmapPlace(time.Now()), 99, MapSize{400, 200}, MapTheme{}); err != ErrInvalidArgument {
		t.Fatal(err)
	}
}
func TestTodo_CHATMAP_003_Performance(t *testing.T) {
	start := time.Now()
	for range 50 {
		pic, err := (SchematicMap{}).Render(chatmapPlace(start), 16, MapSize{400, 200}, MapTheme{})
		if err != nil || len(pic.Image) > 20000 {
			t.Fatal(err)
		}
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Fatal("50 schematic renders exceeded 300ms")
	}
}
func TestTodo_CHATMAP_003_Security(t *testing.T) {
	place := chatmapPlace(time.Now())
	if _, err := (SchematicMap{}).Render(place, 16, MapSize{400, 200}, MapTheme{Surface: "url(https://outside.example/pixel)"}); err != ErrInvalidArgument {
		t.Fatal("external colour request allowed", err)
	}
	if _, err := (SchematicMap{}).Render(place, 16, MapSize{400, 200}, MapTheme{PanX: 1201}); err != ErrInvalidArgument {
		t.Fatal("unbounded map pan", err)
	}
	place.Precision = "approximate"
	place.ApproximateRadius = 500
	place.Position.Accuracy = 500
	if zoom := LocationMapZoom(place, MapSize{400, 200}); zoom >= 16 || zoom < 1 {
		t.Fatal("accuracy circle does not fit", zoom)
	}
	if LocationAccuracy(place) != 1000 {
		t.Fatal("coarsening uncertainty not included")
	}
}
