package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

func TestChattoneY2TmpOwn(t *testing.T) {
	rig, setting := chattoneY2Rig(t, chattoneRewordReplayFixture(t))
	rig.enable(chatlang.Workspace{})
	for _, lang := range []string{"en", "de"} {
		rig.reads("alice", lang)
		calm := rig.send("calm-"+lang, w10Calm)
		heated := rig.send("heated-"+lang, w10Heated)
		for _, mode := range []string{chatstore.RewordModeOff, chatstore.RewordModeOffered, chatstore.RewordModeOn} {
			for _, view := range []bool{true, false} {
				setting(mode, view)
				for _, who := range []string{"alice", "bruno"} {
					code, out := chattoneY2Reader(t, rig, who, calm.ID, heated.ID)
					t.Logf("lang=%s mode=%s view=%v reader=%s -> %d (%d answers)", lang, mode, view, who, code, len(out))
					if code != 200 {
						t.Errorf("FAIL")
					}
				}
			}
		}
	}
}
