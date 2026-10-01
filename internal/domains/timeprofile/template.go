package timeprofile

// TemplateFor selects the one workflow template a valid profile resolves to
// (WTIME-002 GREEN). Contractor and agency-temp categories always resolve to
// their own template regardless of capture mode, because their time-to-
// invoice and time-to-VMS templates carry milestone and approval steps a
// capture mode does not describe. Every other category resolves by capture
// mode; CaptureNone records no time and has no template.
//
// TemplateFor never returns more than one candidate: the category/capture
// mapping below is a function, not a search, so ambiguity cannot arise here.
// A publish-time ambiguity between competing tenant overlays of one template
// is WF-EXT-026's concern, not this package's.
func TemplateFor(p TimeProfile) (Template, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	switch p.Category {
	case CategoryContractor:
		return TemplateContractorTime, nil
	case CategoryAgencyTemp:
		return TemplateAgencyTime, nil
	}
	switch p.Capture {
	case CapturePunch:
		return TemplatePunchSession, nil
	case CaptureDuration:
		return TemplateDurationSheet, nil
	case CaptureException:
		return TemplateExceptionOnly, nil
	default:
		return "", ErrNoTimeTemplate
	}
}
