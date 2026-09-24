package chatui

import (
	"errors"
	"testing"
)

func TestTodo_CHAT_039_DescriptorRequiresExactApprovedHTTPSOrigin(t *testing.T) {
	valid := WebEmbedDescriptor{URL: "https://widgets.example/app", Origin: "https://widgets.example", Grant: "grant-1", Nonce: "nonce-1234567890", Title: "Planning widget"}
	if err := ValidateWebEmbedDescriptor(valid); err != nil {
		t.Fatalf("valid descriptor rejected: %v", err)
	}
	for name, mutate := range map[string]func(*WebEmbedDescriptor){
		"other-origin":  func(v *WebEmbedDescriptor) { v.Origin = "https://evil.example" },
		"port-change":   func(v *WebEmbedDescriptor) { v.URL = "https://widgets.example:8443/app" },
		"credentials":   func(v *WebEmbedDescriptor) { v.URL = "https://user:pass@widgets.example/app" },
		"script-scheme": func(v *WebEmbedDescriptor) { v.URL = "javascript:alert(1)" },
		"fragment":      func(v *WebEmbedDescriptor) { v.URL = "https://widgets.example/app#bridge" },
		"missing-grant": func(v *WebEmbedDescriptor) { v.Grant = "" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := ValidateWebEmbedDescriptor(candidate); !errors.Is(err, ErrWebEmbedDenied) {
				t.Fatalf("descriptor error = %v, want ErrWebEmbedDenied", err)
			}
		})
	}
}

func TestTodo_CHAT_039_BridgeAcceptsOnlyBoundedTypedResize(t *testing.T) {
	embed := WebEmbedDescriptor{URL: "https://widgets.example/app", Origin: "https://widgets.example", Grant: "grant-1", Nonce: "nonce-1234567890", Title: "Planning widget"}
	valid := WebEmbedBridgeMessage{Version: 1, Grant: embed.Grant, Nonce: embed.Nonce, Action: "resize", Height: 280}
	if got, err := ValidateWebEmbedBridge(embed, valid); err != nil || got != 280 {
		t.Fatalf("valid resize = %d, %v", got, err)
	}
	for name, mutate := range map[string]func(*WebEmbedBridgeMessage){
		"wrong-version": func(v *WebEmbedBridgeMessage) { v.Version = 2 },
		"wrong-grant":   func(v *WebEmbedBridgeMessage) { v.Grant = "other" },
		"wrong-nonce":   func(v *WebEmbedBridgeMessage) { v.Nonce = "other" },
		"forged-action": func(v *WebEmbedBridgeMessage) { v.Action = "read-chat" },
		"too-short":     func(v *WebEmbedBridgeMessage) { v.Height = 79 },
		"too-tall":      func(v *WebEmbedBridgeMessage) { v.Height = 601 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if _, err := ValidateWebEmbedBridge(embed, candidate); !errors.Is(err, ErrWebEmbedDenied) {
				t.Fatalf("bridge error = %v, want ErrWebEmbedDenied", err)
			}
		})
	}
}
