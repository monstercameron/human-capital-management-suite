package productui

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var roleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)

// roleCreateValidation contains presentation keys, not user input. Keeping
// the validation result separate from AccessRole lets the form report every
// fixable field without sending an invalid mutation to the service.
type roleCreateValidation struct {
	ID   string
	Name string
}

func (v roleCreateValidation) valid() bool { return v.ID == "" && v.Name == "" }

func validateRoleCreateDraft(draft AccessRole) roleCreateValidation {
	var result roleCreateValidation
	if strings.TrimSpace(draft.Name) == "" {
		result.Name = "roles.create_name_required"
	}
	id := strings.TrimSpace(draft.ID)
	switch {
	case id == "":
		result.ID = "roles.create_id_required"
	case !roleIDPattern.MatchString(id):
		result.ID = "roles.create_id_invalid"
	}
	return result
}

// deriveRoleID creates the editable, policy-safe starting value for a role
// display name. A leading digit is made valid without inventing a persona or
// changing the administrator's chosen words.
func deriveRoleID(displayName string) string {
	var builder strings.Builder
	underscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(displayName)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if builder.Len() == 0 && unicode.IsDigit(r) {
				builder.WriteString("role_")
			}
			builder.WriteRune(r)
			underscore = false
			continue
		}
		if builder.Len() > 0 && !underscore {
			builder.WriteByte('_')
			underscore = true
		}
	}
	id := strings.Trim(builder.String(), "_")
	if id == "" {
		return ""
	}
	if len(id) > 63 {
		id = strings.TrimRight(id[:63], "_")
	}
	return id
}

func roleCreateErrorNode(i18n I18nProps, id, key string) ui.Node {
	return html.P(html.Props{ID: id, Class: "form-error", Role: "alert"}, ui.Text(i18n.Text(key)))
}

func saveRoleValidated(save func(AccessRole), draft func() AccessRole, report func(roleCreateValidation)) ui.Handler {
	if save == nil || draft == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		current := draft()
		validation := validateRoleCreateDraft(current)
		if report != nil {
			report(validation)
		}
		if validation.valid() {
			save(current)
		}
	})
}

func formattedPeopleRole(role, grade string) string {
	role = strings.TrimSpace(role)
	grade = strings.TrimSpace(grade)
	if role == "" || grade == "" {
		return role
	}
	suffix := "· " + grade
	if strings.HasSuffix(role, suffix) {
		return strings.TrimSpace(strings.TrimSuffix(role, suffix))
	}
	return role
}

func formattedPeopleLocation(location string) string {
	location = strings.TrimSpace(location)
	location = strings.TrimSpace(strings.TrimSuffix(location, " jobsite"))
	return location
}
