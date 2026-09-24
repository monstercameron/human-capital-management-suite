package workload

import (
	"strings"
	"sync"
	"testing"
)

func validManifest() Manifest {
	return Manifest{Name: "api", Namespace: "hcm", CellID: "cell-a", TenantID: "tenant-a", Replicas: 2, GracefulDrainSecs: 30, MaxUnavailable: 0, MinAvailable: 1, ServiceAccount: "api", ServiceAccountRoles: []string{"api.read"}, Containers: []Container{{Name: "api", Image: "registry.example.test/hcm/api", ImageDigest: "sha256:" + strings.Repeat("a", 64), SignatureVerified: true, RunAsNonRoot: true, ReadOnlyRootFilesystem: true, ServiceAccount: "api", Resources: Resources{CPURequest: "100m", CPULimit: "500m", MemoryRequest: "128Mi", MemoryLimit: "512Mi"}, Liveness: Probe{Path: "/healthz", Port: 8080, PeriodSecs: 10, TimeoutSecs: 2, FailureThreshold: 3}, Readiness: Probe{Path: "/readyz", Port: 8080, PeriodSecs: 5, TimeoutSecs: 2, FailureThreshold: 3}}}}
}

func TestTodo_IAC_005(t *testing.T) {
	if err := Check(validManifest()); err != nil {
		t.Fatal(err)
	}
	bad := validManifest()
	c := &bad.Containers[0]
	c.ImageDigest, c.SignatureVerified, c.RunAsNonRoot, c.Privileged = "latest", false, false, true
	c.Resources = Resources{}
	c.Liveness, c.Readiness = Probe{}, Probe{}
	violations := Validate(bad)
	for _, code := range []string{"MUTABLE_IMAGE", "UNVERIFIED_IMAGE", "ROOT_IDENTITY", "PRIVILEGED_CONTAINER", "MISSING_RESOURCE_BUDGET", "MISSING_PROBE"} {
		if !hasViolation(violations, code) {
			t.Errorf("missing violation %s in %+v", code, violations)
		}
	}
}

func TestTodo_IAC_005_Golden(t *testing.T) {
	first, err := CanonicalJSON(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalJSON(validManifest())
	if err != nil || string(first) != string(second) {
		t.Fatalf("canonical manifest is not deterministic: %v", err)
	}
}

func TestTodo_IAC_005_Race(t *testing.T) {
	m := validManifest()
	want, err := Digest(m)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	digests := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Check(m); err != nil {
				errs <- err
				return
			}
			digest, err := Digest(m)
			if err != nil {
				errs <- err
				return
			}
			digests <- digest
		}()
	}
	wg.Wait()
	close(errs)
	close(digests)
	for err := range errs {
		t.Error(err)
	}
	for got := range digests {
		if got != want {
			t.Errorf("concurrent digest=%q, want %q", got, want)
		}
	}
}

func TestTodo_IAC_005_Integration(t *testing.T) {
	digest, err := Digest(validManifest())
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("manifest digest = %q, err=%v", digest, err)
	}
}

func hasViolation(violations []Violation, code string) bool {
	for _, violation := range violations {
		if violation.Code == code {
			return true
		}
	}
	return false
}
