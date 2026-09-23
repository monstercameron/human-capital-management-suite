package chatui

import (
	"net/url"
	"testing"
)

func TestValidChatRenditionURL(t *testing.T) {
	tests := []struct {
		name, raw, variant string
		want               bool
	}{
		{"thumbnail", "/v1/chat/media/media_123?grant=opaque&variant=thumbnail", "thumbnail", true},
		{"display", "/v1/chat/media/media_123?grant=opaque&variant=display", "display", true},
		{"wrong variant", "/v1/chat/media/media_123?grant=opaque&variant=thumbnail", "display", false},
		{"missing grant", "/v1/chat/media/media_123?variant=thumbnail", "thumbnail", false},
		{"duplicate grant", "/v1/chat/media/media_123?grant=one&grant=two&variant=thumbnail", "thumbnail", false},
		{"duplicate variant", "/v1/chat/media/media_123?grant=opaque&variant=thumbnail&variant=display", "thumbnail", false},
		{"absolute host", "https://elsewhere.test/v1/chat/media/media_123?grant=opaque&variant=thumbnail", "thumbnail", false},
		{"foreign path", "/v1/chat/media/media_123/../other?grant=opaque&variant=thumbnail", "thumbnail", false},
		{"wrong endpoint", "/v1/chat/mediax/media_123?grant=opaque&variant=thumbnail", "thumbnail", false},
		{"fragment", "/v1/chat/media/media_123?grant=opaque&variant=thumbnail#x", "thumbnail", false},
		{"unknown variant", "/v1/chat/media/media_123?grant=opaque&variant=original", "original", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validChatRenditionURL(tc.raw, tc.variant); got != tc.want {
				t.Fatalf("validChatRenditionURL(%q, %q) = %v, want %v", tc.raw, tc.variant, got, tc.want)
			}
		})
	}
}

func TestChatMediaVariantURLKeepsGrantAndSelectsVariant(t *testing.T) {
	base := "/v1/chat/media/media_123?grant=opaque&other=value"
	for _, variant := range []string{"thumbnail", "display"} {
		got, ok := chatMediaVariantURL(base, variant)
		if !ok || !validChatRenditionURL(got, variant) {
			t.Fatalf("chatMediaVariantURL(%q, %q) = (%q, %v)", base, variant, got, ok)
		}
		parsed, _ := url.Parse(got)
		if parsed.Query().Get("grant") != "opaque" || parsed.Query().Get("other") != "value" {
			t.Fatalf("variant URL changed grant or adjacent query values: %q", got)
		}
	}
	if _, ok := chatMediaVariantURL("https://foreign.test/v1/chat/media/x?grant=y", "thumbnail"); ok {
		t.Fatal("absolute URL must not be accepted")
	}
}

func TestChatMediaOriginalURLAndViewerMemoryBound(t *testing.T) {
	original, ok := chatMediaOriginalURL("/v1/chat/media/media_123?grant=opaque&variant=display")
	if !ok || original != "/v1/chat/media/media_123?grant=opaque" {
		t.Fatalf("original URL = (%q, %v)", original, ok)
	}
	if _, ok := chatMediaOriginalURL("/v1/chat/media/media_123?grant=a&grant=b"); ok {
		t.Fatal("duplicate grants must be rejected")
	}
	if chatImageOriginalFitsViewerBounds(0, 100, 10) || chatImageOriginalFitsViewerBounds(100, 100, 0) {
		t.Fatal("unknown image bounds must not be fetched into the viewer")
	}
	if !chatImageOriginalFitsViewerBounds(2400, 1600, 2<<20) {
		t.Fatal("ordinary image should fit the viewer budget")
	}
	if chatImageOriginalFitsViewerBounds(4032, 3024, 8<<20) {
		t.Fatal("large decoded image must use the download affordance")
	}
}
