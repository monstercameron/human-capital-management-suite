// Package openapi is the INTAPI-008 generator: it walks the compiled
// Protobuf descriptors of every hcmnext package and renders
// schema/openapi/rpcs.openapi.yaml, an OpenAPI 3.1.0 document describing each
// declared service and RPC as the Connect/HTTP API.
//
// The Protobuf sources under schema/proto stay canonical. The document is a
// projection of them: one POST operation per RPC at its Connect path, request
// and response schemas that follow the Protobuf JSON mapping, the Connect error
// model, bearer security, and x-hcmnext-* extensions naming each operation's
// service, method kind, gRPC registration and HTTP exposure. Rendering is pure
// and deterministic; TestTodo_INTAPI_008_Golden fails whenever the checked-in
// document differs from a fresh generation.
//
// Regenerate with:
//
//	go run ./tools/gen/openapi/cmd/openapigen
package openapi

import (
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Blank imports link every hcmnext Protobuf package's descriptors into
	// protoregistry.GlobalFiles. TestTodo_INTAPI_008 proves the list is
	// complete by matching the registered files against schema/proto.
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/capabilities/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/position/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/reviewparticipants/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
)

// packagePrefix is the Protobuf package namespace this generator documents.
const packagePrefix = "hcmnext."

// hcmnextFiles returns every linked file descriptor in the hcmnext package
// namespace, sorted by path.
func hcmnextFiles(files *protoregistry.Files) []protoreflect.FileDescriptor {
	var out []protoreflect.FileDescriptor
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if strings.HasPrefix(string(fd.Package()), packagePrefix) {
			out = append(out, fd)
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path() < out[j].Path() })
	return out
}

// services returns every service declared in files, sorted by full name.
func services(files []protoreflect.FileDescriptor) []protoreflect.ServiceDescriptor {
	var out []protoreflect.ServiceDescriptor
	for _, fd := range files {
		for i := 0; i < fd.Services().Len(); i++ {
			out = append(out, fd.Services().Get(i))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FullName() < out[j].FullName() })
	return out
}

// methods returns the service's RPCs sorted by name.
func methods(sd protoreflect.ServiceDescriptor) []protoreflect.MethodDescriptor {
	out := make([]protoreflect.MethodDescriptor, 0, sd.Methods().Len())
	for i := 0; i < sd.Methods().Len(); i++ {
		out = append(out, sd.Methods().Get(i))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// procedurePath is the Connect (and gRPC) procedure path of md.
func procedurePath(md protoreflect.MethodDescriptor) string {
	return "/" + string(md.Parent().FullName()) + "/" + string(md.Name())
}
