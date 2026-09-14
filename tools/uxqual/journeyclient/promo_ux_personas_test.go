package journeyclient

import "testing"

// promoUXPersonaAffordances is the presentation contract shared by the
// served-cell regression and the browser client. It intentionally describes
// discoverability only: the real server repeats authorization for every RPC.
type promoUXPersonaAffordances struct {
	name   string
	create bool
	update bool
}

func TestPromoUXPersonaAffordanceContract(t *testing.T) {
	page := "journey.promotion"
	cases := []promoUXPersonaAffordances{
		{name: "proposer", create: true, update: false},
		{name: "finance", create: false, update: true},
		{name: "manager", create: false, update: true},
		{name: "employee", create: false, update: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := Config{PagePermissions: []PagePermission{{PageID: page, Create: tc.create, Update: tc.update}}}
			if got := config.CanPageAction(page, "create"); got != tc.create {
				t.Fatalf("create affordance = %v, want %v", got, tc.create)
			}
			if got := config.CanPageAction(page, "update"); got != tc.update {
				t.Fatalf("update affordance = %v, want %v", got, tc.update)
			}
		})
	}
}
