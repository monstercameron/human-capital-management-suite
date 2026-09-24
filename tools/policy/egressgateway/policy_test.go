package egressgateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodo_REV_033_02(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	violations, err := ScanRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("connectivity packages directly construct outbound http.Clients (an egress import does not authorize a bypass): %+v", violations)
	}
}

func TestTodo_REV_033_02_Security(t *testing.T) {
	source := `package bypass
import "net/http"
var client = &http.Client{}
`
	client, gateway, err := AnalyzeSource("bypass.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if !RequiresGateway(client, gateway) {
		t.Fatalf("fixture client=%v gateway=%v; want direct client construction without gateway", client, gateway)
	}
	secured := `package secured
import (
 "net/http"
 "github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
)
var client = &http.Client{}
var _ *egress.Gateway
`
	client, gateway, err = AnalyzeSource("secured.go", secured)
	if err != nil {
		t.Fatal(err)
	}
	if RequiresGateway(client, gateway) {
		t.Fatal("client construction with egress dependency was rejected")
	}
	if !ForbiddenConstruction(client) {
		t.Fatal("direct http.Client construction was accepted merely because egress was imported")
	}
	defaultClient := `package bypass
import "net/http"
var client = http.DefaultClient
`
	client, gateway, err = AnalyzeSource("default_client.go", defaultClient)
	if err != nil {
		t.Fatal(err)
	}
	if !client || gateway || !ForbiddenConstruction(client) {
		t.Fatalf("http.DefaultClient bypass was not rejected: client=%v gateway=%v", client, gateway)
	}
	adapter := `package adapter
import "github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
var gateway *egress.Gateway
`
	client, gateway, err = AnalyzeSource("adapter.go", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if ForbiddenConstruction(client) || !gateway {
		t.Fatal("gateway adapter without a direct client was rejected")
	}
}

func TestTodo_REV_033_02_Integration(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"oauthcc", "iamsim", "providerdelivery", "payrollsim"} {
		path := filepath.Join(root, "internal", "connectivity", pkg)
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		constructed := false
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(path, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			client, gateway, err := AnalyzeSource(entry.Name(), string(data))
			if err != nil {
				t.Fatal(err)
			}
			found = found || gateway
			constructed = constructed || client
		}
		if !found {
			t.Errorf("%s must depend on the egress gateway port", pkg)
		}
		if constructed {
			t.Errorf("%s constructs http.Client directly instead of using its HTTP port", pkg)
		}
	}
}
