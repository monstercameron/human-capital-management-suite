package main

import "testing"

func TestTransientPopoverDismissalPolicy(t *testing.T) {
	tests := []struct {
		name           string
		eventType      string
		key            string
		relatedInside  bool
		focusInPopover bool
		want           bool
	}{
		{name: "pointer leaves disclosure", eventType: "mouseout", want: true},
		{name: "pointer crosses descendants", eventType: "mouseout", relatedInside: true},
		{name: "focused popover action remains operable", eventType: "mouseout", focusInPopover: true},
		{name: "focus leaves disclosure", eventType: "focusout", want: true},
		{name: "focus moves into popover action", eventType: "focusout", relatedInside: true},
		{name: "escape", eventType: "keydown", key: "Escape", want: true},
		{name: "other key", eventType: "keydown", key: "Enter"},
		{name: "unrelated event", eventType: "click"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldDismissTransientPopover(test.eventType, test.key, test.relatedInside, test.focusInPopover); got != test.want {
				t.Fatalf("shouldDismissTransientPopover() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTransientPopoverGraceStateMachine(t *testing.T) {
	tests := []struct {
		name           string
		eventType      string
		key            string
		relatedInside  bool
		focusInPopover bool
		want           transientPopoverAction
	}{
		{name: "gap crossing schedules instead of closing", eventType: "mouseout", want: transientPopoverScheduleClose},
		{name: "panel entry cancels pending close", eventType: "mouseover", want: transientPopoverCancelPendingClose},
		{name: "blur schedules instead of closing", eventType: "focusout", want: transientPopoverScheduleClose},
		{name: "panel focus cancels pending close", eventType: "focusin", want: transientPopoverCancelPendingClose},
		{name: "travel inside does nothing", eventType: "mouseout", relatedInside: true, want: transientPopoverKeep},
		{name: "escape closes immediately", eventType: "keydown", key: "Escape", want: transientPopoverCloseNow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := transientPopoverEventAction(test.eventType, test.key, test.relatedInside, test.focusInPopover); got != test.want {
				t.Fatalf("transientPopoverEventAction() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTransientPopoverGraceBounds(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want int
	}{
		{raw: "180", want: 180},
		{raw: "80", want: 80},
		{raw: "500", want: 500},
		{raw: "79", want: defaultTransientPopoverGraceMilliseconds},
		{raw: "501", want: defaultTransientPopoverGraceMilliseconds},
		{raw: "invalid", want: defaultTransientPopoverGraceMilliseconds},
	} {
		if got := normalizedTransientPopoverGraceMilliseconds(test.raw); got != test.want {
			t.Errorf("normalizedTransientPopoverGraceMilliseconds(%q) = %d, want %d", test.raw, got, test.want)
		}
	}
}

func TestTodo_UXAUDIT_006_LocaleMenuClosesAfterNavigation(t *testing.T) {
	for _, tc := range []struct {
		eventType string
		link      bool
		want      bool
	}{
		{"click", true, true},
		{"click", false, false},
		{"pointerdown", true, false},
		{"focusin", true, false},
	} {
		if got := transientPopoverLinkActivated(tc.eventType, tc.link); got != tc.want {
			t.Errorf("%s/link=%t closes=%t, want %t", tc.eventType, tc.link, got, tc.want)
		}
	}
}
