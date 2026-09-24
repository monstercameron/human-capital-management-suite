package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/health"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/cache"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type rev03202Probe struct {
	mu    sync.Mutex
	calls map[uuid.UUID]int
}

func (p *rev03202Probe) Run(_ context.Context, tenant uuid.UUID) health.Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls[tenant]++
	return health.Snapshot{SchemaVersion: 1, Tenant: tenant, State: health.StateUnknown, Reasons: []string{"probe unavailable"}}
}

func TestTodo_REV_032_02(t *testing.T) {
	probe := &rev03202Probe{calls: make(map[uuid.UUID]int)}
	verifier := rev03202Verifier(t)
	handler := overlayStoreHealth(http.NotFoundHandler(), transport.Config{Verifier: verifier}, probe, cache.New[health.Snapshot](cache.Config{TTL: time.Minute}))

	request := httptest.NewRequest(http.MethodGet, storeHealthPath, nil)
	request.Header.Set("Authorization", "Bearer operator-a")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want UNKNOWN status %d; body %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
	var got health.Snapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if got.State != health.StateUnknown || got.Tenant != rev03202Tenant("tenant-a") {
		t.Fatalf("snapshot = tenant %s state %s, want tenant-a and UNKNOWN", got.Tenant, got.State)
	}
	again := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, storeHealthPath, nil)
	secondRequest.Header.Set("Authorization", "Bearer operator-a")
	handler.ServeHTTP(again, secondRequest)
	if again.Code != http.StatusServiceUnavailable {
		t.Fatalf("second health status = %d, want UNKNOWN's 503", again.Code)
	}
	probe.mu.Lock()
	calls := probe.calls[rev03202Tenant("tenant-a")]
	probe.mu.Unlock()
	if calls != 1 {
		t.Fatalf("two reads for the same tenant called the source %d times, want 1 cache fill", calls)
	}
	for name, token := range map[string]string{"ordinary": "ordinary", "anonymous": ""} {
		deniedRequest := httptest.NewRequest(http.MethodGet, storeHealthPath, nil)
		if token != "" {
			deniedRequest.Header.Set("Authorization", "Bearer "+token)
		}
		denied := httptest.NewRecorder()
		handler.ServeHTTP(denied, deniedRequest)
		if denied.Code != http.StatusForbidden {
			t.Errorf("%s status = %d, want forbidden", name, denied.Code)
		}
	}
}

func TestTodo_REV_032_02_Integration(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cfg := stubServeConfig()
	cfg.DatabaseURL = db.URL
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg, Pool: pool, Identity: "rev-032-02-integration",
		Options: Options{}.Apply(WithVerifier(rev03202Verifier(t))),
	})
	if err != nil {
		t.Fatalf("ComposeServe against PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := composed.Stop(ctx); err != nil {
			t.Errorf("stop composed serve app: %v", err)
		}
	})
	if node, ok := composed.Graph().Component(ComponentStoreHealth); !ok || node.Impl != "*health.Probe" {
		t.Fatalf("store-health graph component = %+v, present=%t; want the composed health.Probe", node, ok)
	}
	if err := composed.Start(context.Background()); err != nil {
		t.Fatalf("start composed serve app: %v", err)
	}
	// A closed authoritative pool makes the real health probe publish UNKNOWN.
	// The two operator principals still exercise independent tenant keys and
	// receive only their own UUID-derived snapshot over the running HTTP edge.
	pool.Close()

	get := func(token string) health.Snapshot {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, "http://"+composed.HTTPAddr()+storeHealthPath, nil)
		if err != nil {
			t.Fatalf("build GET as %s: %v", token, err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("GET as %s from composed HTTP server: %v", token, err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("GET as %s: status = %d, want UNKNOWN's 503", token, response.StatusCode)
		}
		var snapshot health.Snapshot
		if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
			t.Fatalf("decode GET as %s: %v", token, err)
		}
		if snapshot.State != health.StateUnknown {
			t.Fatalf("GET as %s state = %s, want UNKNOWN", token, snapshot.State)
		}
		return snapshot
	}

	firstA := get("operator-a")
	snapshotB := get("operator-b")
	secondA := get("operator-a")
	if firstA.Tenant != rev03202Tenant("tenant-a") || secondA.Tenant != firstA.Tenant {
		t.Fatalf("tenant-a snapshots = %s, %s; both must be tenant-a", firstA.Tenant, secondA.Tenant)
	}
	if snapshotB.Tenant != rev03202Tenant("tenant-b") || snapshotB.Tenant == firstA.Tenant {
		t.Fatalf("tenant-b snapshot = %s, want isolated tenant-b %s", snapshotB.Tenant, rev03202Tenant("tenant-b"))
	}
}

func rev03202Tenant(tenant string) uuid.UUID { return pgstore.TenantID(tenant) }

func rev03202Verifier(t *testing.T) trust.Verifier {
	t.Helper()
	return trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		tenant := "tenant-a"
		roles := []string{admin.OperatorRole}
		switch credential.Token {
		case "operator-a":
		case "operator-b":
			tenant = "tenant-b"
		case "ordinary":
			roles = nil
		default:
			return nil, fmt.Errorf("unknown fixture credential")
		}
		principal, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant: values.TenantId(tenant), Subject: "test-operator", SubjectKind: trust.SubjectKindHuman,
			Roles: roles, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance: trust.AssuranceHigh, SessionRef: "test-session", IssuedAt: time.Now().Add(-time.Minute),
			ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "fixture:" + credential.Token,
		})
		if err != nil {
			return nil, fmt.Errorf("build fixture principal: %w", err)
		}
		return principal, nil
	})
}
