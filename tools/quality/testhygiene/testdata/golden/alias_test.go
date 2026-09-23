package golden

import "testing"

func TestAliasForwardsToRealTest(t *testing.T) { TestRealThing(t) }

func TestRealThing(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("arithmetic broke")
	}
}
