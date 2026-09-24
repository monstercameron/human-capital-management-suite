// Package documentembed provides the embedding models behind document
// meaning search: an OpenAI-compatible HTTP client and a pure-Go static
// model (model2vec layout) loaded from a local directory. Nothing here
// downloads a model; both are configured by the operator.
package documentembed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Environment variables that configure the document embedder. A model
// directory wins over an HTTP endpoint when both are set.
const (
	EnvURL    = "HCMNEXT_EMBEDDING_URL"
	EnvModel  = "HCMNEXT_EMBEDDING_MODEL"
	EnvAPIKey = "HCMNEXT_EMBEDDING_API_KEY"
	EnvDir    = "HCMNEXT_EMBEDDING_DIR"
)

// Embedder turns texts into vectors. Model names the model and is stored
// with every vector, so vectors from different models never mix.
type Embedder interface {
	Model() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Local reports whether an embedder keeps text inside this deployment.
// Only local embedders may see unpublished drafts.
type Local interface {
	Local() bool
}

// IsLocal reports whether e keeps text in-process or on a loopback host.
func IsLocal(e Embedder) bool {
	l, ok := e.(Local)
	return ok && l.Local()
}

// ErrNotConfigured is returned by FromEnv when no embedder is configured.
var ErrNotConfigured = errors.New("document embedding: no model configured")

// FromEnv builds the configured embedder, or returns ErrNotConfigured.
// getenv is os.Getenv in production and a map in tests.
func FromEnv(getenv func(string) string) (Embedder, error) {
	if dir := strings.TrimSpace(getenv(EnvDir)); dir != "" {
		return LoadStatic(dir)
	}
	endpoint, model := strings.TrimSpace(getenv(EnvURL)), strings.TrimSpace(getenv(EnvModel))
	if endpoint == "" && model == "" {
		return nil, ErrNotConfigured
	}
	if endpoint == "" || model == "" {
		return nil, fmt.Errorf("document embedding: both %s and %s are required", EnvURL, EnvModel)
	}
	return NewHTTP(endpoint, model, strings.TrimSpace(getenv(EnvAPIKey)), nil)
}

// HTTP calls an OpenAI-compatible /v1/embeddings endpoint.
type HTTP struct {
	endpoint, model, apiKey string
	client                  *http.Client
	local                   bool
}

// httpBatch bounds how many texts one request carries.
const httpBatch = 64

// NewHTTP returns an embedder for baseURL. A URL that already ends in
// /embeddings is used as is; otherwise /v1/embeddings is appended. A nil
// client gets a 30-second timeout.
func NewHTTP(baseURL, model, apiKey string, client *http.Client) (*HTTP, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("document embedding: %s must be an http(s) URL", EnvURL)
	}
	endpoint := u.String()
	if !strings.HasSuffix(u.Path, "/embeddings") {
		endpoint += "/v1/embeddings"
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	return &HTTP{endpoint: endpoint, model: model, apiKey: apiKey, client: client, local: host == "localhost" || (ip != nil && ip.IsLoopback())}, nil
}

// Model returns the configured model name.
func (h *HTTP) Model() string { return h.model }

// Local reports whether the endpoint is on a loopback host.
func (h *HTTP) Local() bool { return h.local }

// Embed posts texts in bounded batches and returns vectors in input order.
func (h *HTTP) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += httpBatch {
		end := min(start+httpBatch, len(texts))
		vecs, err := h.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (h *HTTP) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"model": h.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("document embedding: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("document embedding: endpoint returned %s", resp.Status)
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("document embedding: malformed response: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("document embedding: %d vectors for %d texts", len(parsed.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(texts) || out[d.Index] != nil || len(d.Embedding) == 0 {
			return nil, errors.New("document embedding: response indexes are inconsistent")
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}
