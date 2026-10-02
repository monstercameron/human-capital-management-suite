//go:build js && wasm

package pagedef

// The browser bundle must not import the admin and registry generated
// packages: nothing in the client calls those services, and linking a
// generated package keeps all of its messages, descriptors and registration
// (about 520 KB of the module for these two). Their RPC lists are therefore
// the literal lists in rpcregistry_literals.go, which
// TestRegistryAndAdminLiteralsMatchServiceDescs holds equal to the generated
// ServiceDesc values.

func registryServiceRPCs() []string {
	return serviceMethods(RegistryServiceName, registryMethodNames, nil)
}

func adminServiceRPCs() []string {
	return serviceMethods(AdminServiceName, adminMethodNames, nil)
}
