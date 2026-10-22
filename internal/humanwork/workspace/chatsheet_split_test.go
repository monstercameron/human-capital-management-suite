package workspace

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatSheetLink is the head element a Chat page carries for Chat's sheet.
func chatSheetLink() string {
	sheet := chatStylesheet()
	return `<link rel="stylesheet" href="` + PathAssetPrefix + sheet.name + `" integrity="` + sheet.integrity + `">`
}

// chatSheetNote is the head element every other page carries for it.
func chatSheetNote() string {
	sheet := chatStylesheet()
	return `<meta name="hcm-chat-stylesheet" content="` + PathAssetPrefix + sheet.name + `" data-integrity="` + sheet.integrity + `">`
}

// A Chat page links Chat's sheet; any other page names it without loading it,
// and both link the control sheet, which no longer holds Chat's rules.
func TestChatStylesheetIsLinkedOnlyByChatPages(t *testing.T) {
	h, token := newShellHandler(t, false)
	for _, test := range []struct {
		page productui.PageID
		chat bool
	}{
		{productui.PageChat, true},
		{productui.PageChatSettings, true},
		{productui.PageHome, false},
		{productui.PageAgents, false},
	} {
		path := productui.Path(test.page)
		document := chatperf2Get(t, h, token, path, nil)
		if document.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, document.Code)
		}
		body := document.Body.String()
		if strings.Contains(body, "<style") {
			t.Errorf("%s: the document carries an inline stylesheet", path)
		}
		if !strings.Contains(body, defaultServedStylesheet().linkElement()) {
			t.Errorf("%s: the document does not link the control sheet", path)
		}
		wantLinks := 1
		if test.chat {
			wantLinks = 2
		}
		if got := strings.Count(body, "<link rel=\"stylesheet\""); got != wantLinks {
			t.Errorf("%s: %d stylesheet links, want %d", path, got, wantLinks)
		}
		if strings.Contains(body, chatSheetLink()) != test.chat {
			t.Errorf("%s: Chat's sheet linked = %v, want %v", path, !test.chat, test.chat)
		}
		if strings.Contains(body, chatSheetNote()) == test.chat {
			t.Errorf("%s: Chat's sheet named by a meta element = %v, want %v", path, test.chat, !test.chat)
		}
		// The element the shell writes is the one productui describes; the
		// client reads its name from there.
		if want := productui.ChatStylesheetElement(test.page); !strings.Contains(body, want) {
			t.Errorf("%s: the document lacks %s", path, want)
		}
		// Whatever the page, the policy admits both addresses and no inline style.
		policy := document.Header().Get("Content-Security-Policy")
		for _, source := range []string{PathAssetPrefix + defaultServedStylesheet().name, PathAssetPrefix + chatStylesheet().name} {
			if !strings.Contains(chatperf2Directive(policy, "style-src-elem"), "example.com"+source) {
				t.Errorf("%s: style-src-elem does not name %s: %s", path, source, policy)
			}
		}
		if chatperf2Directive(policy, "style-src-attr") != "'none'" || strings.Contains(policy, "'unsafe-inline'") {
			t.Errorf("%s: the policy admits inline style: %s", path, policy)
		}
	}
}

// Chat's sheet is served like the control sheet: at the address that carries
// its digest, with those bytes, compressed, kept for good, and not to a
// caller who is not signed in.
func TestChatStylesheetIsServedAtItsDigest(t *testing.T) {
	h, token := newShellHandler(t, false)
	sheet := chatStylesheet()
	href := PathAssetPrefix + sheet.name
	asset := chatperf2Get(t, h, token, href, nil)
	if asset.Code != http.StatusOK || asset.Body.String() != productui.ChatStylesheet() {
		t.Fatalf("GET %s = %d with %d bytes, want Chat's %d bytes", href, asset.Code, asset.Body.Len(), len(productui.ChatStylesheet()))
	}
	sum := sha256.Sum256(asset.Body.Bytes())
	if want := chatStylesheetAssetPrefix + hex.EncodeToString(sum[:]) + productStylesheetAssetSuffix; want != sheet.name {
		t.Fatalf("the sheet's bytes hash to %s, its address says %s", want, sheet.name)
	}
	if got := productui.ChatStylesheetAsset(); got.Name != sheet.name || got.Integrity != sheet.integrity || got.Href != href {
		t.Fatalf("productui names the Chat sheet %+v, the workspace serves %s", got, href)
	}
	if got := asset.Header().Get("Cache-Control"); got != "private, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := asset.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	compressed := chatperf2Get(t, h, token, href, map[string]string{"Accept-Encoding": "gzip"})
	if compressed.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("Chat's sheet was not compressed")
	}
	reader, err := gzip.NewReader(compressed.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil || string(plain) != productui.ChatStylesheet() {
		t.Fatalf("the compressed Chat sheet does not decompress to Chat's rules (%v)", err)
	}
	if anonymous := chatperf2Get(t, h, "", href, nil); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated request for Chat's sheet = %d, want 401", anonymous.Code)
	}
	if unknown := chatperf2Get(t, h, token, PathAssetPrefix+chatStylesheetAssetPrefix+strings.Repeat("0", 64)+productStylesheetAssetSuffix, nil); unknown.Code != http.StatusNotFound {
		t.Fatalf("a Chat sheet address that is not this build's = %d, want 404", unknown.Code)
	}
}

// The control sheet the workspace serves has Chat's rules out of it and stays
// under the size bound; Chat's sheet has its own bound. A tenant with its own
// colours changes the control sheet only.
func TestControlStylesheetDoesNotCarryChat(t *testing.T) {
	control := productStylesheet()
	if strings.Contains(control, "@scope (.chat-workspace)") || strings.Contains(control, productui.ChatStylesheet()) {
		t.Fatal("the control sheet carries Chat's rules")
	}
	if len(control) > 1_000_000 {
		t.Fatalf("the control sheet the workspace serves (the journey's rules and the product's) is %d bytes, over the 1,000,000-byte bound", len(control))
	}
	if len(productui.ChatStylesheet()) > 1_000_000 {
		t.Fatalf("Chat's sheet is %d bytes, over the 1,000,000-byte bound", len(productui.ChatStylesheet()))
	}
	h, token := newShellHandler(t, false)
	h.preferences = chatperf2ThemePreferences{theme: chatperf2CustomTheme}
	document := chatperf2Get(t, h, token, productui.Path(productui.PageChat), nil)
	if !strings.Contains(document.Body.String(), chatSheetLink()) {
		t.Fatal("a custom-theme Chat page does not link the one Chat sheet")
	}
}
