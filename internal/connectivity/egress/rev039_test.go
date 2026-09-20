package egress

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

// rev039Gateway builds the standard test gateway with a counting
// transport and an optional CONNECT opt-in.
func rev039Gateway(t *testing.T, allowCONNECT bool) (*Gateway, *roundTripper) {
	t.Helper()
	transport := &roundTripper{fn: okResponse}
	outboundPolicy, err := outbound.NewPolicy(outbound.Destination{Name: "api.example.test", TrustBundleRef: "bundle:v1", Purposes: []string{"promotion.read"}, DataClasses: []string{"PUBLIC", "PII"}})
	if err != nil {
		t.Fatal(err)
	}
	dlpPolicy, err := dlp.NewPolicy(outboundPolicy, dlp.Clearance{Destination: "api.example.test", DataClass: dlp.ClassPublic, Decision: dlp.Allow}, dlp.Clearance{Destination: "api.example.test", DataClass: dlp.ClassPII, Decision: dlp.Refuse})
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := url.Parse("http://egress-proxy.example.test:8080")
	gateway, err := New(Config{Outbound: outboundPolicy, DLP: dlpPolicy, ProxyURL: proxy, Resolver: egressResolver{"api.example.test": {netip.MustParseAddr("192.0.2.10")}}, Transport: transport, AllowCONNECT: allowCONNECT, Now: func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return gateway, transport
}

func rev039Request(method string) Request {
	return Request{Method: method, Target: "https://api.example.test/v1", Purpose: "promotion.read", Principal: "worker/worker-1", Payload: []byte("safe"), DataClasses: []dlp.DataClass{dlp.ClassPublic}}
}

// TestTodo_REV_039_02 proves non-repository methods never reach the
// proxy: CONNECT, TRACE, TRACK and unknown methods are refused before any
// network or DLP activity, with no transport call and no receipt.
func TestTodo_REV_039_02(t *testing.T) {
	gateway, transport := rev039Gateway(t, false)
	for _, method := range []string{http.MethodConnect, http.MethodTrace, "TRACK", "FOO"} {
		result, err := gateway.Do(context.Background(), rev039Request(method))
		if !errors.Is(err, ErrUnsafeMethod) {
			t.Errorf("%s: Do err = %v, want ErrUnsafeMethod", method, err)
		}
		if result.Response != nil {
			t.Errorf("%s: refused request produced a response", method)
		}
		if len(result.Receipts) != 0 {
			t.Errorf("%s: refused request produced %d receipts", method, len(result.Receipts))
		}
	}
	if transport.calls != 0 {
		t.Fatalf("refused methods reached the transport %d times", transport.calls)
	}
	if err := gateway.receipts.Verify(); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_REV_039_02_Security proves the CONNECT opt-in is explicit and
// narrow: CONNECT passes only when the gateway opts in, TRACE never
// passes even then, and the repository methods keep working either way.
func TestTodo_REV_039_02_Security(t *testing.T) {
	open, transport := rev039Gateway(t, true)
	if _, err := open.Do(context.Background(), rev039Request(http.MethodConnect)); err != nil {
		t.Fatalf("opted-in CONNECT refused: %v", err)
	}
	if transport.calls != 1 {
		t.Fatalf("opted-in CONNECT made %d transport calls, want 1", transport.calls)
	}
	if _, err := open.Do(context.Background(), rev039Request(http.MethodTrace)); !errors.Is(err, ErrUnsafeMethod) {
		t.Fatalf("TRACE with CONNECT opt-in err = %v, want ErrUnsafeMethod", err)
	}
	if _, err := open.Do(context.Background(), rev039Request(http.MethodGet)); err != nil {
		t.Fatalf("GET with CONNECT opt-in refused: %v", err)
	}
	closed, _ := rev039Gateway(t, false)
	if _, err := closed.Do(context.Background(), rev039Request(http.MethodGet)); err != nil {
		t.Fatalf("GET refused: %v", err)
	}
}

// TestTodo_REV_039_02_Race proves the method gate holds under concurrent
// mixed use: refused CONNECTs never leak transport calls while allowed
// GETs all complete, and the receipt log stays consistent.
func TestTodo_REV_039_02_Race(t *testing.T) {
	gateway, transport := rev039Gateway(t, false)
	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			method := http.MethodGet
			if i%2 == 0 {
				method = http.MethodConnect
			}
			_, errs[i] = gateway.Do(context.Background(), rev039Request(method))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if i%2 == 0 {
			if !errors.Is(err, ErrUnsafeMethod) {
				t.Errorf("worker %d CONNECT err = %v, want ErrUnsafeMethod", i, err)
			}
		} else if err != nil {
			t.Errorf("worker %d GET err = %v", i, err)
		}
	}
	if transport.calls != workers/2 {
		t.Fatalf("transport calls = %d, want %d allowed GETs", transport.calls, workers/2)
	}
	if err := gateway.receipts.Verify(); err != nil {
		t.Fatal(err)
	}
}
