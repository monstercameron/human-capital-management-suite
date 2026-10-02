package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

type FilterAPIClient struct {
	Client  *http.Client
	BaseURL string
	Headers func() http.Header
}
type FilterAPIError struct {
	Code     string           `json:"code"`
	RuleName string           `json:"rule_name"`
	Span     *chatfilter.Span `json:"span"`
}

func (e *FilterAPIError) Error() string { return e.Code }
func (c FilterAPIClient) call(ctx context.Context, method, path string, body, result any) error {
	var buffer bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buffer).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+"/api/chat/filters/v1"+path, &buffer)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Headers != nil {
		for key, values := range c.Headers() {
			req.Header[key] = append([]string(nil), values...)
		}
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		var typed FilterAPIError
		if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&typed); err != nil {
			return &FilterAPIError{Code: "filters_unavailable"}
		}
		return &typed
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(result)
	}
	return nil
}
func (c FilterAPIClient) List(ctx context.Context) ([]chatfilter.Definition, error) {
	return c.ListForChannel(ctx, "")
}
func (c FilterAPIClient) ListForChannel(ctx context.Context, channel string) ([]chatfilter.Definition, error) {
	var out []chatfilter.Definition
	err := c.call(ctx, "GET", "?channel="+url.QueryEscape(channel), nil, &out)
	return out, err
}
func (c FilterAPIClient) CreateVersion(ctx context.Context, d chatfilter.Definition) error {
	return c.call(ctx, "POST", "/versions", map[string]any{"Definition": d}, nil)
}
func (c FilterAPIClient) Enable(ctx context.Context, rule, channel string, enabled, dry bool) error {
	return c.EnableAction(ctx, rule, channel, enabled, dry, "")
}
func (c FilterAPIClient) EnableAction(ctx context.Context, rule, channel string, enabled, dry bool, action string) error {
	path := "/disable"
	if enabled {
		path = "/enable"
	}
	return c.call(ctx, "POST", path, map[string]any{"Enablement": chatfilter.Enablement{RuleID: rule, Channel: channel, Action: action}, "DryRun": dry}, nil)
}
func (c FilterAPIClient) Try(ctx context.Context, d chatfilter.Definition, channel, sample string) (chatfilter.Result, error) {
	var out chatfilter.Result
	err := c.call(ctx, "POST", "/try", map[string]any{"Definition": d, "Channel": channel, "Sample": sample}, &out)
	return out, err
}
func (c FilterAPIClient) Hits(ctx context.Context) ([]chatfilter.Record, error) {
	return c.SearchHits(ctx, "", "", 0)
}
func (c FilterAPIClient) SearchHits(ctx context.Context, query, channel string, before int64) ([]chatfilter.Record, error) {
	var out []chatfilter.Record
	path := "/hits?q=" + url.QueryEscape(query) + "&channel=" + url.QueryEscape(channel) + "&before=" + strconv.FormatInt(before, 10)
	err := c.call(ctx, "GET", path, nil, &out)
	return out, err
}

// ChatFilterHint uses the server's compiler for enabled product definitions.
// Custom/private definitions remain on the server, whose decision is final.
func ChatFilterHint(enabled []chatfilter.Definition, input chatfilter.Input) (chatfilter.Result, error) {
	var product []chatfilter.Definition
	for _, definition := range enabled {
		if definition.Product {
			product = append(product, definition)
		}
	}
	evaluator, err := chatfilter.NewRegistry().Compile(product)
	if err != nil {
		return chatfilter.Result{}, err
	}
	return evaluator.Evaluate(input)
}

func (c FilterAPIClient) Enablements(ctx context.Context) ([]chatfilter.Enablement, error) {
	return c.EnablementsForChannel(ctx, "")
}
func (c FilterAPIClient) EnablementsForChannel(ctx context.Context, channel string) ([]chatfilter.Enablement, error) {
	var out []chatfilter.Enablement
	err := c.call(ctx, "GET", "/enablements?channel="+url.QueryEscape(channel), nil, &out)
	return out, err
}

// FilterBlockedDraft preserves exact bytes. Coordinates are accepted only when
// they identify whole UTF-8 codepoints inside that draft.
type FilterBlockedDraft struct {
	Draft, RuleName string
	Span            chatfilter.Span
}

func ParseFilterBlockedDraft(draft string, err error) (FilterBlockedDraft, bool) {
	if err == nil {
		return FilterBlockedDraft{}, false
	}
	var typed *FilterAPIError
	if !errors.As(err, &typed) {
		raw := err.Error()
		start := strings.IndexByte(raw, '{')
		if start < 0 {
			return FilterBlockedDraft{}, false
		}
		typed = &FilterAPIError{}
		if json.Unmarshal([]byte(raw[start:]), typed) != nil {
			return FilterBlockedDraft{}, false
		}
	}
	if typed.Code != "content_blocked" || typed.Span == nil {
		return FilterBlockedDraft{}, false
	}
	span := *typed.Span
	if span.Start < 0 || span.End > len(draft) || span.End <= span.Start || !utf8.ValidString(draft[:span.Start]) || !utf8.ValidString(draft[:span.End]) {
		return FilterBlockedDraft{}, false
	}
	return FilterBlockedDraft{Draft: draft, RuleName: typed.RuleName, Span: span}, true
}
