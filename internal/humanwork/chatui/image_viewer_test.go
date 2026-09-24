package chatui

import "testing"

func TestChatImageViewerActualSizeControlOnlyForReducedImages(t *testing.T) {
	tests := []struct {
		name                                         string
		width, height, viewportWidth, viewportHeight int
		want                                         bool
	}{
		{name: "small original already natural", width: 160, height: 160, viewportWidth: 900, viewportHeight: 700, want: false},
		{name: "exact fit", width: 900, height: 700, viewportWidth: 900, viewportHeight: 700, want: false},
		{name: "reduced by width", width: 1800, height: 700, viewportWidth: 900, viewportHeight: 700, want: true},
		{name: "reduced by height", width: 800, height: 1400, viewportWidth: 900, viewportHeight: 700, want: true},
		{name: "unknown dimensions", width: 0, height: 160, viewportWidth: 900, viewportHeight: 700, want: false},
		{name: "unknown viewport", width: 1800, height: 700, viewportWidth: 0, viewportHeight: 700, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chatImageViewerNeedsActualSize(tt.width, tt.height, tt.viewportWidth, tt.viewportHeight)
			if got != tt.want {
				t.Fatalf("chatImageViewerNeedsActualSize(%d, %d, %d, %d) = %t, want %t", tt.width, tt.height, tt.viewportWidth, tt.viewportHeight, got, tt.want)
			}
		})
	}
}
