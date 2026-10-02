//go:build !(js && wasm)

package pagedef

import (
	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
)

// registryServiceRPCs and adminServiceRPCs read their lists from the generated
// ServiceDesc values, so on the server and in every native test a new RPC is
// known the moment it is generated. The browser bundle uses the literal lists
// in rpcregistry_literals.go instead (see rpcregistry_wasm.go), and
// TestRegistryAndAdminLiteralsMatchServiceDescs holds the two together.

func registryServiceRPCs() []string {
	desc := registryv1.RegistryService_ServiceDesc
	methods := make([]string, 0, len(desc.Methods))
	for _, m := range desc.Methods {
		methods = append(methods, m.MethodName)
	}
	streams := make([]string, 0, len(desc.Streams))
	for _, s := range desc.Streams {
		streams = append(streams, s.StreamName)
	}
	return serviceMethods(desc.ServiceName, methods, streams)
}

func adminServiceRPCs() []string {
	desc := adminv1.AdminService_ServiceDesc
	methods := make([]string, 0, len(desc.Methods))
	for _, m := range desc.Methods {
		methods = append(methods, m.MethodName)
	}
	streams := make([]string, 0, len(desc.Streams))
	for _, s := range desc.Streams {
		streams = append(streams, s.StreamName)
	}
	return serviceMethods(desc.ServiceName, methods, streams)
}
