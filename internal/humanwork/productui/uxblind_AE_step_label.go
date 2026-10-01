package productui

// ResolveNextStepLabel is the single product-copy resolver for a server
// next-step code. My Work and the Journeys projector both call it, so a
// localized page never falls back to the wire vocabulary or English-only
// workflow wording.
func ResolveNextStepLabel(locale LocaleContext, code string) string {
	if !knownWorkNextSteps[code] {
		return ""
	}
	return locale.Text("work.next_step." + code)
}
