package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// ISSUE-3929: the browser side of Saved searches. A saved search is a name and
// the query text, kept on the person's account; opening one is the ordinary
// search. Nothing here sends the query anywhere but the person's own saved
// searches route, and nothing logs it.

const issue3929Path = "/api/chat/search/saved"

type issue3929Wire struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Query string `json:"query"`
}

// issue3929Request calls the saved-search route. id, when set, names the one
// shortcut a change or delete is about.
func issue3929Request(ctx context.Context, client *http.Client, cfg journeyclient.Config, method, id string, input, output any) error {
	u, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" {
		return chatsearch.ErrInvalid
	}
	u.Path, u.RawPath, u.Fragment = issue3929Path, "", ""
	u.RawQuery = ""
	if id != "" {
		u.RawQuery = url.Values{"id": {id}}.Encode()
	}
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
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&failure)
		if failure.Code == "" {
			failure.Code = "unavailable"
		}
		return chatsearchClientError{failure.Code}
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output)
}

func issue3929Items(wire []issue3929Wire) []chatui.SavedSearchItem {
	items := make([]chatui.SavedSearchItem, 0, len(wire))
	for _, w := range wire {
		if w.ID != "" && strings.TrimSpace(w.Query) != "" {
			items = append(items, chatui.SavedSearchItem{ID: w.ID, Name: w.Name, Query: w.Query})
		}
	}
	return items
}

// issue3929StatusFor turns a failed call into the status the popover speaks.
func issue3929StatusFor(err error) string {
	var problem chatsearchClientError
	if errors.As(err, &problem) {
		switch problem.Code {
		case "name_taken":
			return "nameTaken"
		case "limit":
			return "limit"
		case "invalid":
			return "invalid"
		}
	}
	return "failed"
}

// issue3929Apply puts the saved searches and the outcome of the last change on
// the model. A change that failed leaves the list as it was.
func issue3929Apply(m *chatui.Model, items []chatui.SavedSearchItem, status, name string) {
	if items != nil {
		m.SearchSaved = items
	}
	m.SavedSearchStatus, m.SavedSearchName = status, name
	if status == "updated" || status == "deleted" || status == "saved" {
		m.SavedSearchEditing = ""
	}
}
