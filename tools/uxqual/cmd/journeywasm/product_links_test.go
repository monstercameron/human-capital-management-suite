package main

import "testing"

func TestProductLinkFallbackTarget(t *testing.T) {
	const origin = "http://127.0.0.1:9998"
	cases := []struct {
		name string
		in   productLinkFallbackAnchor
		want string
		ok   bool
	}{
		{"chat to doc", productLinkFallbackAnchor{Href: origin + "/workspace/app/docs?document=doc-1", Origin: origin, Current: "/workspace/app/chat"}, "/workspace/app/docs?document=doc-1", true},
		{"doc to chat channel", productLinkFallbackAnchor{Href: origin + "/workspace/app/chat#channel=c1", Origin: origin, Current: "/workspace/app/docs?document=doc-1"}, "/workspace/app/chat#channel=c1", true},
		{"chat to project task detail", productLinkFallbackAnchor{Href: origin + "/workspace/app/project?project=p-1&task=t-1", Origin: origin, Current: "/workspace/app/chat#channel=c1"}, "/workspace/app/project?project=p-1&task=t-1", true},
		{"project board to docs", productLinkFallbackAnchor{Href: origin + "/workspace/app/docs?document=doc-1", Origin: origin, Current: "/workspace/app/project?project=p-1&view=list"}, "/workspace/app/docs?document=doc-1", true},
		{"same-page fragment stays with the browser", productLinkFallbackAnchor{Href: origin + "/workspace/app/chat#person=p1", Origin: origin, Current: "/workspace/app/chat"}, "", false},
		{"new tab", productLinkFallbackAnchor{Href: origin + "/workspace/app/docs", Target: "_blank", Origin: origin}, "", false},
		{"download", productLinkFallbackAnchor{Href: origin + "/workspace/app/docs", Download: true, Origin: origin}, "", false},
		{"other origin", productLinkFallbackAnchor{Href: "https://example.com/workspace/app/docs", Origin: origin}, "", false},
		{"outside the app", productLinkFallbackAnchor{Href: origin + "/v1/documents/media/x", Origin: origin}, "", false},
		{"logout is not an app page", productLinkFallbackAnchor{Href: origin + "/workspace/logout", Origin: origin}, "", false},
	}
	for _, tc := range cases {
		got, ok := productLinkFallbackTarget(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}
