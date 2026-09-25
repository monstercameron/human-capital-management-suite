package manifest

import (
	"fmt"
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Blank imports are enough: every generated .pb.go file registers its
	// FileDescriptorProto into protoregistry.GlobalFiles from an init()
	// function. Importing the package for its side effect, rather than
	// invoking buf or parsing schema/proto/**, is what makes RPC discovery
	// read "the generated descriptors" (this package's mandate) instead of
	// re-deriving a second, potentially divergent, view of the schema.
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/capabilities/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workorder/v1"
)

// governedServices lists the fully qualified service names this manifest
// covers. It is the one place a third governed service would be added; every
// other function in this package discovers its methods from the descriptor,
// never from a hand-written method list.
var governedServices = []protoreflect.FullName{
	"hcmnext.intents.v1.IntentService",
	"hcmnext.registry.v1.RegistryService",
	"hcmnext.project.v1.ProjectService",
	"hcmnext.workorder.v1.WorkOrderService",
}

// RPCDescriptor is one method of one governed service, read directly from
// the compiled-in Protobuf descriptor.
type RPCDescriptor struct {
	ServiceFullName string
	MethodName      string
	RequestType     string
	ResponseType    string
	Input           protoreflect.MessageDescriptor
	Output          protoreflect.MessageDescriptor
}

// EndpointID returns the manifest identity for this method.
func (d RPCDescriptor) EndpointID() string {
	return d.ServiceFullName + "/" + d.MethodName
}

// GRPCProcedure returns the gRPC full method path for this method.
func (d RPCDescriptor) GRPCProcedure() string {
	return "/" + d.ServiceFullName + "/" + d.MethodName
}

// DiscoverRPCs returns every method of every service named in
// governedServices, sorted by EndpointID, by resolving each service against
// protoregistry.GlobalFiles. It fails closed: an unresolvable or
// non-service name is a programming error, never a silently empty manifest.
func DiscoverRPCs() ([]RPCDescriptor, error) {
	var out []RPCDescriptor
	for _, name := range governedServices {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
		if err != nil {
			return nil, fmt.Errorf("manifest: resolving service %s: %w", name, err)
		}
		svc, ok := descriptor.(protoreflect.ServiceDescriptor)
		if !ok {
			return nil, fmt.Errorf("manifest: %s is a %T, not a service", name, descriptor)
		}
		methods := svc.Methods()
		for i := 0; i < methods.Len(); i++ {
			m := methods.Get(i)
			out = append(out, RPCDescriptor{
				ServiceFullName: string(svc.FullName()),
				MethodName:      string(m.Name()),
				RequestType:     string(m.Input().FullName()),
				ResponseType:    string(m.Output().FullName()),
				Input:           m.Input(),
				Output:          m.Output(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndpointID() < out[j].EndpointID() })
	return out, nil
}
