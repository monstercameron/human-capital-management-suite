package productui

import "strings"

// PreferredFamilyName returns the viewer-safe display name used by people
// surfaces. Preferred names remain primary, while the family name makes two
// workers with the same preferred name distinguishable.
func PreferredFamilyName(person Person) string {
	preferred := strings.TrimSpace(person.PreferredName)
	legal := strings.TrimSpace(person.LegalName)
	name := strings.TrimSpace(person.Name)
	if preferred == "" {
		preferred = name
	}
	if preferred == "" {
		return legal
	}
	if legal == "" {
		return preferred
	}
	legalParts := strings.Fields(legal)
	preferredParts := strings.Fields(preferred)
	if len(legalParts) == 0 || len(preferredParts) == 0 {
		return preferred
	}
	family := legalParts[len(legalParts)-1]
	if strings.EqualFold(preferredParts[len(preferredParts)-1], family) {
		return preferred
	}
	return preferred + " " + family
}

// ObjectIdentity is the typed identity header of one object card: what the
// object is for a person (Primary, e.g. "Promotion for Amara"), the short
// handle a reader can quote or copy (Reference, e.g. "8CF888"), and the one
// status it is in (Status). UXLIVE-032 replaced a heading that glued the
// person's name to the reference ("Amara8CF888") and then repeated the
// status as a chip: the three facts are separate slots so a renderer places
// each in its own element instead of concatenating display strings, and the
// accessible name is composed from the same slots in one reviewed template.
type ObjectIdentity struct {
	// Primary is the human task label and the visible heading.
	Primary string
	// ReferenceLabel names what Reference is ("Request"); it is rendered as
	// quiet metadata beside the reference, never inside the heading.
	ReferenceLabel string
	// Reference is the short, copyable handle. It is an atomic token: a
	// renderer must keep it on one line and isolate its direction.
	Reference string
	// Status is the one status word shown for the object.
	Status string
	// StatusTone is the renderer's tone vocabulary for Status.
	StatusTone string
}

// ReferenceText is the reference with its label, e.g. "Request 8CF888", or
// empty when the object has no reference.
func (identity ObjectIdentity) ReferenceText(locale LocaleContext) string {
	reference := strings.TrimSpace(identity.Reference)
	if reference == "" {
		return ""
	}
	label := strings.TrimSpace(identity.ReferenceLabel)
	if label == "" {
		return reference
	}
	return locale.Text("identity.reference", map[string]string{"label": label, "reference": reference})
}

// AccessibleName identifies the object by its primary label, reference and
// status in one localized sentence, so a heading list read by a screen
// reader still distinguishes two requests for the same person even though
// the status is shown only once visually. Empty slots are left out rather
// than rendered as blank separators.
func (identity ObjectIdentity) AccessibleName(locale LocaleContext) string {
	primary := strings.TrimSpace(identity.Primary)
	reference := identity.ReferenceText(locale)
	status := strings.TrimSpace(identity.Status)
	switch {
	case reference != "" && status != "":
		return locale.Text("identity.name_reference_status", map[string]string{"primary": primary, "reference": reference, "status": status})
	case reference != "":
		return locale.Text("identity.name_pair", map[string]string{"first": primary, "second": reference})
	case status != "":
		return locale.Text("identity.name_pair", map[string]string{"first": primary, "second": status})
	default:
		return primary
	}
}

// viewerDisplayName is PreferredFamilyName limited to what the viewer may
// see: a legal name the server withheld contributes no family name, so the
// header, breadcrumb and title cannot leak it (UXAUDIT-016).
func viewerDisplayName(view View, person Person) string {
	if _, status := resolveIdentityField(view.Locale, person, workerIdentityVerdicts(view), "legal_name", person.LegalName); status == WorkerFactWithheld {
		person.LegalName = ""
	}
	return PreferredFamilyName(person)
}
