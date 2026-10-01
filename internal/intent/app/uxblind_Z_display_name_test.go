package app

import "testing"

func TestTodo_UXBLIND_082_ServerProjection(t *testing.T) {
	for _, tc := range []struct {
		preferred, legal, want string
	}{
		{preferred: "Linh", legal: "Linh Nguyen", want: "Linh Nguyen"},
		{preferred: "Linh Nguyen", legal: "Linh Nguyen", want: "Linh Nguyen"},
		{preferred: "", legal: "Linh Nguyen", want: "Linh Nguyen"},
	} {
		if got := journeyDisplayName(tc.preferred, tc.legal); got != tc.want {
			t.Errorf("journeyDisplayName(%q, %q) = %q, want %q", tc.preferred, tc.legal, got, tc.want)
		}
	}
}
