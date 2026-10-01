package private

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

const maxBodyBytes = 4 << 20

var (
	ErrNotConfigured = errors.New("private model adapter: not configured")
	ErrEndpointDrift = errors.New("private model adapter: endpoint registration changed")
	ErrEndpointTrust = errors.New("private model adapter: endpoint trust verification failed")
)

// Registration is an administrator-approved immutable endpoint declaration.
// The model protocol is the versioned agentmodel JSON contract.
type Registration struct {
	ID              string
	Profile         string
	EndpointURL     string
	NetworkPathRef  string
	ServiceIdentity string
	Identity        agentmodel.ModelIdentity
	Region          string
	DataClasses     []string
	Retention       string
	TrainingUse     agentmodel.ProcessingUse
	Logging         agentmodel.ProcessingUse
	HealthPath      string
	InferencePath   string
	Capabilities    agentmodel.AdapterCapabilities
	Approved        bool
}

// Registry resolves the current admin-owned registration on every call.
type Registry interface {
	ResolvePrivateEndpoint(context.Context, string) (Registration, error)
}

// EndpointTransport supplies a client that enforces registered service
// identity and egress network path on the actual connection.
type EndpointTransport interface {
	ClientForPrivateEndpoint(context.Context, Registration) (*http.Client, error)
}

// Config pins one private endpoint registration and its expected digest.
type Config struct {
	EndpointID string
	Pin        string
	Registry   Registry
	Transport  EndpointTransport
}

// Adapter invokes only the endpoint represented by its pinned admin record.
type Adapter struct {
	registration Registration
	pin          string
	registry     Registry
	transport    EndpointTransport
}

// New constructs an adapter from a reviewed endpoint registration.
func New(ctx context.Context, cfg Config) (*Adapter, error) {
	if ctx == nil || cfg.Registry == nil || cfg.Transport == nil || strings.TrimSpace(cfg.EndpointID) == "" || cfg.Pin == "" {
		return nil, ErrNotConfigured
	}
	registration, err := cfg.Registry.ResolvePrivateEndpoint(ctx, cfg.EndpointID)
	if err != nil || !registration.Approved || registration.ID != cfg.EndpointID || !validRegistration(registration) || registrationPin(registration) != cfg.Pin {
		return nil, ErrNotConfigured
	}
	return &Adapter{registration: cloneRegistration(registration), pin: cfg.Pin, registry: cfg.Registry, transport: cfg.Transport}, nil
}

// RegistrationPin returns a stable digest for an admin registration.
func RegistrationPin(reg Registration) string { return registrationPin(reg) }

// Identity reports the exact identity approved in the private endpoint record.
func (a *Adapter) Identity() agentmodel.ModelIdentity {
	if a == nil {
		return agentmodel.ModelIdentity{}
	}
	return a.registration.Identity
}

// Capabilities reports the endpoint's administrator-qualified capabilities.
func (a *Adapter) Capabilities() agentmodel.AdapterCapabilities {
	if a == nil {
		return agentmodel.AdapterCapabilities{}
	}
	return cloneCapabilities(a.registration.Capabilities)
}

// Invoke re-resolves and verifies the exact endpoint registration, checks its
// health contract, then sends one bounded provider-neutral request.
func (a *Adapter) Invoke(ctx context.Context, req agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	if a == nil || a.registry == nil || a.transport == nil {
		return agentmodel.ModelResult{}, ErrNotConfigured
	}
	if ctx == nil {
		return agentmodel.ModelResult{}, errors.New("private model adapter: context is required")
	}
	if err := agentmodel.ValidateModelRequest(req); err != nil {
		return agentmodel.ModelResult{}, err
	}
	if req.ModelProfile != a.registration.Profile {
		return agentmodel.ModelResult{}, &agentmodel.ModelRefusal{Code: agentmodel.RefusalFeature, Detail: "request profile is not configured for this adapter"}
	}
	if refusal := agentmodel.CheckCapabilities(req, a.registration.Capabilities); refusal != nil {
		return agentmodel.ModelResult{}, refusal
	}
	if err := ctx.Err(); err != nil {
		return cancelled(a.registration.Identity, err)
	}
	callCtx, cancel := context.WithDeadline(ctx, req.Deadline)
	defer cancel()
	current, err := a.registry.ResolvePrivateEndpoint(callCtx, a.registration.ID)
	if err != nil {
		return unavailable(a.registration.Identity, "endpoint registration unavailable", err)
	}
	if !current.Approved || registrationPin(current) != a.pin {
		return agentmodel.ModelResult{}, ErrEndpointDrift
	}
	if err := validateRequestTerms(current, req); err != nil {
		return agentmodel.ModelResult{}, err
	}
	client, err := a.transport.ClientForPrivateEndpoint(callCtx, current)
	if err != nil || client == nil {
		return agentmodel.ModelResult{}, fmt.Errorf("%w: service identity or network path rejected", ErrEndpointTrust)
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if err := a.health(callCtx, &clientCopy, current); err != nil {
		return unavailable(current.Identity, "endpoint health contract failed", err)
	}
	return a.invoke(callCtx, &clientCopy, current, req)
}

func (a *Adapter) health(ctx context.Context, client *http.Client, reg Registration) error {
	body, _ := json.Marshal(struct {
		EndpointID      string `json:"endpoint_id"`
		RegistrationPin string `json:"registration_pin"`
	}{reg.ID, a.pin})
	response, err := a.post(ctx, client, reg, reg.HealthPath, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var health struct {
		Status          string `json:"status"`
		EndpointID      string `json:"endpoint_id"`
		RegistrationPin string `json:"registration_pin"`
	}
	if response.StatusCode != http.StatusOK || decodeBounded(response.Body, &health) != nil || health.Status != "ok" || health.EndpointID != reg.ID || health.RegistrationPin != a.pin {
		return errors.New("health response did not match the registered endpoint")
	}
	return nil
}

func (a *Adapter) invoke(ctx context.Context, client *http.Client, reg Registration, req agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return agentmodel.ModelResult{}, fmt.Errorf("private model adapter: encode request: %w", err)
	}
	response, err := a.post(ctx, client, reg, reg.InferencePath, body)
	if err != nil {
		return unavailable(reg.Identity, "private endpoint request failed", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return unavailable(reg.Identity, "private endpoint returned a non-success status", fmt.Errorf("status %d", response.StatusCode))
	}
	var result agentmodel.ModelResult
	if err := decodeBounded(response.Body, &result); err != nil {
		return invalid(reg.Identity, "private endpoint returned invalid contract JSON")
	}
	if err := agentmodel.ValidateModelResult(req, result, reg.Capabilities); err != nil {
		return invalid(reg.Identity, "private endpoint result failed contract validation")
	}
	if result.Provider != reg.Identity {
		return invalid(reg.Identity, "private endpoint result identity changed")
	}
	return result, nil
}

func (a *Adapter) post(ctx context.Context, client *http.Client, reg Registration, path string, body []byte) (*http.Response, error) {
	u, _ := url.Parse(reg.EndpointURL)
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	return client.Do(request)
}

func validateRequestTerms(reg Registration, req agentmodel.ModelRequest) error {
	if req.Processing.Residency != reg.Region || req.Processing.Retention != reg.Retention ||
		!processingSatisfies(reg.TrainingUse, req.Processing.TrainingUse) || !processingSatisfies(reg.Logging, req.Processing.Logging) {
		return &agentmodel.ModelRefusal{Code: agentmodel.RefusalFeature, Detail: "request processing policy does not match the registered endpoint terms"}
	}
	return nil
}

func validRegistration(r Registration) bool {
	u, err := url.Parse(r.EndpointURL)
	return strings.TrimSpace(r.ID) != "" && strings.TrimSpace(r.Profile) != "" && strings.TrimSpace(r.NetworkPathRef) != "" &&
		strings.TrimSpace(r.ServiceIdentity) != "" && strings.TrimSpace(r.Identity.ProviderID) != "" && strings.TrimSpace(r.Identity.ModelID) != "" && strings.TrimSpace(r.Identity.Version) != "" &&
		strings.TrimSpace(r.Region) != "" && len(r.DataClasses) > 0 && strings.TrimSpace(r.Retention) != "" && validUse(r.TrainingUse) && validUse(r.Logging) &&
		validPath(r.HealthPath) && validPath(r.InferencePath) && validCapabilities(r.Capabilities) && err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Scheme == "https" || u.Scheme == "http" && loopback(u.Hostname()))
}

func validPath(path string) bool {
	return path != "" && strings.HasPrefix(path, "/") && !strings.Contains(path, "..") && !strings.Contains(path, "?") && !strings.Contains(path, "#")
}
func loopback(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
func validUse(v agentmodel.ProcessingUse) bool {
	return v == agentmodel.UseAllowed || v == agentmodel.UseDenied
}
func processingSatisfies(registered, requested agentmodel.ProcessingUse) bool {
	return registered == requested
}

func validCapabilities(c agentmodel.AdapterCapabilities) bool {
	if len(c.ContractVersions) == 0 || len(c.OutputModes) == 0 {
		return false
	}
	for _, version := range c.ContractVersions {
		if version != agentmodel.ContractVersion {
			return false
		}
	}
	return c.MaxTools >= 0
}

func registrationPin(r Registration) string {
	copy := cloneRegistration(r)
	slices.Sort(copy.DataClasses)
	slices.Sort(copy.Capabilities.Features)
	slices.Sort(copy.Capabilities.OutputModes)
	versions := append([]int(nil), copy.Capabilities.ContractVersions...)
	sort.Ints(versions)
	copy.Capabilities.ContractVersions = versions
	canonical, _ := json.Marshal(copy)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func cloneRegistration(r Registration) Registration {
	r.DataClasses = append([]string(nil), r.DataClasses...)
	r.Capabilities = cloneCapabilities(r.Capabilities)
	return r
}
func cloneCapabilities(c agentmodel.AdapterCapabilities) agentmodel.AdapterCapabilities {
	c.ContractVersions = append([]int(nil), c.ContractVersions...)
	c.Features = append([]agentmodel.ModelFeature(nil), c.Features...)
	c.OutputModes = append([]agentmodel.OutputMode(nil), c.OutputModes...)
	return c
}

func decodeBounded(reader io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(reader, maxBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxBodyBytes {
		return errors.New("response exceeded size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func cancelled(identity agentmodel.ModelIdentity, err error) (agentmodel.ModelResult, error) {
	return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: identity, Finish: agentmodel.FinishCancelled}, err
}
func unavailable(identity agentmodel.ModelIdentity, message string, err error) (agentmodel.ModelResult, error) {
	code := agentmodel.FailureUnavailable
	if errors.Is(err, context.DeadlineExceeded) {
		code = agentmodel.FailureTimeout
	}
	return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: identity, Failure: &agentmodel.ModelFailure{Code: code, Retryable: true, Message: message}}, err
}
func invalid(identity agentmodel.ModelIdentity, message string) (agentmodel.ModelResult, error) {
	return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: identity, Failure: &agentmodel.ModelFailure{Code: agentmodel.FailureInvalid, Message: message}}, errors.New("private model adapter: " + message)
}

// Compile-time assertion keeps private endpoints on the shared model port.
var _ agentmodel.ModelAdapter = (*Adapter)(nil)
