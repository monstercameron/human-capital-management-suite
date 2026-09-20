package journeyclient

import (
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// REV-095-01: the edit-proposal dialog validates its required fields in the
// client, before any request is sent, from the Required flag its own
// projected fields carry. A refused submit marks each blank field invalid
// with an inline message and moves focus to the first one; the dialog stays
// open, because the answer the reader needs is inside it.

// editProposalAction returns the projected edit-proposal action on page.
func editProposalAction(page journey.Page) (journey.Action, bool) {
	if page.Detail == nil {
		return journey.Action{}, false
	}
	for _, action := range page.Detail.Actions {
		if action.ID == ActionEditProposal && !action.Disabled {
			return action, true
		}
	}
	return journey.Action{}, false
}

// editProposalMissing reports the blank required edit fields, keyed by field
// id with the localized inline message. ok is false when the page carries no
// offered edit action to validate against.
func (a *App) editProposalMissing(values map[string]string) (errs map[string]string, ok bool) {
	action, found := editProposalAction(a.store.Page())
	if !found {
		return nil, false
	}
	missing := journey.MissingRequired(action.Fields, values)
	if len(missing) == 0 {
		return nil, true
	}
	message := a.localeCopy().Text("journey.required_field")
	errs = make(map[string]string, len(missing))
	for _, field := range missing {
		errs[field.ID] = message
	}
	return errs, true
}

// refuseEditProposal publishes the field errors and asks the browser adapter
// to focus the first invalid field (Page.FocusInvalidRevision).
func (a *App) refuseEditProposal(errs map[string]string) {
	a.mu.Lock()
	a.editErrors = errs
	a.proposalFocusRevision++
	a.mu.Unlock()
	a.show(keyedNotice(toneWarning, "journey.required_fields_title", "journey.required_fields_detail"))
}

// applyEditErrors copies the current edit-dialog errors onto the projected
// edit action's fields.
func applyEditErrors(page *journey.Page, errs map[string]string) {
	if page == nil || page.Detail == nil || len(errs) == 0 {
		return
	}
	for i := range page.Detail.Actions {
		if page.Detail.Actions[i].ID == ActionEditProposal {
			page.Detail.Actions[i].Fields = journey.WithFieldErrors(page.Detail.Actions[i].Fields, errs)
		}
	}
}

// clearEditFieldError drops the error a field's own edit answers, in the same
// render as the keystroke, so the message does not outlive the fix.
func (a *App) clearEditFieldError(fieldID string) {
	a.mu.Lock()
	_, had := a.editErrors[fieldID]
	if had {
		delete(a.editErrors, fieldID)
	}
	a.mu.Unlock()
	if !had {
		return
	}
	a.store.Update(func(page *journey.Page) {
		if page.Detail == nil {
			return
		}
		for i := range page.Detail.Actions {
			if page.Detail.Actions[i].ID != ActionEditProposal {
				continue
			}
			fields := append([]journey.Field(nil), page.Detail.Actions[i].Fields...)
			for j := range fields {
				if fields[j].ID == fieldID {
					fields[j].Error = ""
				}
			}
			page.Detail.Actions[i].Fields = fields
		}
	})
}
