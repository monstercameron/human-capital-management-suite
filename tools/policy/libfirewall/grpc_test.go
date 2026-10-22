package libfirewall_test

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall"
)

func loadFirewallConfig(t *testing.T) *libfirewall.Config {
	t.Helper()
	root := repopath.RootDir()
	c, err := libfirewall.LoadConfig(filepath.Join(root, "definitions", "architecture", "library-firewall.yaml"))
	if err != nil {
		t.Fatalf("loading library-firewall config: %v", err)
	}
	return c
}

// TestGRPCBackendQualification is the LIB-003 primary test: Protobuf and
// grpc-go (plus the Connect/genproto-status modules that ride alongside
// them on the wire) are qualified as wire/RPC mechanics only. Their shared
// scan uses the union of reviewed wire/client/transport roots; LIB-002 still
// enforces each imported module's exact dependency-roles.yaml row, including
// the distinction between cmd's grpc/connect access and protobuf access.
// Every other business package importing them directly is a violation.
func TestGRPCBackendQualification(t *testing.T) {
	cfg := loadFirewallConfig(t)
	mod := cfg.Module

	cases := []struct {
		name     string
		importer string
		imported string
		wantV    bool
	}{
		{"gen may import protobuf", mod + "/gen/go/hcm/v1", "google.golang.org/protobuf/proto", false},
		{"internal/transport may import grpc", mod + "/internal/transport/grpcserver", "google.golang.org/grpc", false},
		{"internal/intent/protomap may import protobuf", mod + "/internal/intent/protomap", "google.golang.org/protobuf/types/known/timestamppb", false},
		{"wire canonical byte engine may reflect protobuf", mod + "/internal/engines/wire/canonical", "google.golang.org/protobuf/reflect/protoreflect", false},
		{"wire digest registry may reflect protobuf", mod + "/internal/engines/wire/digest", "google.golang.org/protobuf/proto", false},
		{"client generator may inspect protobuf descriptors", mod + "/tools/gen/clients", "google.golang.org/protobuf/reflect/protoregistry", false},
		{"product client may map grpc status", mod + "/tools/uxqual/productclient", "google.golang.org/grpc/status", false},
		{"reference kiosk consumes public protobuf device messages", mod + "/internal/timeclockapp", "google.golang.org/protobuf/encoding/protojson", false},
		{"clock partner simulator consumes public protobuf messages", mod + "/tools/clockpartner", "google.golang.org/protobuf/types/known/timestamppb", false},
		{"clock partner SDK reflects canonical protobuf descriptors", mod + "/tools/timeclockkit", "google.golang.org/protobuf/reflect/protoreflect", false},
		{"cmd may import grpc to wire the server", mod + "/cmd/hcmnext", "google.golang.org/grpc", false},
		{"cmd may import connect to wire the edge", mod + "/cmd/hcmnext", "connectrpc.com/connect", false},
		{"internal/intent (not protomap) importing protobuf is forbidden", mod + "/internal/intent", "google.golang.org/protobuf/types/known/structpb", true},
		{"application response mapping may import protobuf", mod + "/internal/application", "google.golang.org/protobuf/types/known/timestamppb", false},
		{"intent application mapping may import protobuf", mod + "/internal/intent/app", "google.golang.org/protobuf/proto", false},
		{"project workflow client may import protobuf", mod + "/tools/uxqual/projectclient", "google.golang.org/protobuf/proto", false},
		{"an unlisted intent package importing protobuf is forbidden", mod + "/internal/intent/other", "google.golang.org/protobuf/proto", true},
		{"kernel values importing protobuf is forbidden", mod + "/internal/kernel/values", "google.golang.org/protobuf/proto", true},
		{"an unrelated tool importing protobuf is forbidden", mod + "/tools/policy", "google.golang.org/protobuf/proto", true},
		{"a domain package importing grpc is forbidden", mod + "/internal/domains/people", "google.golang.org/grpc", true},
		{"a domain package importing protobuf is forbidden", mod + "/internal/domains/people", "google.golang.org/protobuf/proto", true},
		{"a domain package importing connect is forbidden", mod + "/internal/domains/people", "connectrpc.com/connect", true},
		{"an unrelated module is not this check's concern", mod + "/internal/domains/people", "github.com/google/uuid", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := libfirewall.CheckImportAgainstRoots(mod, tc.importer, tc.imported, cfg.ProtobufGRPC.Modules, cfg.ProtobufGRPC.AllowedImportRoots)
			if tc.wantV && v == nil {
				t.Errorf("CheckImportAgainstRoots(%q, %q) = nil, want a violation", tc.importer, tc.imported)
			}
			if !tc.wantV && v != nil {
				t.Errorf("CheckImportAgainstRoots(%q, %q) = %+v, want no violation", tc.importer, tc.imported, v)
			}
		})
	}
}

// TestTodo_LIB_003_Golden pins the exact allowed-roots list this
// qualification enforces, so a silent edit to library-firewall.yaml's
// protobuf_grpc section is a visible diff.
func TestTodo_LIB_003_Golden(t *testing.T) {
	cfg := loadFirewallConfig(t)
	want := []string{"gen", "schema/proto/gen", "internal/transport", "internal/data/projection/wire", "internal/data/provenance", "internal/data/roleaccessstore", "internal/data/siemstore", "internal/application", "internal/intent/app", "internal/intent/protomap", "internal/engines/wire", "internal/timeclockapp", "tools/gen", "tools/uxqual/journeyclient", "tools/uxqual/cmd/journeywasm", "tools/uxqual/productclient", "tools/clockpartner", "tools/timeclockkit", "tools/uxqual/projectclient", "tools/quality/bufprotovalidatekit", "cmd"}
	got := cfg.ProtobufGRPC.AllowedImportRoots
	if len(got) != len(want) {
		t.Fatalf("protobuf_grpc.allowed_import_roots = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("protobuf_grpc.allowed_import_roots = %v, want %v", got, want)
		}
	}
}

// TestTodo_LIB_003_ModuleSpecificRootsRemainEnforced ensures the shared
// LIB-003 union does not widen the independent per-module dependency rows.
func TestTodo_LIB_003_ModuleSpecificRootsRemainEnforced(t *testing.T) {
	manifest := loadRoleManifest(t)
	cmd := manifest.Module + "/cmd/hcmnext"
	for _, tc := range []struct {
		name, imported string
		allowed        bool
	}{
		{"grpc composition", "google.golang.org/grpc", true},
		{"connect composition", "connectrpc.com/connect", true},
		{"protobuf composition remains outside its exact row", "google.golang.org/protobuf/proto", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := libfirewall.CheckImport(manifest, cmd, tc.imported)
			if (got == nil) != tc.allowed {
				t.Errorf("CheckImport(%q, %q) = %+v, allowed = %t", cmd, tc.imported, got, tc.allowed)
			}
		})
	}
}

// TestTodo_LIB_003_Integration runs the qualification against the real
// import graph and reports every real violation found in HEAD.
func TestTodo_LIB_003_Integration(t *testing.T) {
	cfg := loadFirewallConfig(t)
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var total int
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if v := libfirewall.CheckImportAgainstRoots(cfg.Module, pkg.ImportPath, imp, cfg.ProtobufGRPC.Modules, cfg.ProtobufGRPC.AllowedImportRoots); v != nil {
				total++
				t.Errorf("LIB-003 grpc/protobuf qualification violation: %s imports %s (allowed roots: %v)", v.Importer, v.ImportedPath, v.AllowedRoots)
			}
		}
	}
	t.Logf("scanned %d packages, %d LIB-003 violations", len(pkgs), total)
}

// TestTodo_LIB_003_Conformance checks a small canonical vector set covering
// every module this qualification governs (protobuf, grpc, connect,
// genproto status codes), not just the one used in the primary test table.
func TestTodo_LIB_003_Conformance(t *testing.T) {
	cfg := loadFirewallConfig(t)
	mod := cfg.Module

	for _, module := range cfg.ProtobufGRPC.Modules {
		t.Run(module, func(t *testing.T) {
			bad := mod + "/internal/domains/people"
			if v := libfirewall.CheckImportAgainstRoots(mod, bad, module, cfg.ProtobufGRPC.Modules, cfg.ProtobufGRPC.AllowedImportRoots); v == nil {
				t.Errorf("a domain package importing %s was not flagged", module)
			}
			good := mod + "/gen/go/hcm/v1"
			if v := libfirewall.CheckImportAgainstRoots(mod, good, module, cfg.ProtobufGRPC.Modules, cfg.ProtobufGRPC.AllowedImportRoots); v != nil {
				t.Errorf("gen importing %s was flagged: %+v", module, v)
			}
		})
	}
}
