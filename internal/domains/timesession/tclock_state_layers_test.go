package timesession

import (
	"errors"
	"testing"
	"time"
)

func testLayeredState(t *testing.T) LayeredState {
	t.Helper()
	s, err := NewLayeredState(TimeSessionBaseFacts{Tenant: "tenant-1", Subject: "session-1", BaseRevision: 7, Reason: "source"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func approvedLayer(s LayeredState, rev, parent uint64, patches ...StateLayerPatch) StateCorrectionLayer {
	l := StateCorrectionLayer{Tenant: s.Base.Tenant, Subject: s.Base.Subject, BaseRevision: s.Base.BaseRevision, Revision: rev, ParentRevision: parent, Approved: true, Provenance: LayerProvenance{Actor: "supervisor-1", Reason: "missed punch reviewed", WorkflowProof: WorkflowProof{InstanceID: "123e4567-e89b-12d3-a456-426614174000", PlanID: "plan-1", PlanDigest: "sha256:plan", TraceID: "trace-1", NodeID: "review", Attempt: 1, Version: 1}}, Patches: patches}
	l.Digest = l.DigestValue()
	return l
}

func TestTodo_TCLOCK_011_StateLayers(t *testing.T) {
	s := testLayeredState(t)
	at := time.Date(2026, 9, 28, 17, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	layer := approvedLayer(s, 1, 0, StateLayerPatch{Path: "clock_out_at", Value: TimeValue(at)}, StateLayerPatch{Path: "x.source", Value: StringValue("kiosk")})
	next, err := s.AppendLayer(layer)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := next.Resolve(1)
	if err != nil || !resolved.ClockOutAt.Equal(at.UTC()) || resolved.Extensions["x.source"].String != "kiosk" {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	v, p, err := next.Get("clock_out_at", 1)
	if err != nil || v.Kind != LayerValueTime || p.LayerRevision != 1 || p.Actor != "supervisor-1" || p.WorkflowProof.TraceID != "trace-1" {
		t.Fatalf("value=%+v provenance=%+v err=%v", v, p, err)
	}
}

func TestTodo_TCLOCK_011_StateLayers_Security(t *testing.T) {
	s := testLayeredState(t)
	good := approvedLayer(s, 1, 0, StateLayerPatch{Path: "reason", Value: StringValue("corrected")})
	for name, layer := range map[string]StateCorrectionLayer{
		"wrong tenant":            func() StateCorrectionLayer { x := good; x.Tenant = "other"; x.Digest = x.DigestValue(); return x }(),
		"wrong subject":           func() StateCorrectionLayer { x := good; x.Subject = "other"; x.Digest = x.DigestValue(); return x }(),
		"unapproved":              func() StateCorrectionLayer { x := good; x.Approved = false; x.Digest = x.DigestValue(); return x }(),
		"forbidden identity path": approvedLayer(s, 1, 0, StateLayerPatch{Path: "tenant", Value: StringValue("other")}),
		"duplicate path":          approvedLayer(s, 1, 0, StateLayerPatch{Path: "reason", Value: StringValue("a")}, StateLayerPatch{Path: "reason", Value: StringValue("b")}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.AppendLayer(layer)
			if err == nil {
				t.Fatal("AppendLayer unexpectedly succeeded")
			}
		})
	}
	if _, err := s.AppendLayer(good); err != nil {
		t.Fatal(err)
	}
	stale := approvedLayer(s, 2, 0, StateLayerPatch{Path: "reason", Value: StringValue("race")})
	if _, err := s.AppendLayer(stale); !errors.Is(err, ErrLayerCAS) {
		t.Fatalf("stale=%v", err)
	}
}

func TestTodo_TCLOCK_011_StateLayers_ReplayAndDeletion(t *testing.T) {
	s := testLayeredState(t)
	layer := approvedLayer(s, 1, 0, StateLayerPatch{Path: "reason", Value: StringValue("changed")})
	next, err := s.AppendLayer(layer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.AppendLayer(layer); !errors.Is(err, ErrLayerExactReplay) {
		t.Fatalf("replay=%v", err)
	}
	deletion := approvedLayer(next, 2, 1, StateLayerPatch{Path: "reason", Value: NullValue()})
	next, err = next.AppendLayer(deletion)
	if err != nil {
		t.Fatal(err)
	}
	v, p, err := next.Get("reason", 2)
	if err != nil || v.Kind != LayerValueNull || p.LayerRevision != 2 || p.Actor != "supervisor-1" {
		t.Fatalf("deleted=%+v provenance=%+v err=%v", v, p, err)
	}
	if _, _, err := next.Get("workflow_proof", 2); !errors.Is(err, ErrLayerPathForbidden) {
		t.Fatalf("metadata path=%v", err)
	}
}

func TestTodo_TCLOCK_011_StateLayers_DigestStable(t *testing.T) {
	s := testLayeredState(t)
	left := approvedLayer(s, 1, 0, StateLayerPatch{Path: "x.b", Value: StringValue("2")}, StateLayerPatch{Path: "x.a", Value: StringValue("1")})
	right := approvedLayer(s, 1, 0, StateLayerPatch{Path: "x.a", Value: StringValue("1")}, StateLayerPatch{Path: "x.b", Value: StringValue("2")})
	if left.Digest != right.Digest || left.Digest[:7] != "sha256:" {
		t.Fatalf("digest left=%s right=%s", left.Digest, right.Digest)
	}
	left.Provenance.Reason = "different"
	if left.DigestValue() == left.Digest {
		t.Fatal("digest ignored reason")
	}
}
