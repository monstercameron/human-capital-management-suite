package application

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	dataconfigbundlekill "github.com/monstercameron/human-capital-management-suite/internal/data/configbundlekill"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	adminpolicy "github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const configBundleControlPath = "/admin/configbundle/"

// configBundleControl is the process-local composition of publication,
// signing, distribution, receiver application, adoption receipts, rollback,
// and scoped emergency switches. The transport handler exposes only these
// use cases; policy remains in configbundle.
type configBundleControl struct {
	mu              sync.Mutex
	now             func() time.Time
	registry        platformconfig.Store
	keys            *configbundle.InMemoryKeyring
	distributor     *configbundle.Distributor
	activator       *configbundle.Activator
	receipts        *configbundle.ReceiptStore
	killSwitches    *configbundle.KillSwitchStore
	killSwitchStore *dataconfigbundlekill.Store
	bundlePrivate   ed25519.PrivateKey
	receiptPrivate  ed25519.PrivateKey
	receiptPublic   ed25519.PublicKey
	bundles         map[string]configbundle.SignedBundle
	receivers       map[configbundle.Scope]*configbundle.Receiver
	placements      map[configbundle.Scope]tenant.Placement
}

func (c *configBundleControl) configureKillSwitchPersistence(store *dataconfigbundlekill.Store) {
	if c == nil || store == nil {
		return
	}
	c.killSwitchStore = store
	c.killSwitches.SetAppliedWriter(func(switchValue configbundle.SignedKillSwitch, receipt configbundle.AppliedKillSwitchReceipt) error {
		return store.PutApplied(context.Background(), switchValue, receipt)
	})
}

func (c *configBundleControl) reloadKillSwitches(tenantID string) error {
	if c == nil || c.killSwitchStore == nil || tenantID == "" {
		return nil
	}
	items, err := c.killSwitchStore.ListApplied(context.Background(), tenantID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := c.killSwitches.RestoreApplied(item.Switch, item.Receipt); err != nil {
			return err
		}
	}
	return nil
}

func newConfigBundleControl(cfg ServeConfig, now func() time.Time, stores ...platformconfig.Store) (*configBundleControl, error) {
	if cfg.ConfigBundleSigningSeed == "" && cfg.ConfigBundleReceiptSeed == "" {
		return nil, nil
	}
	if now == nil {
		now = time.Now
	}
	bundleSeed, err := base64.StdEncoding.DecodeString(cfg.ConfigBundleSigningSeed)
	if err != nil || len(bundleSeed) != ed25519.SeedSize {
		return nil, fmt.Errorf("application: invalid config bundle signing seed")
	}
	receiptSeed, err := base64.StdEncoding.DecodeString(cfg.ConfigBundleReceiptSeed)
	if err != nil || len(receiptSeed) != ed25519.SeedSize {
		return nil, fmt.Errorf("application: invalid config bundle receipt seed")
	}
	bundlePrivate := ed25519.NewKeyFromSeed(bundleSeed)
	receiptPrivate := ed25519.NewKeyFromSeed(receiptSeed)
	bundlePublic := bundlePrivate.Public().(ed25519.PublicKey)
	receiptPublic := append(ed25519.PublicKey(nil), receiptPrivate.Public().(ed25519.PublicKey)...)
	keyring := configbundle.NewKeyring()
	if err := keyring.Put(configbundle.BundleKey{Handle: "hcmnext-configbundle", Version: "v1", PublicKey: bundlePublic, TrustProfile: "config-control"}); err != nil {
		return nil, err
	}
	receiptSigner, err := configbundle.NewEd25519ReceiptSigner("hcmnext-configbundle-receipts", "v1", receiptPrivate)
	if err != nil {
		return nil, err
	}
	clock := func() time.Time { return now().UTC() }
	distributor, err := configbundle.NewDistributor("hcmnext-configbundle", "v1", bundlePrivate, clock)
	if err != nil {
		return nil, err
	}
	killSwitches := configbundle.NewKillSwitchStore(receiptSigner, receiptPublic)
	killSwitches.SetClock(clock)
	var registry platformconfig.Store = platformconfig.NewRegistry()
	if len(stores) > 0 && stores[0] != nil {
		registry = stores[0]
	}
	return &configBundleControl{
		now: clock, registry: registry, keys: keyring,
		distributor:   distributor,
		activator:     configbundle.NewActivator(keyring, configbundle.ActivationPolicy{RuntimeVersion: runtime.Version()}, receiptSigner, clock),
		receipts:      configbundle.NewReceiptStore(receiptSigner, clock),
		killSwitches:  killSwitches,
		bundlePrivate: bundlePrivate, receiptPrivate: receiptPrivate, receiptPublic: receiptPublic,
		bundles: make(map[string]configbundle.SignedBundle), receivers: make(map[configbundle.Scope]*configbundle.Receiver), placements: make(map[configbundle.Scope]tenant.Placement),
	}, nil
}

// overlayConfigBundleControl mounts the authenticated operator control API
// around the existing edge. The route is absent when signing keys are not
// configured, so deployments cannot accidentally expose unsigned control.
func overlayConfigBundleControl(next http.Handler, cfg transport.Config, control *configBundleControl) http.Handler {
	if control == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, configBundleControlPath) {
			control.serveHTTP(w, r, cfg)
			return
		}
		// Enforce tenant/service-scoped emergency switches at the HTTP service
		// execution boundary. The control API remains reachable for recovery.
		principal, _, err := transport.PreAdmit(r.Context(), cfg, transport.MapMetadata(r.Header), r.URL.Path)
		if err == nil && principal.SubjectKind() == trust.SubjectKindService {
			target := configbundle.KillSwitchTarget{
				TenantID: string(principal.Tenant()), Service: principal.Subject(),
			}
			if control.killSwitchStore != nil {
				guardErr := control.killSwitchStore.GuardContext(r.Context(), target, func(decision configbundle.KillDecision) error {
					if decision.Disabled {
						writeConfigControlError(w, http.StatusServiceUnavailable, "service disabled by active emergency switch")
						return nil
					}
					next.ServeHTTP(w, r)
					return nil
				})
				if guardErr != nil {
					writeConfigControlError(w, http.StatusServiceUnavailable, "emergency control state is unavailable")
				}
				return
			}
			decision := control.killSwitches.Evaluate(target, control.now())
			if decision.Disabled {
				writeConfigControlError(w, http.StatusServiceUnavailable, "service disabled by active emergency switch")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (c *configBundleControl) serveHTTP(w http.ResponseWriter, r *http.Request, cfg transport.Config) {
	method := map[string]string{
		"objects": "PublishConfigurationObject", "activate": "ActivateBundle",
		"rollback": "RollbackBundle", "ack": "AcknowledgeApplication",
		"watermark":   "GetAdoptionWatermark",
		"kill-switch": "PublishKillSwitch", "kill-switch/evaluate": "EvaluateKillSwitch",
	}[strings.TrimPrefix(r.URL.Path, configBundleControlPath)]
	if method == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	principal, _, admissionErr := transport.PreAdmit(r.Context(), cfg, transport.MapMetadata(r.Header), "/hcmnext.admin.v1.ConfigBundleService/"+method)
	if admissionErr != nil {
		writeConfigControlError(w, http.StatusUnauthorized, admissionErr.Error())
		return
	}
	if method != "AcknowledgeApplication" && adminpolicy.RequireOperator(principal) != nil {
		writeConfigControlError(w, http.StatusForbidden, "operator role required")
		return
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var request any
	switch method {
	case "PublishConfigurationObject":
		request = &publishObjectRequest{}
	case "ActivateBundle":
		request = &activateBundleRequest{}
	case "RollbackBundle":
		request = &rollbackBundleRequest{}
	case "AcknowledgeApplication":
		request = &acknowledgeApplicationRequest{}
	case "GetAdoptionWatermark":
		request = &adoptionWatermarkRequest{}
	case "PublishKillSwitch":
		request = &killSwitchRequest{}
	case "EvaluateKillSwitch":
		request = &configbundle.KillSwitchTarget{}
	}
	if err := decoder.Decode(request); err != nil {
		writeConfigControlError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeConfigControlError(w, http.StatusBadRequest, "request must contain one JSON object")
		return
	}

	var value any
	var err error
	switch typed := request.(type) {
	case *publishObjectRequest:
		value, err = c.publishObject(principal, typed.Object)
	case *activateBundleRequest:
		value, err = c.activate(principal, *typed)
	case *rollbackBundleRequest:
		value, err = c.rollback(*typed)
	case *acknowledgeApplicationRequest:
		value, err = c.acknowledge(principal, typed.Receipt)
	case *adoptionWatermarkRequest:
		value, err = c.adoptionWatermark(*typed)
	case *killSwitchRequest:
		value, err = c.publishKillSwitch(r, cfg, principal, *typed)
	case *configbundle.KillSwitchTarget:
		if err = c.reloadKillSwitches(typed.TenantID); err != nil {
			break
		}
		value = c.killSwitches.Evaluate(*typed, c.now())
	}
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, configbundle.ErrEpochConflict) || errors.Is(err, configbundle.ErrEpochReplay) || errors.Is(err, configbundle.ErrApplicationReceiptConflict) {
			code = http.StatusConflict
		}
		writeConfigControlError(w, code, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(value)
}

type publishObjectRequest struct {
	Object platformconfig.ConfigurationObject `json:"object"`
}

type activateBundleRequest struct {
	Object                platformconfig.ConfigurationObject `json:"object"`
	ObjectRef             configbundle.ObjectRef             `json:"object_ref,omitempty"`
	BundleID              string                             `json:"bundle_id"`
	MinimumRuntimeVersion string                             `json:"minimum_runtime_version"`
	Placement             tenant.Placement                   `json:"placement"`
	Epoch                 uint64                             `json:"epoch"`
}

type rollbackBundleRequest struct {
	Tenant      string                        `json:"tenant"`
	CellID      string                        `json:"cell_id"`
	PriorDigest string                        `json:"prior_digest"`
	Epoch       uint64                        `json:"epoch"`
	Revoked     []string                      `json:"revoked,omitempty"`
	Pinned      []configbundle.PinnedWorkflow `json:"pinned,omitempty"`
}

type acknowledgeApplicationRequest struct {
	Receipt configbundle.ApplicationReceipt `json:"receipt"`
}

type adoptionWatermarkRequest struct {
	TenantID      string   `json:"tenant_id"`
	CellID        string   `json:"cell_id"`
	DesiredDigest string   `json:"desired_digest"`
	Epoch         uint64   `json:"epoch"`
	Required      []string `json:"required_services"`
}

type killSwitchRequest struct {
	SwitchID       string                        `json:"switch_id"`
	Target         configbundle.KillSwitchTarget `json:"target"`
	Priority       uint32                        `json:"priority"`
	Reason         string                        `json:"reason"`
	IncidentRef    string                        `json:"incident_ref"`
	EvidenceRef    string                        `json:"evidence_ref"`
	IssuedAt       time.Time                     `json:"issued_at"`
	ExpiresAt      time.Time                     `json:"expires_at"`
	PropagationSLO time.Duration                 `json:"propagation_slo"`
}

func (c *configBundleControl) publishObject(principal *trust.Principal, object platformconfig.ConfigurationObject) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if object.PublisherPrincipal != "" && object.PublisherPrincipal != principal.Subject() {
		return nil, fmt.Errorf("publisher principal must match authenticated operator")
	}
	object.PublisherPrincipal = principal.Subject()
	if object.PublishedAt.IsZero() {
		object.PublishedAt = c.now()
	}
	published, err := platformconfig.Publish(c.registry, object)
	if err != nil {
		return nil, err
	}
	return map[string]any{"object_ref": published.Ref(), "digest": published.Digest(), "published_at": published.PublishedAt}, nil
}

func (c *configBundleControl) activate(principal *trust.Principal, request activateBundleRequest) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if request.BundleID == "" || request.Epoch == 0 || request.MinimumRuntimeVersion == "" {
		return nil, fmt.Errorf("bundle_id, positive epoch, and minimum_runtime_version are required")
	}
	var object platformconfig.ConfigurationObject
	var err error
	if request.Object.ID != "" {
		if request.Object.PublisherPrincipal != "" && request.Object.PublisherPrincipal != principal.Subject() {
			return nil, fmt.Errorf("publisher principal must match authenticated operator")
		}
		request.Object.PublisherPrincipal = principal.Subject()
		if request.Object.PublishedAt.IsZero() {
			request.Object.PublishedAt = c.now()
		}
		object = request.Object
	} else if request.ObjectRef.ID != "" {
		var found bool
		object, found, err = c.registry.GetObject(request.ObjectRef)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("configuration object reference is not published")
		}
	} else {
		return nil, fmt.Errorf("object or exact object_ref is required")
	}
	scope := configbundle.Scope{TenantID: request.Placement.Tenant, CellID: request.Placement.Cell}
	if object.Scope != scope {
		return nil, configbundle.ErrScopeMismatch
	}
	prepared, err := platformconfig.Prepare(c.registry, object)
	if err != nil {
		return nil, err
	}
	object = prepared.Object()
	evidence := platformconfig.ActivationEvidence{
		ActivatedBy: principal.Subject(), Authority: "config-control", Reason: "verified bundle compilation", ActivatedAt: c.now(),
	}
	bundle, err := configbundle.Compile(preparedConfigStore{Store: c.registry, candidate: object, evidence: evidence}, configbundle.CompileOptions{BundleID: request.BundleID, Roots: []configbundle.ObjectRef{object.Ref()}, TargetScope: scope, MinimumRuntimeVersion: request.MinimumRuntimeVersion})
	if err != nil {
		return nil, err
	}
	signed, err := configbundle.SignBundle(bundle, "hcmnext-configbundle", "v1", c.bundlePrivate)
	if err != nil {
		return nil, err
	}
	if _, err := platformconfig.CommitActivation(c.registry, prepared, evidence); err != nil {
		return nil, err
	}
	activation := configbundle.ActivationRequest{Bundle: signed, Scope: scope, TenantID: scope.TenantID,
		Environment: "PRODUCTION", TrustProfile: "config-control", Epoch: request.Epoch,
		IssuedAt: c.now().Add(-time.Minute), ExpiresAt: c.now().Add(5 * time.Minute), ReceiptSigner: c.receiptsSigner()}
	activated, err := c.activator.Activate(activation)
	if err != nil {
		return nil, err
	}
	desired, err := c.distributor.Publish(bundle, request.Placement, request.Epoch)
	if err != nil {
		return nil, err
	}
	receiver := c.receiver(scope, request.Placement)
	application, err := receiver.Receive(desired, request.Placement)
	if err != nil {
		return nil, err
	}
	c.bundles[signed.Digest] = signed
	c.placements[scope] = request.Placement
	return map[string]any{"object_digest": object.Digest(), "bundle_digest": signed.Digest, "desired_state": desired, "activation_receipt": activated, "application": application}, nil
}

// preparedConfigStore lets the compiler read a validated but unpublished
// candidate while every dependency continues to resolve from the registry.
type preparedConfigStore struct {
	platformconfig.Store
	candidate platformconfig.ConfigurationObject
	evidence  platformconfig.ActivationEvidence
}

func (s preparedConfigStore) GetObject(ref platformconfig.ObjectRef) (platformconfig.ConfigurationObject, bool, error) {
	if ref == s.candidate.Ref() {
		return s.candidate, true, nil
	}
	return s.Store.GetObject(ref)
}

func (s preparedConfigStore) GetLatestActivation(scope platformconfig.Scope, kind platformconfig.Kind, id string) (platformconfig.ActivationRecord, bool, error) {
	if scope == s.candidate.Scope && kind == s.candidate.Kind && id == s.candidate.ID {
		return platformconfig.ActivationRecord{
			Scope: s.candidate.Scope, Kind: s.candidate.Kind, ID: s.candidate.ID,
			Revision: s.candidate.Revision, ActivatedBy: s.evidence.ActivatedBy,
			Authority: s.evidence.Authority, Reason: s.evidence.Reason,
			ActivatedAt: s.evidence.ActivatedAt, ObjectDigest: s.candidate.Digest(),
		}, true, nil
	}
	return s.Store.GetLatestActivation(scope, kind, id)
}

func (c *configBundleControl) rollback(request rollbackBundleRequest) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	scope := configbundle.Scope{TenantID: request.Tenant, CellID: request.CellID}
	prior, ok := c.bundles[request.PriorDigest]
	if !ok {
		return nil, fmt.Errorf("prior bundle digest is not in this process history")
	}
	rollback, err := configbundle.Rollback(c.activator, configbundle.RollbackRequest{
		Tenant: request.Tenant, Prior: prior, PriorDigest: request.PriorDigest, NewEpoch: request.Epoch,
		Scope: scope, Environment: "PRODUCTION", TrustProfile: "config-control",
		IssuedAt: c.now().Add(-time.Minute), ExpiresAt: c.now().Add(5 * time.Minute), Revoked: request.Revoked, Pinned: request.Pinned,
		Sign: func(bundle configbundle.Bundle) (configbundle.SignedBundle, error) {
			return configbundle.SignBundle(bundle, "hcmnext-configbundle", "v1", c.bundlePrivate)
		}, ReceiptSigner: c.receiptsSigner(),
	})
	if err != nil {
		return nil, err
	}
	placement, ok := c.placements[scope]
	if !ok {
		return nil, fmt.Errorf("rollback target has no known placement")
	}
	desired, err := c.distributor.Publish(prior.Bundle, placement, request.Epoch)
	if err != nil {
		return nil, err
	}
	application, err := c.receiver(scope, placement).Receive(desired, placement)
	if err != nil {
		return nil, err
	}
	return map[string]any{"rollback_receipt": rollback, "desired_state": desired, "application": application}, nil
}

func (c *configBundleControl) acknowledge(principal *trust.Principal, receipt configbundle.ApplicationReceipt) (any, error) {
	if principal.SubjectKind() != trust.SubjectKindService || string(principal.Tenant()) != receipt.TenantID || principal.Subject() != receipt.Service {
		return nil, fmt.Errorf("application acknowledgement requires matching authenticated service identity and tenant")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	scope := configbundle.Scope{TenantID: receipt.TenantID, CellID: receipt.CellID}
	desired, found := c.distributor.Desired(scope)
	if !found || desired.Epoch != receipt.Epoch || desired.Bundle.Bundle.Digest != receipt.DesiredDigest || receipt.AppliedDigest != desired.Bundle.Bundle.Digest {
		return nil, fmt.Errorf("application acknowledgement does not match the current desired bundle and epoch")
	}
	receiver := c.receivers[scope]
	if receiver == nil {
		return nil, fmt.Errorf("receiver has not applied the desired bundle")
	}
	snapshot, applied := receiver.Snapshot()
	if !applied || snapshot.Epoch != receipt.Epoch || snapshot.BundleDigest != receipt.AppliedDigest {
		return nil, fmt.Errorf("receiver has not applied the desired bundle")
	}
	signed, err := c.receipts.Record(receipt)
	if err != nil {
		return nil, err
	}
	return map[string]any{"receipt": signed}, nil
}

func (c *configBundleControl) adoptionWatermark(request adoptionWatermarkRequest) (any, error) {
	return c.receipts.Watermark(request.TenantID, request.CellID, request.DesiredDigest, request.Epoch, request.Required)
}

func (c *configBundleControl) publishKillSwitch(r *http.Request, cfg transport.Config, principal *trust.Principal, request killSwitchRequest) (any, error) {
	if err := c.reloadKillSwitches(request.Target.TenantID); err != nil {
		return nil, err
	}
	approverToken := strings.TrimSpace(r.Header.Get("X-Approver-Authorization"))
	if strings.HasPrefix(strings.ToLower(approverToken), "bearer ") {
		approverToken = strings.TrimSpace(approverToken[len("Bearer "):])
	}
	approver, _, admissionErr := transport.PreAdmit(r.Context(), cfg, transport.MapMetadata(map[string][]string{transport.AuthorizationMetadataKey: {"Bearer " + approverToken}}), "/hcmnext.admin.v1.ConfigBundleService/ApproveKillSwitch")
	if admissionErr != nil || adminpolicy.RequireOperator(approver) != nil || approver.Subject() == principal.Subject() {
		return nil, fmt.Errorf("a distinct authenticated operator approver is required")
	}
	switchValue, err := c.killSwitches.Publish(configbundle.KillSwitchRequest{
		SwitchID: request.SwitchID, Target: request.Target, Priority: request.Priority,
		Reason: request.Reason, IncidentRef: request.IncidentRef, Operator: principal.Subject(),
		Approver: approver.Subject(), EvidenceRef: request.EvidenceRef, IssuedAt: request.IssuedAt,
		ExpiresAt: request.ExpiresAt, PropagationSLO: request.PropagationSLO,
	})
	if err != nil {
		return nil, err
	}
	receipt, err := c.killSwitches.Apply(switchValue)
	if err != nil {
		return nil, err
	}
	return map[string]any{"switch": switchValue, "applied_receipt": receipt}, nil
}

func (c *configBundleControl) receiver(scope configbundle.Scope, placement tenant.Placement) *configbundle.Receiver {
	if receiver := c.receivers[scope]; receiver != nil {
		return receiver
	}
	receiver := configbundle.NewReceiver(c.keys, c.registry, configbundle.ReceiverOptions{
		Scope: scope, Placement: placement,
		Freshness: configbundle.FreshnessPolicy{MaxAge: 24 * time.Hour, HardMaxAge: 72 * time.Hour}, Now: c.now, RuntimeVersion: runtime.Version(),
	})
	c.receivers[scope] = receiver
	return receiver
}

// runReceiver keeps the composed service subscribed to its desired-state
// distributor for the lifetime of the serve workload. A refused update is
// returned to bootstrap so the service cannot silently claim to be applying
// desired configuration while its local snapshot is stale.
func (c *configBundleControl) runReceiver(ctx context.Context, interval time.Duration) error {
	if c == nil || ctx == nil || interval <= 0 {
		return fmt.Errorf("application: invalid config bundle receiver lifecycle")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.reconcileDesired(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (c *configBundleControl) reconcileDesired() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for scope, placement := range c.placements {
		desired, ok := c.distributor.Desired(scope)
		if !ok {
			continue
		}
		if _, err := c.receiver(scope, placement).Receive(desired, placement); err != nil {
			return err
		}
	}
	return nil
}

func (c *configBundleControl) receiptsSigner() configbundle.ReceiptSigner {
	signer, _ := configbundle.NewEd25519ReceiptSigner("hcmnext-configbundle-receipts", "v1", c.receiptPrivate)
	return signer
}

func writeConfigControlError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
