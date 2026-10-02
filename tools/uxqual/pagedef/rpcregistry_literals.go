package pagedef

// registryMethodNames and adminMethodNames are the RPCs of the registry and
// admin services, in the order their generated ServiceDesc lists them (unary
// methods, then streams; neither service has a stream). They are literals
// because the browser bundle must not import those two generated packages
// (see rpcregistry_wasm.go). A new RPC on either service fails
// TestRegistryAndAdminLiteralsMatchServiceDescs until it is added here.
var registryMethodNames = []string{
	"ListIntentDefinitions",
	"GetIntentDefinition",
	"ListCapabilities",
	"GetCapability",
}

var adminMethodNames = []string{
	"ListIntents",
	"GetReleaseManifest",
	"ListCapabilityProfiles",
	"ExplainTransaction",
	"GetWorkerState",
	"GetWorkflowInstance",
	"ListLedgerEvents",
	"GetChainVerification",
	"SimulateAuthorization",
	"ConfigInspect",
	"ConfigTest",
	"ConfigRedrive",
	"ConfigReconcile",
	"ConfigDiff",
	"ConfigSimulate",
	"ConfigPromote",
	"ConfigRollback",
}
