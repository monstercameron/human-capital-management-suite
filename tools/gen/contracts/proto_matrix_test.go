package contracts

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
)

func TestTodo_PROTO_003_Conformance(t *testing.T) {
	fds := buildDescriptorSet(t, findRepoRoot(t), t.TempDir(), "workflow.binpb")
	assertServiceHasTotalDispositionComments(t, fds, "hcmnext/workflow/v1/workflow_service.proto", "WorkflowService", []string{"GetWorkflow", "ListNodeExecutions", "PauseWorkflow", "ResumeWorkflow", "CancelWorkflow", "RetryNode"})
	assertServiceHasTotalDispositionComments(t, fds, "hcmnext/humanwork/v1/humanwork_service.proto", "WorkService", []string{"ListWorkItems", "GetWorkItem", "ClaimWorkItem", "ReleaseWorkItem", "CompleteWorkItem", "DecideApproval", "GetThresholdTable"})
}

func TestTodo_PROTO_003_Fault(t *testing.T) {
	for _, wire := range [][]byte{{0x0a, 0x08, 'x'}, {0x80}} {
		if err := proto.Unmarshal(wire, &workflowv1.WorkflowInstance{}); err == nil {
			t.Errorf("malformed workflow wire %x accepted", wire)
		}
	}
}

func FuzzTodo_PROTO_003(f *testing.F) {
	f.Add("work-1", uint64(1))
	f.Add("", uint64(0))
	f.Fuzz(func(t *testing.T, id string, version uint64) {
		if len(id) > 4096 {
			t.Skip()
		}
		m := &workflowv1.WorkflowInstance{InstanceId: id, InstanceVersion: version}
		got := roundTrip(t, m).(*workflowv1.WorkflowInstance)
		if !proto.Equal(m, got) {
			t.Fatalf("workflow instance changed on wire round trip")
		}
	})
}

func TestTodo_PROTO_003_Golden(t *testing.T) {
	m := &humanworkv1.WorkItem{WorkItemId: "w-1", TenantId: "tenant-1", ItemVersion: 7}
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got humanworkv1.WorkItem
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(m, &got) {
		t.Fatalf("work item golden round trip changed: %v", &got)
	}
}

func TestTodo_PROTO_003_Integration(t *testing.T) {
	fds := buildDescriptorSet(t, findRepoRoot(t), t.TempDir(), "workflow.binpb")
	for _, pkg := range []string{"hcmnext.workflow.v1", "hcmnext.humanwork.v1"} {
		found := false
		for _, fd := range fds.GetFile() {
			if fd.GetPackage() == pkg {
				found = true
				if len(fd.GetService()) == 0 {
					t.Errorf("%s has no service", pkg)
				}
			}
		}
		if !found {
			t.Errorf("descriptor set omits %s", pkg)
		}
	}
}

func TestTodo_PROTO_003_Mutation(t *testing.T) {
	m := &workflowv1.WorkflowInstance{InstanceId: "instance-1"}
	wire, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	mutated := append([]byte(nil), wire...)
	mutated[len(mutated)-1] ^= 1
	var got workflowv1.WorkflowInstance
	if err := proto.Unmarshal(mutated, &got); err != nil {
		t.Fatal(err)
	}
	if proto.Equal(m, &got) {
		t.Fatal("workflow identifier wire mutation was not observable")
	}
}

func TestTodo_PROTO_004_Security(t *testing.T) {
	md := (&integrationv1.ExternalObservation{}).ProtoReflect().Descriptor()
	for _, name := range []string{"tenant_id", "raw_artifact_ref", "content_digest"} {
		if md.Fields().ByName(protoreflect.Name(name)) == nil {
			t.Fatalf("external observation omits tenant-bound protected evidence field %s", name)
		}
	}
	if md.Fields().ByName("raw_payload") != nil {
		t.Fatal("external observation exposes raw provider payload")
	}
}

func FuzzTodo_PROTO_004(f *testing.F) {
	f.Add("tenant-1", "sha256:abc", uint64(1))
	f.Add("", "", uint64(0))
	f.Fuzz(func(t *testing.T, tenant, digest string, seq uint64) {
		if len(tenant) > 4096 || len(digest) > 4096 {
			t.Skip()
		}
		m := &integrationv1.ExternalObservation{TenantId: tenant, ContentDigest: digest, PageSequence: seq}
		got := roundTrip(t, m).(*integrationv1.ExternalObservation)
		if !proto.Equal(m, got) {
			t.Fatal("external observation changed during round trip")
		}
	})
}

func TestTodo_PROTO_004_Golden(t *testing.T) {
	m := &dataopsv1.RepairPlan{TenantId: "tenant-1", Executable: false, NotExecutableReason: "p1a_repair_execution_not_authorized"}
	got := roundTrip(t, m).(*dataopsv1.RepairPlan)
	if !proto.Equal(m, got) || got.GetExecutable() {
		t.Fatalf("repair plan lost its non-executable safety state: %v", got)
	}
}

func TestTodo_PROTO_004_Integration(t *testing.T) {
	fds := buildDescriptorSet(t, findRepoRoot(t), t.TempDir(), "ops.binpb")
	for _, pkg := range []string{"hcmnext.dataops.v1", "hcmnext.integration.v1", "hcmnext.evidence.v1"} {
		found := false
		for _, fd := range fds.GetFile() {
			if fd.GetPackage() == pkg {
				found = true
				if len(fd.GetService()) == 0 {
					t.Errorf("%s has no service", pkg)
				}
			}
		}
		if !found {
			t.Errorf("descriptor set omits %s", pkg)
		}
	}
}

func TestTodo_PROTO_004_Mutation(t *testing.T) {
	m := &evidencev1.ZeroEffectReceipt{IntentType: "x", Counters: &evidencev1.EffectCounters{}}
	a, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	b, err := proto.Marshal(&evidencev1.ZeroEffectReceipt{IntentType: "x", Counters: &evidencev1.EffectCounters{DomainWrites: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("effect counter mutation produced identical wire bytes")
	}
}

func TestTodo_PROTO_005_Golden(t *testing.T) {
	m := &commonv1.ScopeContext{TenantId: "tenant-1", Purpose: "payroll"}
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got commonv1.ScopeContext
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(m, &got) {
		t.Fatalf("scope golden vector changed: %v", &got)
	}
}

func FuzzTodo_PROTO_005(f *testing.F) {
	f.Add("tenant-1", "org-1", "payroll")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, tenant, org, purpose string) {
		if len(tenant) > 4096 || len(org) > 4096 || len(purpose) > 4096 {
			t.Skip()
		}
		m := &commonv1.ScopeContext{TenantId: tenant, OrganizationScopeId: org, Purpose: purpose}
		got := roundTrip(t, m).(*commonv1.ScopeContext)
		if !proto.Equal(m, got) {
			t.Fatal("scope changed on wire round trip")
		}
	})
}

func TestTodo_PROTO_005_Integration(t *testing.T) {
	fds := buildDescriptorSet(t, findRepoRoot(t), t.TempDir(), "all.binpb")
	if len(fds.GetFile()) == 0 {
		t.Fatal("buf emitted empty descriptor set")
	}
	for _, fd := range fds.GetFile() {
		if len(fd.GetPackage()) >= 8 && fd.GetPackage()[:8] == "hcmnext." && len(fd.GetSourceCodeInfo().GetLocation()) == 0 {
			t.Errorf("%s lacks source info needed for contract tooling", fd.GetName())
		}
	}
}
