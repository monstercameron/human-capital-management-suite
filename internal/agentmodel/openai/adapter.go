// Package openai implements the agentmodel contract over OpenAI's Responses API.
// It buffers complete responses and returns function calls only as untrusted
// proposals for HCM Next to validate and execute.
package openai

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
	defaultBaseURL   = "https://api.openai.com/v1"
	maxResponseBytes = 4 << 20
	maxFunctionTools = 128
)

var (
	// ErrNotConfigured marks a missing credential, profile, or valid endpoint.
	ErrNotConfigured = errors.New("openai adapter: not configured")
	// ErrInvalidResponse marks provider output that violates the wire contract.
	ErrInvalidResponse = errors.New("openai adapter: invalid provider response")
)

// Config pins one approved profile and model identity to a credential and API endpoint.
type Config struct {
	APIKey       string
	ModelProfile string
	Identity     agentmodel.ModelIdentity
	BaseURL      string
	HTTPClient   *http.Client
	// Production composition enables exact provider input counting after egress
	// approval and before paid inference. Disabled only for standalone adapters
	// whose caller owns input admission (for example, isolated contract tests).
	PreflightInputTokens bool
}

// Adapter calls OpenAI's Responses API without creating provider-side state.
type Adapter struct {
	apiKey               string
	modelProfile         string
	identity             agentmodel.ModelIdentity
	endpoint             string
	client               *http.Client
	preflightInputTokens bool
}

var _ agentmodel.ModelAdapter = (*Adapter)(nil)

// New constructs an adapter for one approved model profile.
func New(cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.ModelProfile) == "" ||
		cfg.Identity.ProviderID != "openai" || strings.TrimSpace(cfg.Identity.ModelID) == "" || strings.TrimSpace(cfg.Identity.Version) == "" {
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
	// Credentials must never follow an API redirect to a different authority.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Adapter{
		apiKey: cfg.APIKey, modelProfile: cfg.ModelProfile, identity: cfg.Identity,
		endpoint: base + "/responses", client: client, preflightInputTokens: cfg.PreflightInputTokens,
	}, nil
}

func isLoopback(host string) bool {
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// Capabilities reports only features this adapter can preserve.
func (a *Adapter) Capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{
		ContractVersions: []int{agentmodel.ContractVersion},
		Features:         []agentmodel.ModelFeature{agentmodel.FeatureTools, agentmodel.FeatureStructuredJSON},
		OutputModes:      []agentmodel.OutputMode{agentmodel.OutputText, agentmodel.OutputJSON, agentmodel.OutputSchema},
		MaxTools:         maxFunctionTools,
	}
}

// Identity reports the exact configured provider model and revision.
func (a *Adapter) Identity() agentmodel.ModelIdentity {
	if a == nil {
		return agentmodel.ModelIdentity{}
	}
	return a.identity
}

// Invoke buffers and validates the complete provider response before returning it.
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
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: a.identity, Finish: agentmodel.FinishCancelled}, err
	}
	callCtx, cancel := context.WithDeadline(ctx, req.Deadline)
	defer cancel()
	if a.preflightInputTokens {
		if refused, err := a.checkInputTokenLimit(callCtx, req); err != nil || refused.Refusal != nil {
			return refused, err
		}
	}
	body, err := json.Marshal(makeRequest(providerModelID(a.identity), req))
	if err != nil {
		return agentmodel.ModelResult{}, fmt.Errorf("openai adapter: encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return agentmodel.ModelResult{}, fmt.Errorf("openai adapter: build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+a.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Client-Request-Id", requestIdentity(body, req.TraceID))
	response, err := a.client.Do(request)
	if err != nil {
		if callCtx.Err() != nil {
			return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: a.identity, Finish: agentmodel.FinishCancelled}, callCtx.Err()
		}
		code, retryable := agentmodel.FailureUnavailable, true
		if errors.Is(err, context.DeadlineExceeded) {
			code = agentmodel.FailureTimeout
		}
		return failureResult(a.identity, code, retryable, "provider request failed"), providerError{code: code, retryable: retryable, message: "provider request failed"}
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
	cause     error
}

func (e providerError) Error() string { return "openai adapter: " + e.message }
func (e providerError) Unwrap() error { return e.cause }

func invalidResult(identity agentmodel.ModelIdentity, message string) (agentmodel.ModelResult, error) {
	return failureResult(identity, agentmodel.FailureInvalid, false, message), providerError{code: agentmodel.FailureInvalid, message: message, cause: ErrInvalidResponse}
}

func failureResult(identity agentmodel.ModelIdentity, code agentmodel.FailureCode, retryable bool, message string) agentmodel.ModelResult {
	return agentmodel.ModelResult{
		ContractVersion: agentmodel.ContractVersion, Provider: identity,
		Failure: &agentmodel.ModelFailure{Code: code, Retryable: retryable, Message: message},
	}
}

func httpFailure(identity agentmodel.ModelIdentity, status int) (agentmodel.ModelResult, error) {
	code, retryable := agentmodel.FailureInvalid, false
	if status == http.StatusTooManyRequests || status >= 500 {
		code, retryable = agentmodel.FailureUnavailable, true
	}
	if status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout {
		code, retryable = agentmodel.FailureTimeout, true
	}
	message := fmt.Sprintf("provider returned HTTP %d", status)
	return failureResult(identity, code, retryable, message), providerError{code: code, retryable: retryable, message: message}
}
