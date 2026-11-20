package productui

// #3764: on a phone or any touch screen the account menu's links (Myself,
// Settings, Sign out) are 44 by 44 CSS pixels, like the other controls of the
// bar they open from. The class is the shell's, so the rule is part of the
// platform sheet and not of the Chat-only sheet.
func issue3764AccountLinkStylesheet() string {
	return `@media(max-width:760px),(pointer:coarse){.app-shell .account-menu-link{display:flex;align-items:center;min-block-size:44px;min-inline-size:44px}}`
}
