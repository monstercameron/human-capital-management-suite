package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// SignedOutProps carries the server-projected signed-out state. Detail is
// the server's sign-out text, Revoked the display names of the grants the
// server revoked, and SignInHref the single sign-in destination.
// Presentation revokes nothing itself: it renders the server's converged
// verdict and invents no grant, detail, or destination.
type SignedOutProps struct {
	I18nProps
	Detail     string
	Revoked    []string
	SignInHref string
	Navigate   func(string)
}

// UnauthenticatedRecovery builds the shared recovery projection for an
// UNAUTHENTICATED response. The server did not provide revoked grants or a
// safe detail, so this mapping supplies only the application's sign-in route.
func UnauthenticatedRecovery() *SignedOutProps {
	return &SignedOutProps{SignInHref: "/workspace/login"}
}

// signedOutState reports whether the shell is in the converged signed-out
// state. Every authority surface — the session warning, step-up prompt,
// authority banner, break-glass prompt, simulation panel, context
// switcher, and delegation selector — checks this first, so a stale
// projection lingering on the view can never resurrect revoked authority.
func signedOutState(view View) bool {
	return view.SignedOut != nil
}

// SignedOut renders the converged signed-out panel: what happened, which
// grants the server revoked, and the single sign-in path. An unsafe
// sign-in destination yields no link instead of an invented one. Blank
// detail and revoked projections omit their blocks. The panel is never
// locally dismissible: there is no authority left to return to.
func SignedOut(props SignedOutProps) ui.Node {
	nodes := []ui.Node{}
	if strings.TrimSpace(props.Detail) != "" {
		nodes = append(nodes, html.P(html.Props{Class: "signed-out-detail"}, ui.Text(props.Detail)))
	}
	var revoked []string
	seen := make(map[string]struct{}, len(props.Revoked))
	for _, grant := range props.Revoked {
		if trimmed := strings.TrimSpace(grant); trimmed != "" {
			if _, duplicate := seen[trimmed]; duplicate {
				continue
			}
			seen[trimmed] = struct{}{}
			revoked = append(revoked, trimmed)
		}
	}
	if len(revoked) > 0 {
		items := make([]ui.Node, 0, len(revoked))
		for _, grant := range revoked {
			items = append(items, html.Li(html.Props{Class: "signed-out-grant"}, ui.Text(grant)))
		}
		nodes = append(nodes, html.Ul(html.Props{Class: "signed-out-revoked", Aria: map[string]string{"label": props.Text("signed_out.revoked")}}, items...))
	}
	if validRecoveryHref(props.SignInHref) {
		nodes = append(nodes, html.Div(html.Props{Class: "signed-out-actions"},
			softwareLink(props.Navigate, html.Props{Class: "signed-out-signin"}, props.SignInHref, ui.Text(props.Text("signed_out.signin")))))
	}
	return html.Section(html.Props{ID: "signed-out", Class: "signed-out", Role: "alert", Aria: map[string]string{"labelledby": "signed-out-title"}},
		append([]ui.Node{html.H2(html.Props{ID: "signed-out-title", Class: "signed-out-title"}, ui.Text(props.Text("signed_out.title")))}, nodes...)...,
	)
}

func signedOut(view View) ui.Node {
	props := *view.SignedOut
	props.I18nProps = I18nProps{Locale: view.Locale}
	props.Navigate = view.Navigate
	return ui.CreateElement(SignedOut, props)
}
