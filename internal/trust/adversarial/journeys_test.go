package adversarial

import (
	"context"
	"strings"
	"testing"
)

func TestAdversarialJourneysDenyWithoutExistenceMetadataOrTelemetryLeakage(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	journeys := PresetJourneys()
	results := engine.RunAll(context.Background(), journeys)
	if err := VerifyNoLeakage(results); err != nil {
		t.Fatalf("VerifyNoLeakage %v", err)
	}
	for _, r := range results {
		if r.EvidenceID == "" {
			t.Fatalf("journey %s no evidence", r.JourneyID)
		}
		if !r.EvidenceRedacted {
			t.Fatalf("journey %s evidence not redacted", r.JourneyID)
		}
		if r.UnauthorizedEffects != 0 {
			t.Fatalf("journey %s effects %d", r.JourneyID, r.UnauthorizedEffects)
		}
		if r.ExistenceLeak || r.MetadataLeak || r.TelemetryLeak {
			t.Fatalf("journey %s leaked %+v", r.JourneyID, r)
		}
	}
	t.Run("alternate_channel_parity", func(t *testing.T) {
		grpc := Journey{ID: "parity-1", Kind: KindAuthz, Channel: ChannelGRPC, Principal: "user-a", Tenant: "tenant-a", Target: "worker:secret", Action: "read"}
		http := Journey{ID: "parity-1", Kind: KindAuthz, Channel: ChannelHTTP, Principal: "user-a", Tenant: "tenant-a", Target: "worker:secret", Action: "read"}
		rg := engine.Run(context.Background(), grpc)
		rh := engine.Run(context.Background(), http)
		if rg.Denied != rh.Denied || rg.NonDisclosing != rh.NonDisclosing {
			t.Fatalf("parity mismatch grpc %+v http %+v", rg, rh)
		}
	})
	t.Run("delegated_session_denied", func(t *testing.T) {
		j := Journey{ID: "delegated", Kind: KindAuthz, Channel: ChannelDelegated, Principal: "delegated-evil", Tenant: "tenant-a", Target: "worker:x", Action: "read"}
		r := engine.Run(context.Background(), j)
		if !r.Denied || !r.NonDisclosing {
			t.Fatalf("delegated not denied %+v", r)
		}
	})
	t.Run("bulk_action_denied", func(t *testing.T) {
		j := Journey{ID: "bulk", Kind: KindAbuse, Channel: ChannelBulk, Principal: "user-a", Tenant: "tenant-a", Target: "bulk", Action: "bulk_action"}
		r := engine.Run(context.Background(), j)
		if !r.Denied {
			t.Fatalf("bulk not denied %+v", r)
		}
	})
	t.Run("retry_idempotent_denied", func(t *testing.T) {
		j := Journey{ID: "retry", Kind: KindAbuse, Channel: ChannelRetry, Principal: "user-a", Tenant: "tenant-a", Target: "intent-1", Action: "retry"}
		r := engine.Run(context.Background(), j)
		if !r.Denied {
			t.Fatalf("retry not denied %+v", r)
		}
	})
	t.Run("export_denied", func(t *testing.T) {
		j := Journey{ID: "export", Kind: KindPrivacy, Channel: ChannelExport, Principal: "user-a", Tenant: "tenant-a", Target: "export", Action: "export"}
		r := engine.Run(context.Background(), j)
		if !r.Denied {
			t.Fatalf("export not denied %+v", r)
		}
	})
	t.Run("support_tool_denied", func(t *testing.T) {
		j := Journey{ID: "support", Kind: KindAuthz, Channel: ChannelSupport, Principal: "support-agent", Tenant: "tenant-a", Target: "worker:secret", Action: "support_read"}
		r := engine.Run(context.Background(), j)
		if !r.Denied {
			t.Fatalf("support not denied %+v", r)
		}
	})
	t.Run("log_trace_query_denied_without_metadata_leak", func(t *testing.T) {
		for _, ch := range []string{ChannelLogQuery, ChannelTraceQuery} {
			j := Journey{ID: "query-" + ch, Kind: KindPrivacy, Channel: ch, Principal: "user-a", Tenant: "tenant-a", Target: "logs", Action: "query"}
			r := engine.Run(context.Background(), j)
			if !r.Denied || r.MetadataLeak || r.TelemetryLeak {
				t.Fatalf("query %s leaked %+v", ch, r)
			}
		}
	})
}

func TestTodo_THREAT_002(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	journeys := PresetJourneys()
	results := engine.RunAll(context.Background(), journeys)
	if len(results) != len(journeys) {
		t.Fatalf("evaluated %d journeys, want %d", len(results), len(journeys))
	}
	if err := VerifyNoLeakage(results); err != nil {
		t.Fatalf("preset attack corpus leaked sensitive data: %v", err)
	}
	for i, result := range results {
		if !result.Denied || !result.NonDisclosing || result.UnauthorizedEffects != 0 {
			t.Fatalf("preset journey %d bypassed deny boundary: %+v", i, result)
		}
	}
}

func TestTodo_THREAT_002_Property(t *testing.T) {
	if !IsNonDisclosingDeny(&DenyError{Code: CodeThreatDenied, Message: "denied"}) {
		t.Fatal("non-disclosing failed")
	}
	if IsNonDisclosingDeny(&DenyError{Code: CodeThreatDenied, Message: "worker: exists"}) {
		t.Fatal("leaking message considered non-disclosing")
	}
	if IsNonDisclosingDeny(nil) {
		t.Fatal("nil considered non-disclosing")
	}
}

func TestTodo_THREAT_002_Golden(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	j := Journey{ID: "golden-1", Kind: KindPrivacy, Channel: ChannelGRPC, Principal: "user-a", Tenant: "tenant-a", Target: "worker:secret", Action: "read_sensitive"}
	r1 := engine.Run(context.Background(), j)
	r2 := engine.Run(context.Background(), j)
	if r1.EvidenceID != r2.EvidenceID {
		t.Fatalf("evidence not deterministic %s vs %s", r1.EvidenceID, r2.EvidenceID)
	}
	if r1.Denied != r2.Denied || r1.NonDisclosing != r2.NonDisclosing {
		t.Fatal("golden not stable")
	}
}

func FuzzTodo_THREAT_002(f *testing.F) {
	f.Add("grpc", "tenant-a", "worker:secret")
	f.Add("http", "tenant-b", "worker:x")
	f.Fuzz(func(t *testing.T, channel, tenant, target string) {
		engine := NewEngine(DefaultDenyHandler)
		j := Journey{ID: "fuzz", Kind: KindAuthz, Channel: channel, Principal: "user-a", Tenant: tenant, Target: target, Action: "read"}
		r := engine.Run(context.Background(), j)
		if r.ExistenceLeak || r.MetadataLeak || r.TelemetryLeak {
			t.Fatalf("fuzz leaked %+v", r)
		}
		if r.EvidenceID == "" {
			t.Fatal("no evidence")
		}
		if strings.Contains(r.EvidenceID, target) {
			t.Fatal("evidence contains payload")
		}
	})
}

func TestTodo_THREAT_002_Race(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	journeys := PresetJourneys()
	done := make(chan []JourneyResult, 2)
	go func() { done <- engine.RunAll(context.Background(), journeys) }()
	go func() { done <- engine.RunAll(context.Background(), journeys) }()
	r1 := <-done
	r2 := <-done
	if len(r1) != len(r2) {
		t.Fatalf("race len %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].Denied != r2[i].Denied || r1[i].EvidenceID != r2[i].EvidenceID {
			t.Fatalf("race mismatch %d", i)
		}
	}
}

func TestTodo_THREAT_002_Integration(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	journeys := PresetJourneys()
	results := engine.RunAll(context.Background(), journeys)
	if err := VerifyNoLeakage(results); err != nil {
		t.Fatalf("integration leakage %v", err)
	}
	grpcJ := Journey{ID: "int-parity", Kind: KindAuthz, Channel: ChannelGRPC, Principal: "user-a", Tenant: "tenant-a", Target: "worker:secret", Action: "read"}
	httpJ := Journey{ID: "int-parity", Kind: KindAuthz, Channel: ChannelHTTP, Principal: "user-a", Tenant: "tenant-a", Target: "worker:secret", Action: "read"}
	rg := engine.Run(context.Background(), grpcJ)
	rh := engine.Run(context.Background(), httpJ)
	if rg.Denied != rh.Denied {
		t.Fatal("integration channel parity failed")
	}
}

func TestTodo_THREAT_002_Fault(t *testing.T) {
	engine := NewEngine(func(ctx context.Context, j Journey) error {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	})
	j := Journey{ID: "fault", Kind: KindAbuse, Channel: ChannelRetry, Principal: "user-a", Tenant: "tenant-a", Target: "x", Action: "retry"}
	r := engine.Run(context.Background(), j)
	if !r.Denied || r.UnauthorizedEffects != 0 {
		t.Fatalf("fault not denied %+v", r)
	}
}

func TestTodo_THREAT_002_Security(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	t.Run("disclosing_message_is_leak", func(t *testing.T) {
		leakingEngine := NewEngine(func(ctx context.Context, j Journey) error {
			return &DenyError{Code: CodeThreatDenied, Message: "worker:secret exists"}
		})
		r := leakingEngine.Run(context.Background(), Journey{ID: "leak", Kind: KindPrivacy, Channel: ChannelGRPC, Principal: "u", Tenant: "t", Target: "w", Action: "read"})
		if !r.ExistenceLeak {
			t.Fatal("did not detect existence leak")
		}
		if r.NonDisclosing {
			t.Fatal("leaking error considered non-disclosing")
		}
	})
	t.Run("all_journeys_denied_without_payload_in_evidence", func(t *testing.T) {
		for _, j := range PresetJourneys() {
			r := engine.Run(context.Background(), j)
			if strings.Contains(r.EvidenceID, j.Target) {
				t.Fatalf("evidence leaked target for %s", j.ID)
			}
		}
	})
}

func TestTodo_THREAT_002_Conformance(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	journeys := PresetJourneys()
	for _, j := range journeys {
		r := engine.Run(context.Background(), j)
		if !r.Denied {
			t.Fatalf("journey %s not denied", j.ID)
		}
		if r.Channel != j.Channel {
			t.Fatalf("channel mismatch %s vs %s", r.Channel, j.Channel)
		}
	}
}

func TestTodo_THREAT_002_Browser(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	channels := []string{ChannelGRPC, ChannelHTTP, ChannelDeepLink}
	for _, ch := range channels {
		j := Journey{ID: "browser-" + ch, Kind: KindPrivacy, Channel: ch, Principal: "user-a", Tenant: "tenant-a", Target: "worker:secret", Action: "read_sensitive"}
		r := engine.Run(context.Background(), j)
		if !r.Denied || !r.NonDisclosing {
			t.Fatalf("browser channel %s not denied %+v", ch, r)
		}
	}
}

func TestTodo_THREAT_002_Recovery(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	j := Journey{ID: "recovery", Kind: KindAuthz, Channel: ChannelRetry, Principal: "user-a", Tenant: "tenant-a", Target: "worker:secret", Action: "read"}
	r1 := engine.Run(context.Background(), j)
	r2 := engine.Run(context.Background(), j)
	if r1.Denied != r2.Denied || r1.EvidenceID != r2.EvidenceID {
		t.Fatal("recovery not deterministic")
	}
	if err := VerifyNoLeakage([]JourneyResult{r1, r2}); err != nil {
		t.Fatalf("recovery leakage %v", err)
	}
}

func TestTodo_THREAT_002_Mutation(t *testing.T) {
	engine := NewEngine(DefaultDenyHandler)
	j := Journey{ID: "mut", Kind: KindPrivacy, Channel: ChannelGRPC, Principal: "user-a", Tenant: "tenant-a", Target: "worker:secret", Action: "read_sensitive"}
	r := engine.Run(context.Background(), j)
	if !r.Denied {
		t.Fatal("mutant: should be denied")
	}
	allowEngine := NewEngine(func(ctx context.Context, j Journey) error { return nil })
	r2 := allowEngine.Run(context.Background(), j)
	if r2.Denied {
		t.Fatal("mutant: allow handler incorrectly denied")
	}
	if err := VerifyNoLeakage([]JourneyResult{r2}); err == nil {
		t.Fatal("mutant: allow should fail verification")
	}
}
