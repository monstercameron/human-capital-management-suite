package chatui

import "testing"

// A cleared quiet-hours time field carries no time. It must not be read as
// midnight (00:00), which is what parseClock("") returns; the caller keeps the
// saved boundary instead, so the field, the summary and the stored value agree.
func TestQuietClockInputClearedFieldKeepsSavedBoundary(t *testing.T) {
	for _, value := range []string{"", "   "} {
		if minute, ok := quietClockInput(value); ok || minute != 0 {
			t.Fatalf("quietClockInput(%q) = (%d, %v), want (0, false) so the saved boundary is kept", value, minute, ok)
		}
	}
}

func TestQuietClockInputReadsAValidTime(t *testing.T) {
	if minute, ok := quietClockInput("07:30"); !ok || minute != 7*60+30 {
		t.Fatalf("quietClockInput(\"07:30\") = (%d, %v), want (450, true)", minute, ok)
	}
	if minute, ok := quietClockInput("23:45"); !ok || minute != 23*60+45 {
		t.Fatalf("quietClockInput(\"23:45\") = (%d, %v), want (1425, true)", minute, ok)
	}
}
