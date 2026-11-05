package connectivity

import (
	"strings"
	"testing"
)

// TestTodo_CHATCALL_002 pins the media-topology comparison: the four
// topologies with their limits and costs, the candidate implementations with
// licences and verdicts, a recommendation per call kind of CHATCALL-001, and
// the measurement contract the prototype run must satisfy.
func TestTodo_CHATCALL_002(t *testing.T) {
	topologies := MediaTopologies()
	if len(topologies) != 4 {
		t.Fatalf("topologies = %d, want 4", len(topologies))
	}
	wantIDs := []string{"P2P", "MESH", "SFU", "MCU"}
	for i, topo := range topologies {
		if topo.ID != wantIDs[i] {
			t.Fatalf("topology %d id = %q, want %q", i, topo.ID, wantIDs[i])
		}
		for _, field := range []struct {
			name, value string
		}{
			{"Name", topo.Name},
			{"ParticipantLimit", topo.ParticipantLimit},
			{"UpstreamPerPerson", topo.UpstreamPerPerson},
			{"DownstreamPerPerson", topo.DownstreamPerPerson},
			{"ServerCPUPerCall", topo.ServerCPUPerCall},
			{"ServerBandwidthPerCall", topo.ServerBandwidthPerCall},
			{"PoorNetwork", topo.PoorNetwork},
			{"Encryption", topo.Encryption},
			{"Captions", topo.Captions},
			{"Recording", topo.Recording},
		} {
			if strings.TrimSpace(field.value) == "" {
				t.Fatalf("topology %s leaves %s unanswered", topo.ID, field.name)
			}
		}
	}
	record := RenderMediaTopologies()
	for _, phrase := range []string{
		"DTLS-SRTP", "simulcast", "audio-only fallback",
		"Pion", "LiveKit", "coturn",
		"One-to-one voice", "One-to-one video", "Drop-in call in a channel", "Screen sharing",
	} {
		if !strings.Contains(record, phrase) {
			t.Fatalf("record does not pin %q", phrase)
		}
	}

	candidates := TopologyCandidates()
	if len(candidates) != 6 {
		t.Fatalf("candidates = %d, want 6", len(candidates))
	}
	licences := map[string]string{}
	verdicts := map[string]string{}
	for _, c := range candidates {
		licences[c.ID] = c.Licence
		verdicts[c.ID] = c.Verdict
		for _, field := range []struct {
			name, value string
		}{
			{"Name", c.Name}, {"Language", c.Language}, {"Licence", c.Licence},
			{"Maturity", c.Maturity}, {"Maintenance", c.Maintenance},
			{"MonolithFit", c.MonolithFit}, {"Verdict", c.Verdict},
		} {
			if strings.TrimSpace(field.value) == "" {
				t.Fatalf("candidate %s leaves %s unanswered", c.ID, field.name)
			}
		}
		switch c.Verdict {
		case "ADOPT", "REJECT":
		default:
			t.Fatalf("candidate %s has verdict %q, want ADOPT or REJECT", c.ID, c.Verdict)
		}
	}
	for id, want := range map[string]string{
		"PION": "MIT", "LIVEKIT": "Apache-2.0", "MEDIASOUP": "ISC",
		"JANUS": "GPLv3", "JITSI": "Apache-2.0", "COTURN": "BSD",
	} {
		if licences[id] != want {
			t.Fatalf("candidate %s licence = %q, want %q", id, licences[id], want)
		}
	}
	for id, want := range map[string]string{
		"PION": "ADOPT", "LIVEKIT": "ADOPT", "COTURN": "ADOPT",
		"MEDIASOUP": "REJECT", "JANUS": "REJECT", "JITSI": "REJECT",
	} {
		if verdicts[id] != want {
			t.Fatalf("candidate %s verdict = %q, want %q", id, verdicts[id], want)
		}
	}
	if !strings.Contains(record, "copyleft") {
		t.Fatal("the record does not say why the GPLv3 candidate is rejected")
	}

	recs := TopologyRecommendations()
	if len(recs) != 4 {
		t.Fatalf("recommendations = %d, want one per call kind", len(recs))
	}
	for _, r := range recs {
		if strings.TrimSpace(r.CallKind) == "" || strings.TrimSpace(r.Recommended) == "" || strings.TrimSpace(r.Crossover) == "" {
			t.Fatalf("recommendation leaves kind, choice or crossover unanswered: %+v", r)
		}
	}

	topologies[0].Name = "changed"
	if MediaTopologies()[0].Name == "changed" {
		t.Fatal("the record is mutable by a caller")
	}
}

// TestTodo_CHATCALL_002_Performance pins the executed review-cell prototype
// run: path, impairment, metrics, budgets, and the measured result plus run
// provenance per metric. Status is RUN only with all three results recorded.
func TestTodo_CHATCALL_002_Performance(t *testing.T) {
	contract := TopologyMeasurementContract()
	if strings.TrimSpace(contract.Path) == "" {
		t.Fatal("the contract names no prototype path")
	}
	if strings.TrimSpace(contract.LossProfile) == "" || strings.TrimSpace(contract.LatencyProfile) == "" {
		t.Fatal("the contract names no loss/latency injection profile")
	}
	wantMetrics := []string{"setup time", "audio quality score", "recovery time"}
	if len(contract.Metrics) != len(wantMetrics) {
		t.Fatalf("metrics = %v", contract.Metrics)
	}
	for i, want := range wantMetrics {
		if contract.Metrics[i] != want {
			t.Fatalf("metric %d = %q, want %q", i, contract.Metrics[i], want)
		}
		if strings.TrimSpace(contract.Budgets[contract.Metrics[i]]) == "" {
			t.Fatalf("metric %q has no budget", want)
		}
	}
	if contract.Status != "RUN" {
		t.Fatalf("status = %q, want RUN after the review-cell run reports", contract.Status)
	}
	if strings.TrimSpace(contract.Run) == "" {
		t.Fatal("the run records no provenance")
	}
	for _, want := range wantMetrics {
		if strings.TrimSpace(contract.Results[want]) == "" {
			t.Fatalf("metric %q has no measured result", want)
		}
	}
	rendered := RenderMediaTopologies()
	if strings.Contains(rendered, "NOT_RUN") {
		t.Fatal("the rendered record still claims the measurement is pending")
	}
	if !strings.Contains(rendered, "status: RUN") {
		t.Fatal("the rendered record hides the completed measurement")
	}
	for _, want := range wantMetrics {
		if !strings.Contains(rendered, contract.Results[want]) {
			t.Fatalf("the rendered record hides the %s result", want)
		}
	}
}

func TestTodo_CHATCALL_002_Golden(t *testing.T) {
	const digest = "05a395c8d116275987e587941f024470453a99954e40c93f33839da3fd74d2a6"
	if got := strings.Count(RenderMediaTopologies(), "\n"); got < 20 {
		t.Fatalf("record has %d lines", got)
	}
	if MediaTopologiesDigest() != digest {
		t.Fatalf("the comparison changed; digest is now %s", MediaTopologiesDigest())
	}
}
