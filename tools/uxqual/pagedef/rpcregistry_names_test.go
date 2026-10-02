package pagedef

import (
	"testing"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
)

// TestServiceNamesMatchGeneratedServiceDescs holds the service names, which
// are derived from generated full-method-name constants so the browser bundle
// does not carry the services' server handlers, to the names the generated
// ServiceDesc values declare.
func TestServiceNamesMatchGeneratedServiceDescs(t *testing.T) {
	for _, tc := range []struct {
		label string
		got   string
		want  string
	}{
		{"journey", JourneyServiceName, journeyv1.JourneyService_ServiceDesc.ServiceName},
		{"intents", IntentServiceName, intentsv1.IntentService_ServiceDesc.ServiceName},
		{"registry", RegistryServiceName, registryv1.RegistryService_ServiceDesc.ServiceName},
		{"admin", AdminServiceName, adminv1.AdminService_ServiceDesc.ServiceName},
	} {
		if tc.got == "" || tc.got != tc.want {
			t.Fatalf("%s service name = %q, want %q", tc.label, tc.got, tc.want)
		}
	}
	if got := serviceNameOf("/hcmnext.admin.v1.AdminService/ListIntents"); got != "hcmnext.admin.v1.AdminService" {
		t.Fatalf("serviceNameOf = %q", got)
	}
	if got := serviceNameOf("NoSlash"); got != "NoSlash" {
		t.Fatalf("serviceNameOf without a slash = %q", got)
	}
}

// TestRegistryAndAdminLiteralsMatchServiceDescs holds the literal RPC lists
// the browser bundle uses (rpcregistry_wasm.go) to what the generated admin
// and registry ServiceDesc values register, and holds the full known-RPC set
// the two builds produce to be the same.
func TestRegistryAndAdminLiteralsMatchServiceDescs(t *testing.T) {
	for _, tc := range []struct {
		label    string
		literals []string
		desc     []string
	}{
		{"registry", registryMethodNames, registryServiceRPCs()},
		{"admin", adminMethodNames, adminServiceRPCs()},
	} {
		want := map[string]bool{}
		for _, rpc := range tc.desc {
			want[rpc] = true
		}
		if len(tc.literals) != len(want) {
			t.Fatalf("%s literal list has %d RPCs, the ServiceDesc registers %d: %v", tc.label, len(tc.literals), len(want), tc.desc)
		}
	}
	registry := serviceMethods(RegistryServiceName, registryMethodNames, nil)
	admin := serviceMethods(AdminServiceName, adminMethodNames, nil)
	for label, pair := range map[string][2][]string{"registry": {registry, registryServiceRPCs()}, "admin": {admin, adminServiceRPCs()}} {
		if len(pair[0]) != len(pair[1]) {
			t.Fatalf("%s: literal-built list %v, descriptor-built list %v", label, pair[0], pair[1])
		}
		for i := range pair[0] {
			if pair[0][i] != pair[1][i] {
				t.Fatalf("%s RPC %d: literal %q, ServiceDesc %q", label, i, pair[0][i], pair[1][i])
			}
		}
	}
}
