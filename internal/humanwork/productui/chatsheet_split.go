package productui

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"sync"
)

// The product stylesheet is served as two sheets. The control sheet carries
// the shell and every product page; the Chat sheet carries the rules only a
// Chat page uses (Chat's workspace, its viewer and its settings), which are by
// far the largest and fastest growing part of the whole. A document links the
// control sheet always and the Chat sheet only on a Chat page; a page the
// client navigates to Chat from loads the Chat sheet before it paints Chat
// (ChatStylesheetMetaName).
//
// Stylesheet still returns the whole of it, in the order it has always had, for
// callers that need one self-contained sheet.

// StylesheetAssetPath is the directory the workspace serves stylesheet assets
// from. internal/humanwork/workspace names the same directory (PathAssetPrefix).
const StylesheetAssetPath = "/workspace/assets/"

const (
	// ControlStylesheetAssetPrefix and ChatStylesheetAssetPrefix start the
	// names of the two stylesheet assets; the rest is the SHA-256 of the bytes.
	ControlStylesheetAssetPrefix = "product-"
	ChatStylesheetAssetPrefix    = "chat-"
	// StylesheetAssetSuffix ends every stylesheet asset name.
	StylesheetAssetSuffix = ".css"
	// ChatStylesheetMetaName names the head element a document without the
	// Chat sheet carries so the client can load it on its way into Chat: its
	// content is the sheet's address and its data-integrity attribute the
	// subresource-integrity value to load it with.
	ChatStylesheetMetaName = "hcm-chat-stylesheet"
)

// StylesheetAsset is the address a sheet is served and pinned under. The name
// carries the digest of the bytes, so the browser keeps the sheet for good and
// a changed sheet is a different address.
type StylesheetAsset struct {
	// Name is the asset name: the prefix, the digest in lower-case hex, ".css".
	Name string
	// Href is the address the document links.
	Href string
	// Integrity is the digest in the form subresource integrity and the
	// content security policy take.
	Integrity string
}

// NewStylesheetAsset names the sheet with this body under prefix.
func NewStylesheetAsset(prefix, body string) StylesheetAsset {
	sum := sha256.Sum256([]byte(body))
	name := prefix + hex.EncodeToString(sum[:]) + StylesheetAssetSuffix
	return StylesheetAsset{
		Name:      name,
		Href:      StylesheetAssetPath + name,
		Integrity: "sha256-" + base64.StdEncoding.EncodeToString(sum[:]),
	}
}

// LinkElement is the head element that loads the sheet.
func (a StylesheetAsset) LinkElement() string {
	return `<link rel="stylesheet" href="` + a.Href + `" integrity="` + a.Integrity + `">`
}

// MetaElement is the head element that tells the client where the sheet is
// without loading it.
func (a StylesheetAsset) MetaElement() string {
	return `<meta name="` + ChatStylesheetMetaName + `" content="` + a.Href + `" data-integrity="` + a.Integrity + `">`
}

// PageNeedsChatStylesheet reports whether a page draws Chat's rules: the Chat
// workspace (and the loading frame that stands in for it) and Chat's settings.
// The agent pages sit under Chat in the navigation but style themselves.
func PageNeedsChatStylesheet(page PageID) bool {
	return page == PageChat || page == PageChatSettings
}

// ChatStylesheet is the rules only a Chat page uses. They do not depend on the
// customer's theme, so there is one Chat sheet for every tenant.
func ChatStylesheet() string {
	platformStylesOnce.Do(initPlatformStyles)
	return platformChatStyles
}

var (
	chatStylesheetAssetOnce sync.Once
	chatStylesheetAsset     StylesheetAsset
)

// ChatStylesheetAsset is the address of ChatStylesheet. It is a function over
// plain package variables, not a package-level closure, so the browser client,
// which names PageNeedsChatStylesheet but never builds a sheet, does not link
// the stylesheets with it.
func ChatStylesheetAsset() StylesheetAsset {
	chatStylesheetAssetOnce.Do(func() {
		chatStylesheetAsset = NewStylesheetAsset(ChatStylesheetAssetPrefix, ChatStylesheet())
	})
	return chatStylesheetAsset
}

// ChatStylesheetElement is what a document's head carries for the Chat sheet:
// the link that loads it on a Chat page, the meta element that names it on any
// other page.
func ChatStylesheetElement(page PageID) string {
	asset := ChatStylesheetAsset()
	if PageNeedsChatStylesheet(page) {
		return asset.LinkElement()
	}
	return asset.MetaElement()
}

var (
	controlStylesheetOnce sync.Once
	controlStylesheet     string
)

// ControlStylesheet is the product stylesheet without Chat's rules: the sheet
// of the shell and of every page that is not Chat.
func ControlStylesheet() string {
	controlStylesheetOnce.Do(func() {
		theme, _ := ResolveTheme(nil)
		platformStylesOnce.Do(initPlatformStyles)
		controlStylesheet = controlStylesheetForTheme(theme)
	})
	return controlStylesheet
}

func controlStylesheetForTheme(theme Theme) string {
	return theme.CSS() + platformHeadStyles + platformStyles + customAppearancePreviewStylesheet(theme) + platformDarkStyles
}

// ControlStylesheetForCustomerTheme is StylesheetForCustomerTheme without
// Chat's rules.
func ControlStylesheetForCustomerTheme(customer CustomerTheme) (string, error) {
	if err := ValidateCustomerTheme(customer); err != nil {
		return "", err
	}
	if customer.Palette != "custom" {
		return ControlStylesheet(), nil
	}
	modes, err := ResolveThemeModesWithDark(customer.TokenOverrides, customer.DarkTokenOverrides)
	if err != nil {
		return "", err
	}
	platformStylesOnce.Do(initPlatformStyles)
	return modes[ThemeModeLight].CSS() + platformHeadStyles + platformStyles + customAppearancePreviewStylesheetForModes(modes) + darkModeStylesStylesheetForCustomer(modes[ThemeModeLight], modes[ThemeModeDark]), nil
}

// RenderLinked is Render for a document that links its stylesheet the way the
// workspace serves it, instead of carrying the sheet inline: the control sheet
// always, the Chat sheet on a Chat page, and a note of where the Chat sheet is
// on any other page. It is the document a person's browser downloads on each
// navigation, so it is what a markup budget measures; the sheets are measured
// separately because the browser keeps them.
//
// The control sheet here is the product sheet alone. The workspace puts the
// journey's sheet in front of it, which changes the digest in the link and
// nothing about the document's size.
func RenderLinked(view View) (string, error) {
	return renderDocument(view, true)
}

func linkedStyleElements(controlBody string, page PageID) string {
	control := NewStylesheetAsset(ControlStylesheetAssetPrefix, controlBody)
	return control.LinkElement() + ChatStylesheetElement(page)
}
