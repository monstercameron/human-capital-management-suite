package chatui

import "testing"

func TestChatShareLocatorsAcceptOnlySameOriginAndBoundedTokens(t *testing.T) {
	body := "[review](https://hcm.example/workspace/app/chat#share=abc_12). https://evil.example/workspace/app/chat#share=secret /chat/share/def-3 #msg=oldpost https://hcm.example/workspace/app/chat#share=abc_12"
	got := ShareLocators(body, "https://hcm.example")
	if len(got) != 3 || got[0].Token != "abc_12" || got[1].Token != "def-3" || got[2].LegacyPost != "oldpost" {
		t.Fatalf("locators = %#v", got)
	}
	if got := ShareLocators("https://evil.example/chat/share/foreign", "https://hcm.example"); len(got) != 0 {
		t.Fatalf("foreign link accepted: %#v", got)
	}
}
