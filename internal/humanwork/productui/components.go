package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
)

func appLink(view View, props html.Props, href string, children ...ui.Node) ui.Node {
	return softwareLink(view.Navigate, props, href, children...)
}

// softwareLink is the narrow navigation primitive used by reusable feature
// components. It accepts only the capability the link needs instead of the
// page-wide View projection.
func softwareLink(navigate func(string), props html.Props, href string, children ...ui.Node) ui.Node {
	props.Href = href
	if navigate != nil && strings.HasPrefix(href, "/workspace/app/") {
		props.OnClick = ui.UseEvent(func(event ui.MouseEvent) {
			event.PreventDefault()
			navigate(href)
		})
	}
	return html.A(props, children...)
}

func navIcon(name string) ui.Node {
	return productIcon(name, "nav-icon")
}

func productIcon(name, class string) ui.Node {
	return html.Tag("svg", html.Props{Class: class, Raw: map[string]any{
		"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8",
		"stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false",
	}}, html.Tag("path", html.Props{Raw: map[string]any{"d": iconPath(name)}}))
}

func unavailablePanel(title, detail string) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{Title: title, Description: detail, Role: "status"})
}

func personAvatar(name, label, photoURL, size string) ui.Node {
	class := "avatar"
	if size != "" {
		class += " " + size
	}
	return uicomponents.Avatar(uicomponents.AvatarProps{
		Name: name, Initials: label, PhotoURL: photoURL, Class: class, Decorative: true,
	})
}
