package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// TestTodo_CHATLANG_006_Gate: a pair the quality gate withholds is never sent to
// the engine, the reader keeps the original, and a pair it offers (or has not
// judged) is translated as before.
func TestTodo_CHATLANG_006_Gate(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	rig.reads("carla", "fr")
	rig.governance.SetGate([]chatlang.GateResult{
		{Pair: chatlang.GatePair{Source: "en", Target: "de"}, Decision: chatlang.GateWithhold},
		{Pair: chatlang.GatePair{Source: "en", Target: "fr"}, Decision: chatlang.GateOfferMarked},
	})
	if rig.governance.PairDecision("en-US", "de") != chatlang.GateWithhold || rig.governance.PairDecision("de", "fr") != chatlang.GateOffer || (*ChatlangGovernance)(nil).PairDecision("en", "de") != chatlang.GateOffer {
		t.Fatal("the decision lookup")
	}
	post := rig.send("gate", englishSentence)
	rig.drain()
	got, mark := rig.read("bruno", post)
	if got.Text != englishSentence || mark.State == "ready" || rig.jobState(post, "de") != "failed" {
		t.Fatalf("a withheld pair was translated: %+v %+v job=%s", got, mark, rig.jobState(post, "de"))
	}
	for _, call := range rig.engine.Calls() {
		if call.Target == "de" {
			t.Fatalf("a withheld pair reached the engine: %+v", call)
		}
	}
	if got, mark := rig.read("carla", post); mark.State != "ready" || got.Text != "[fr] "+englishSentence {
		t.Fatalf("an offered pair: %+v %+v", got, mark)
	}
	// Offering the pair again translates the next message.
	rig.governance.SetGate([]chatlang.GateResult{{Pair: chatlang.GatePair{Source: "en", Target: "de"}, Decision: chatlang.GateOffer}})
	next := rig.send("gate2", "Please review the quarterly report with the team today again.")
	rig.drain()
	if got, mark := rig.read("bruno", next); mark.State != "ready" || got.Text != "[de] Please review the quarterly report with the team today again." {
		t.Fatalf("an offered pair: %+v %+v", got, mark)
	}
}
