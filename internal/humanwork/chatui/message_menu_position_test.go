package chatui

import "testing"

func TestMessageMenuPositionWithinScrollViewport(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		anchorTop, anchorBottom, menuHeight float64
		boundTop, boundBottom               float64
	}{
		{"near bottom flips above", 590, 630, 120, 80, 670},
		{"near top opens below", 100, 140, 120, 80, 670},
		{"short viewport caps height", 145, 185, 400, 80, 260},
	} {
		t.Run(tc.name, func(t *testing.T) {
			top, height := menuTop(tc.anchorTop, tc.anchorBottom, tc.menuHeight, tc.boundTop, tc.boundBottom)
			if top < tc.boundTop-0.01 || top+height > tc.boundBottom+0.01 || height <= 0 {
				t.Fatalf("menu top %.1f height %.1f outside [%.1f,%.1f]", top, height, tc.boundTop, tc.boundBottom)
			}
		})
	}
}
