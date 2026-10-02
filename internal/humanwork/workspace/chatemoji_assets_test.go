package workspace

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

var chatEmojiDataFiles = []string{"emoji-order.json", "emoji-en.json", "emoji-de.json", "emoji-ar.json"}

// CHATEMOJI-001: the emoji data is a static, immutable, compressed asset on the
// product's own origin. The page's content security policy already lets the
// client fetch from the asset prefix of its own origin, so no directive changes.
func TestTodo_CHATEMOJI_001_Serving(t *testing.T) {
	manifest, err := LoadEmbeddedAssetIntegrityManifest()
	if err != nil {
		t.Fatalf("the generated asset manifest does not describe the embedded files: %v", err)
	}
	digests := map[string]string{}
	encodings := map[string][]string{}
	for _, a := range manifest.Assets {
		digests[a.Path] = a.SHA256
		for _, r := range a.Representations {
			encodings[a.Path] = append(encodings[a.Path], r.Encoding)
		}
	}
	handler, token := newShellHandler(t, false)
	get := func(name, query string, header map[string]string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathAssetPrefix+name+query, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		for k, v := range header {
			request.Header.Set(k, v)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	var set *emojiset.Set
	for _, name := range chatEmojiDataFiles {
		path := PathAssetPrefix + name
		sha := digests[path]
		if sha == "" {
			t.Fatalf("%s is not in the asset manifest", name)
		}
		if got := encodings[path]; len(got) != 2 || got[1] != "gzip" {
			t.Fatalf("%s is catalogued as %v, want identity and gzip", name, got)
		}
		// Addressed by its digest: kept for a year, never revalidated.
		response := get(name, "?v="+sha, map[string]string{"Accept-Encoding": "gzip"})
		if response.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", name, response.Code, response.Body.String())
		}
		header := response.Header()
		if header.Get("Content-Type") != "application/json; charset=utf-8" || header.Get("Content-Encoding") != "gzip" || header.Get("Vary") != "Accept-Encoding" {
			t.Errorf("%s headers = %v", name, header)
		}
		if got := header.Get("Cache-Control"); got != "private, max-age=31536000, immutable" {
			t.Errorf("%s Cache-Control = %q", name, got)
		}
		reader, err := gzip.NewReader(response.Body)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if raw, ok := asset(name); !ok || !bytes.Equal(raw, body) {
			t.Errorf("%s: the compressed bytes are not the embedded file", name)
		}
		// Asked for without the digest, it revalidates instead.
		if got := get(name, "", nil).Header().Get("Cache-Control"); got != "private, max-age=0, must-revalidate" {
			t.Errorf("%s without a digest: Cache-Control = %q", name, got)
		}
		revalidate := get(name, "?v="+sha, map[string]string{"Accept-Encoding": "gzip", "If-None-Match": header.Get("ETag")})
		if revalidate.Code != http.StatusNotModified || revalidate.Body.Len() != 0 {
			t.Errorf("%s conditional request = %d with %d bytes", name, revalidate.Code, revalidate.Body.Len())
		}
		if name == "emoji-order.json" {
			if set, err = emojiset.ParseOrder(body); err != nil {
				t.Fatal(err)
			}
		} else if _, err := emojiset.ParseLang(body, set); err != nil {
			t.Errorf("%s does not fit the order file: %v", name, err)
		}
	}

	notice := get("emoji-LICENSE.txt", "", nil)
	if notice.Code != http.StatusOK || !strings.HasPrefix(notice.Header().Get("Content-Type"), "text/plain") || !strings.Contains(notice.Body.String(), "UNICODE LICENSE V3") {
		t.Fatalf("licence notice = %d %q", notice.Code, notice.Header().Get("Content-Type"))
	}
	// Nothing else under the emoji prefix is routable.
	for _, name := range []string{"emoji-fr.json", "emoji-en.json.gz", "emoji-notes.txt", "emoji-.json"} {
		if got := get(name, "", nil).Code; got != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", name, got)
		}
	}
}

// The chat client fetches the data files from its own origin with fetch, which
// connect-src governs. The product's policy admits the asset prefix of the
// document's own host and nothing wider.
func TestTodo_CHATEMOJI_001_ContentSecurityPolicy(t *testing.T) {
	policy := productContentSecurityPolicyForHash("cell.test", sha256Source("body{}"))
	var connect []string
	for _, directive := range strings.Split(policy, ";") {
		if fields := strings.Fields(directive); len(fields) > 0 && fields[0] == "connect-src" {
			connect = fields[1:]
		}
	}
	if len(connect) == 0 {
		t.Fatalf("no connect-src in %q", policy)
	}
	allows := func(url string) bool {
		for _, source := range connect {
			bare := strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "https://")
			if strings.HasSuffix(source, "/") && strings.HasPrefix(bare, source) || bare == source {
				return true
			}
		}
		return false
	}
	for _, name := range append(chatEmojiDataFiles, "manifest.json") {
		for _, scheme := range []string{"http", "https"} {
			if url := scheme + "://cell.test" + PathAssetPrefix + name; !allows(url) {
				t.Errorf("connect-src %v does not admit %s", connect, url)
			}
		}
	}
	for _, url := range []string{"https://elsewhere.test" + PathAssetPrefix + "emoji-en.json", "https://cell.test/api/emoji-en.json"} {
		if allows(url) {
			t.Errorf("connect-src admits %s", url)
		}
	}
}
