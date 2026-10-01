package anthropic

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

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

const (
	defaultBaseURL   = "https://api.anthropic.com/v1"
	apiVersion       = "2023-06-01"
	maxResponseBytes = 4 << 20
	maxTools         = 64
	maxOutputTokens  = 8192
)

var (
	// ErrNotConfigured indicates missing or invalid pinned adapter settings.
	ErrNotConfigured = errors.New("anthropic adapter: not configured")
	// ErrInvalidResponse marks malformed or semantically unsupported provider data.
	ErrInvalidResponse = errors.New("anthropic adapter: invalid provider response")
)

// Config pins one approved profile, provider model identity, credential, and API endpoint.
type Config struct {
	APIKey       string
	ModelProfile string
	Identity     agentmodel.ModelIdentity
	BaseURL      string
	HTTPClient   *http.Client
}

// Adapter calls Anthropic's Messages API without creating hosted conversation state.
type Adapter struct {
	apiKey       string
	modelProfile string
	identity     agentmodel.ModelIdentity
	endpoint     string
	client       *http.Client
}

// New constructs an adapter for one approved model profile.
func New(cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.ModelProfile) == "" ||
		cfg.Identity.ProviderID != "anthropic" || strings.TrimSpace(cfg.Identity.ModelID) == "" || strings.TrimSpace(cfg.Identity.Version) == "" {
		return nil, ErrNotConfigured
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopback(parsed.Hostname()))) {
		return nil, fmt.Errorf("%w: invalid API base URL", ErrNotConfigured)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	} else {
		copy := *client
		client = &copy
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Adapter{
		apiKey: cfg.APIKey, modelProfile: cfg.ModelProfile, identity: cfg.Identity,
		endpoint: base + "/messages", client: client,
	}, nil
}

func isLoopback(host string) bool {
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// Identity returns the exact configured provider model and revision for egress pinning.
func (a *Adapter) Identity() agentmodel.ModelIdentity {
	if a == nil {
		return agentmodel.ModelIdentity{}
	}
	return a.identity
}

// Capabilities reports the features this adapter can preserve with the Messages API.
func (a *Adapter) Capabilities() agentmodel.AdapterCapabilities {
	return capabilities()
}

// Invoke buffers and validates a complete response before returning text or proposals.
func (a *Adapter) Invoke(ctx context.Context, req agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	if a == nil {
		return agentmodel.ModelResult{}, ErrNotConfigured
	}
	if err := agentmodel.ValidateModelRequest(req); err != nil {
		return agentmodel.ModelResult{}, err
	}
	if req.ModelProfile != a.modelProfile {
		return agentmodel.ModelResult{}, &agentmodel.ModelRefusal{Code: agentmodel.RefusalFeature, Detail: "request profile is not configured for this adapter"}
	}
	if refusal := agentmodel.CheckCapabilities(req, a.Capabilities()); refusal != nil {
		return agentmodel.ModelResult{}, refusal
	}
	if req.Limits.MaxOutputTokens > maxOutputTokens {
		return agentmodel.ModelResult{}, &agentmodel.ModelRefusal{Code: agentmodel.RefusalFeature, Detail: "requested output token limit exceeds adapter ceiling"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return cancelledResult(a.identity, err)
	}
	apiReq, err := makeRequest(a.identity.ModelID, req)
	if err != nil {
		return agentmodel.ModelResult{}, err
	}
	body, err := json.Marshal(apiReq)
	if err != nil {
		return agentmodel.ModelResult{}, fmt.Errorf("anthropic adapter: encode request: %w", err)
	}
	callCtx, cancel := context.WithDeadline(ctx, req.Deadline)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return agentmodel.ModelResult{}, fmt.Errorf("anthropic adapter: build request: %w", err)
	}
	request.Header.Set("x-api-key", a.apiKey)
	request.Header.Set("anthropic-version", apiVersion)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		if callCtx.Err() != nil {
			return cancelledResult(a.identity, callCtx.Err())
		}
		code := agentmodel.FailureUnavailable
		if errors.Is(err, context.DeadlineExceeded) {
			code = agentmodel.FailureTimeout
		}
		return failureResult(a.identity, code, true, "provider request failed"), providerError{code: code, retryable: true, message: "provider request failed"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpFailure(a.identity, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return failureResult(a.identity, agentmodel.FailureUnavailable, true, "provider response could not be read"), providerError{code: agentmodel.FailureUnavailable, retryable: true, message: "provider response could not be read"}
	}
	if len(data) > maxResponseBytes {
		return invalidResult(a.identity, "provider response exceeded the size limit")
	}
	decoded, err := decodeResponse(data)
	if err != nil {
		return invalidResult(a.identity, "provider response could not be validated")
	}
	result, err := mapResponse(a.identity, decoded, req)
	if err != nil {
		return result, err
	}
	if err := agentmodel.ValidateModelResult(req, result, a.Capabilities()); err != nil {
		failure := failureResult(a.identity, agentmodel.FailureInvalid, false, "provider result did not satisfy the model contract")
		failure.Usage = result.Usage
		return failure, providerError{code: agentmodel.FailureInvalid, message: failure.Failure.Message}
	}
	return result, nil
}

type providerError struct {
	code      agentmodel.FailureCode
	retryable bool
	message   string
}

func (e providerError) Error() string { return "anthropic adapter: " + e.message }

func cancelledResult(identity agentmodel.ModelIdentity, err error) (agentmodel.ModelResult, error) {
	return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: identity, Finish: agentmodel.FinishCancelled}, err
}

func failureResult(identity agentmodel.ModelIdentity, code agentmodel.FailureCode, retryable bool, message string) agentmodel.ModelResult {
	return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: identity, Failure: &agentmodel.ModelFailure{Code: code, Retryable: retryable, Message: message}}
}

func invalidResult(identity agentmodel.ModelIdentity, message string) (agentmodel.ModelResult, error) {
	return failureResult(identity, agentmodel.FailureInvalid, false, message), providerError{code: agentmodel.FailureInvalid, message: message}
}

func httpFailure(identity agentmodel.ModelIdentity, status int) (agentmodel.ModelResult, error) {
	code, retryable := agentmodel.FailureInvalid, false
	if status == http.StatusTooManyRequests || status == 529 || status >= 500 {
		code, retryable = agentmodel.FailureUnavailable, true
	}
	if status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout {
		code, retryable = agentmodel.FailureTimeout, true
	}
	message := fmt.Sprintf("provider returned HTTP %d", status)
	return failureResult(identity, code, retryable, message), providerError{code: code, retryable: retryable, message: message}
}
