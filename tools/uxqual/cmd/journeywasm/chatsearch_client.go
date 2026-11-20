package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatsearchClientError struct{ Code string }

func (e chatsearchClientError) Error() string { return "chat search: " + e.Code }
func chatsearchRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, path, method string, input, output any) error {
	u, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" {
		return chatsearch.ErrInvalid
	}
	if path != "/api/chat/search" && path != "/api/chat/search/recent" {
		return chatsearch.ErrInvalid
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = path, "", "", ""
	var body io.Reader
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, u.String(), body)
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if input != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, e := client.Do(r)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	if response.StatusCode != http.StatusOK {
		var problem struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&problem)
		if problem.Code == "" {
			problem.Code = "unavailable"
		}
		return chatsearchClientError{problem.Code}
	}
	return json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(output)
}
func chatsearchDecodeTarget(token string) (chatsearch.Target, error) {
	var target chatsearch.Target
	if len(token) > 4096 {
		return target, chatsearch.ErrInvalid
	}
	b, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil || json.Unmarshal(b, &target) != nil {
		return target, chatsearch.ErrInvalid
	}
	if target.ConversationID == "" && target.DocumentID == "" && target.ItemID == "" {
		return target, chatsearch.ErrInvalid
	}
	return target, nil
}

// chatsearchVoiceSentence is the sentence an opened result seeks to: a voice
// result that names one in a message. A correction is not timed and seeks
// nowhere.
func chatsearchVoiceSentence(kind chatsearch.Kind, target chatsearch.Target) (int, bool) {
	if kind != chatsearch.Voice || target.Sentence <= 0 || target.ConversationID == "" || target.MessageID == "" {
		return 0, false
	}
	return target.Sentence, true
}

func chatsearchControlQuery(query string, values map[string]string, checks map[string]bool) (string, error) {
	_, e := chatsearch.Parse(query)
	if e != nil {
		return "", e
	}
	tokens, e := chatsearch.Tokens(query)
	if e != nil {
		return "", e
	}
	// ISSUE-3690: the controls mirror the query, so a control the form submits is
	// the answer for its filter: filled replaces it, empty (blank text, Any
	// choice, unticked box) removes it. A control the form did not submit (a
	// key absent from the maps) leaves its filter alone.
	for _, pair := range [][2]string{{"channel", "in"}, {"person", "from"}, {"kind", "kind"}, {"written", "lang"}, {"before", "before"}, {"after", "after"}, {"on", "on"}} {
		raw, submitted := values[pair[0]]
		if !submitted {
			continue
		}
		value := strings.TrimSpace(raw)
		if strings.Contains(value, `"`) {
			return "", chatsearch.ErrInvalid
		}
		kept := tokens[:0]
		for _, token := range tokens {
			// Only a token with a colon is a filter; "in", "on" and "from"
			// typed as words are part of the search and stay.
			key, old, filter := strings.Cut(token, ":")
			names := strings.EqualFold(key, pair[1]) || (pair[1] == "lang" && strings.EqualFold(key, "language"))
			if filter && names && !(strings.EqualFold(key, "from") && old == "agent") {
				continue
			}
			kept = append(kept, token)
		}
		tokens = kept
		if value != "" {
			tokens = append(tokens, pair[1]+`:"`+value+`"`)
		}
	}
	for _, pair := range [][2]string{{"file", "has:file"}, {"link", "has:link"}, {"reactions", "has:reactions"}, {"threads", "is:thread"}, {"mentions", "mentions:me"}, {"agent", "from:agent"}, {"mine", "is:mine"}, {"voice", "has:voice"}} {
		checked, submitted := checks[pair[0]]
		if !submitted {
			continue
		}
		kept := tokens[:0]
		for _, token := range tokens {
			if strings.EqualFold(token, pair[1]) || (pair[1] == "is:thread" && strings.EqualFold(token, "is:threads")) {
				continue
			}
			kept = append(kept, token)
		}
		tokens = kept
		if checked {
			tokens = append(tokens, pair[1])
		}
	}
	for i, token := range tokens {
		if strings.Contains(token, " ") && !strings.Contains(token, `"`) {
			key, value, ok := strings.Cut(token, ":")
			if ok {
				tokens[i] = key + `:"` + value + `"`
			} else {
				tokens[i] = `"` + token + `"`
			}
		}
	}
	result := strings.Join(tokens, " ")
	if _, e = chatsearch.Parse(result); e != nil {
		return "", e
	}
	return result, nil
}

// chatsearchRemoveChip drops one filter chip from the query. The chips are read
// from the query with its unquoted names resolved (ISSUE-3691), so a chip that
// is not a word of the typed query is removed from the resolved query instead,
// which also takes the words of a name typed as from:@Julian Miller with it.
func chatsearchRemoveChip(model chatui.Model, typed, chip string) (string, error) {
	if tokens, err := chatsearch.Tokens(typed); err == nil {
		for _, token := range tokens {
			if token == chip {
				return chatsearch.RemoveChip(typed, chip)
			}
		}
	}
	resolved, err := chatui.ResolveChatSearchQuery(model, typed)
	if err != nil {
		return "", err
	}
	return chatsearch.RemoveChip(resolved, chip)
}

// chatsearchReversedRange reports that the dates the controls would apply leave
// no day between them: the After date is not earlier than the Before date
// (ISSUE-3859). A date left empty in the form keeps the one the query already
// holds when the form did not submit that field; a field submitted empty removes
// its date (ISSUE-3690). It returns the two dates the fields hold, so the page
// can keep what was typed.
func chatsearchReversedRange(query string, values map[string]string) (before, after string, reversed bool) {
	parsed, _ := chatsearch.Parse(query)
	before, after = strings.TrimSpace(values["before"]), strings.TrimSpace(values["after"])
	if _, submitted := values["before"]; !submitted {
		before = parsed.Filters.Before
	}
	if _, submitted := values["after"]; !submitted {
		after = parsed.Filters.After
	}
	return before, after, before != "" && after != "" && before <= after
}

func chatsearchErrorCode(err error) string {
	var problem chatsearchClientError
	if errors.As(err, &problem) {
		if problem.Code == "invalid" {
			return "invalid"
		}
		if problem.Code == "meaning_unavailable" {
			return "meaning"
		}
	}
	return "error"
}

func chatsearchThreadRequests(tenant string, target chatsearch.Target) (*chatv1.ListPostsRequest, *chatv1.ListPostsRequest, error) {
	if tenant == "" || target.ConversationID == "" || target.ThreadID == "" || target.MessageID == "" || target.Sequence == 0 || target.Sequence > uint64(1<<63-1) {
		return nil, nil, chatsearch.ErrInvalid
	}
	older, newer := chatSearchAnchorRequests(tenant, target.ConversationID, target.Sequence)
	return older, newer, nil
}

func chatsearchThreadReady(model chatui.Model, target chatsearch.Target, opening bool) bool {
	if opening || model.SelectedID != target.ConversationID {
		return false
	}
	if target.ThreadSequence > 0 {
		return model.FocusMessageID == target.ThreadID
	}
	return model.State == chatui.StateReady || model.State == chatui.StateEmpty
}
