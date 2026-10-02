package chatui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// The emoji data is not part of the client program. It is four static files on
// the product's own origin (one order file and one file of names and keywords per
// language), served compressed and addressed by their digest so the browser keeps
// them. The picker loads the reader's language and English the first time it
// opens and keeps them for the session.

type emojiDataStatus int

const (
	emojiDataIdle emojiDataStatus = iota
	emojiDataLoading
	emojiDataReady
	emojiDataFailed
)

// chatEmojiDataStore is the session's emoji data. The client runs on one thread,
// so a plain value is enough; an assignment is what makes loaded data visible.
type chatEmojiDataStore struct {
	status emojiDataStatus
	lang   string
	index  *emojiset.Index
}

var chatEmojiData chatEmojiDataStore

// emojiLoader reads the data files with the person's own credential, from the
// origin that served the page.
type emojiLoader struct {
	Origin, Bearer string
	HTTP           *http.Client
}

const (
	emojiAssetPath     = "/workspace/assets/"
	emojiMaxFileBytes  = 4 << 20
	emojiLoadTimeout   = 30 * time.Second
	emojiManifestLimit = 1 << 20
)

// Load fetches and reads the order file and the names of lang and English.
func (l emojiLoader) Load(ctx context.Context, lang string) (*emojiset.Index, error) {
	origin, err := url.Parse(l.Origin)
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil || l.HTTP == nil {
		return nil, errors.New("emoji data: no usable origin")
	}
	digests := l.digests(ctx, origin)
	fetch := func(name string) ([]byte, error) {
		target := *origin
		target.Path, target.RawQuery, target.Fragment = emojiAssetPath+name, "", ""
		if sha := digests[emojiAssetPath+name]; sha != "" {
			target.RawQuery = url.Values{"v": {sha}}.Encode()
		}
		return l.get(ctx, target.String(), emojiMaxFileBytes)
	}
	orderBytes, err := fetch("emoji-order.json")
	if err != nil {
		return nil, err
	}
	set, err := emojiset.ParseOrder(orderBytes)
	if err != nil {
		return nil, err
	}
	read := func(code string) (*emojiset.Lang, error) {
		data, err := fetch("emoji-" + code + ".json")
		if err != nil {
			return nil, err
		}
		return emojiset.ParseLang(data, set)
	}
	english, err := read("en")
	if err != nil {
		return nil, err
	}
	reader := english
	if lang != "en" {
		// A missing reader's language is not fatal: English names still work.
		if loaded, err := read(lang); err == nil {
			reader = loaded
		}
	}
	return emojiset.NewIndex(set, reader, english), nil
}

// digests reads the asset manifest for each file's SHA-256, so the files can be
// requested by digest and kept immutably. Without it the files are still loaded,
// revalidated instead of kept.
func (l emojiLoader) digests(ctx context.Context, origin *url.URL) map[string]string {
	target := *origin
	target.Path, target.RawQuery, target.Fragment = emojiAssetPath+"manifest.json", "", ""
	body, err := l.get(ctx, target.String(), emojiManifestLimit)
	if err != nil {
		return nil
	}
	var manifest struct {
		Assets []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"assets"`
	}
	if json.Unmarshal(body, &manifest) != nil {
		return nil
	}
	out := map[string]string{}
	for _, a := range manifest.Assets {
		if strings.HasPrefix(a.Path, emojiAssetPath+"emoji-") && len(a.SHA256) == 64 {
			out[a.Path] = a.SHA256
		}
	}
	return out
}

func (l emojiLoader) get(ctx context.Context, target string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	if l.Bearer != "" {
		request.Header.Set("Authorization", "Bearer "+l.Bearer)
	}
	request.Header.Set("Accept", "application/json")
	client := *l.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("emoji data: %s answered %d", request.URL.Path, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("emoji data: file is larger than expected")
	}
	return body, nil
}
