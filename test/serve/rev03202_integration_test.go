package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/health"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_REV_032_02_Integration exercises the installed command boundary:
// it builds cmd/hcmnext, starts that process on OS-selected loopback ports,
// and requests store health through the process's HTTP listener. Its empty
// isolated schema makes the live probe return UNKNOWN. Two signed operator
// credentials prove both tenant partitioning and the cache hit for tenant-a.
func TestTodo_REV_032_02_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	grpcAddr, httpAddr := freeLoopbackAddr(t), freeLoopbackAddr(t)
	binary := buildHCMNext(t)
	process := startHealthServe(t, binary, schemaURL(t, db.URL, db.Schema), grpcAddr, httpAddr)
	t.Cleanup(func() { process.forceStop(t) })

	client := &http.Client{Timeout: 3 * time.Second}
	tokenA := mintStoreHealthToken(t, "tenant-a", "operator-a", true)
	tokenB := mintStoreHealthToken(t, "tenant-b", "operator-b", true)
	tokenOrdinary := mintStoreHealthToken(t, "tenant-a", "ordinary", false)
	waitForStoreHealth(t, process, client, httpAddr, tokenA)

	firstA := fetchStoreHealth(t, client, httpAddr, tokenA)
	snapshotB := fetchStoreHealth(t, client, httpAddr, tokenB)
	secondA := fetchStoreHealth(t, client, httpAddr, tokenA)
	if firstA.State != health.StateUnknown || snapshotB.State != health.StateUnknown {
		t.Fatalf("health states = %s, %s; want UNKNOWN for both tenants", firstA.State, snapshotB.State)
	}
	if firstA.Tenant != pgstore.TenantID("tenant-a") || secondA.Tenant != firstA.Tenant {
		t.Fatalf("tenant-a snapshots = %s and %s; want the same tenant-a UUID %s", firstA.Tenant, secondA.Tenant, pgstore.TenantID("tenant-a"))
	}
	if snapshotB.Tenant != pgstore.TenantID("tenant-b") || snapshotB.Tenant == firstA.Tenant {
		t.Fatalf("tenant-b snapshot = %s; want isolated tenant-b UUID %s", snapshotB.Tenant, pgstore.TenantID("tenant-b"))
	}
	if firstA.Digest == "" || firstA.Digest != secondA.Digest {
		t.Fatalf("repeated tenant-a digest = %q, %q; want one cached snapshot", firstA.Digest, secondA.Digest)
	}
	if got := fetchStoreHealthStatus(t, client, httpAddr, tokenOrdinary); got != http.StatusForbidden {
		t.Fatalf("ordinary signed principal status = %d, want forbidden", got)
	}
	request, err := http.NewRequest(http.MethodGet, "http://"+httpAddr+"/admin/ops/store-health", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("anonymous request to child process: %v\nprocess output:\n%s", err, process.output())
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous status = %d, want forbidden", response.StatusCode)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := process.stop(stopCtx); err != nil {
		t.Fatalf("stop child hcmnext: %v\nprocess output:\n%s", err, process.output())
	}
}

func startHealthServe(t *testing.T, binary, databaseURL, grpcAddr, httpAddr string) *serveProcess {
	t.Helper()
	p := &serveProcess{done: make(chan struct{}), grpcAddr: grpcAddr, httpAddr: httpAddr}
	p.cmd = exec.Command(binary,
		"serve",
		"-grpc-listen="+grpcAddr,
		"-http-listen="+httpAddr,
		"-database-url="+databaseURL,
		"-dev-hmac-key="+serveKey,
		"-page-cursor-key=hcmnext-rev03202-cursor-signing-key-32+",
		"-issuer="+serveIssuer,
		"-audience="+serveAudience,
		"-tenant=",
		"-migrate=false",
		"-execution-authority=false",
		"-scheduler=false",
		"-workspace=false",
		"-otel-exporter=none",
	)
	p.cmd.Stdout = &p.logs
	p.cmd.Stderr = &p.logs
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("start isolated hcmnext process: %v", err)
	}
	go func() {
		err := p.cmd.Wait()
		p.mu.Lock()
		p.exit = err
		p.mu.Unlock()
		close(p.done)
	}()
	return p
}

func mintStoreHealthToken(t *testing.T, tenant, subject string, operator bool) string {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(serveKey), Issuer: serveIssuer, Audience: serveAudience,
	})
	if err != nil {
		t.Fatalf("configure test token signer: %v", err)
	}
	roles := []string{}
	if operator {
		roles = append(roles, admin.OperatorRole)
	}
	issued := time.Now().UTC()
	token, err := verifier.Issue(trust.Claims{
		Issuer: serveIssuer, Audience: serveAudience, Subject: subject, SubjectKind: "human", Tenant: tenant,
		Roles: roles, Purposes: []string{"operator_diagnostics"}, AuthenticationMethod: "bearer_token",
		Assurance: "substantial", SessionRef: "rev03202-" + subject,
		IssuedAtUnix: issued.Add(-time.Minute).Unix(), ExpiresAtUnix: issued.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("mint test token: %v", err)
	}
	return token
}

func waitForStoreHealth(t *testing.T, process *serveProcess, client *http.Client, httpAddr, token string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if exited, exitErr := process.exited(); exited {
			t.Fatalf("hcmnext exited before store-health endpoint became ready: %v\n%s", exitErr, process.output())
		}
		request, _ := http.NewRequest(http.MethodGet, "http://"+httpAddr+"/admin/ops/store-health", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusServiceUnavailable {
				return
			}
			lastErr = fmt.Errorf("store-health readiness status: HTTP %d", response.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for store-health endpoint: %v\nprocess output:\n%s", lastErr, process.output())
}

func fetchStoreHealth(t *testing.T, client *http.Client, httpAddr, token string) health.Snapshot {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "http://"+httpAddr+"/admin/ops/store-health", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request store health: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("store-health status = %d, want UNKNOWN's 503", response.StatusCode)
	}
	var snapshot health.Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode store-health response: %v", err)
	}
	return snapshot
}

func fetchStoreHealthStatus(t *testing.T, client *http.Client, httpAddr, token string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "http://"+httpAddr+"/admin/ops/store-health", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request store health: %v", err)
	}
	_ = response.Body.Close()
	return response.StatusCode
}
