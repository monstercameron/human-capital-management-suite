package productui

// IconDefinition is one governed, decorative interface glyph. Components
// refer to stable names; BrandPacks may replace logo assets later but cannot
// inject arbitrary SVG or alter semantic labels through the icon channel.
type IconDefinition struct {
	Name string
	Path string
}

var registeredIcons = []IconDefinition{
	{Name: "home", Path: "M3 10.5 12 3l9 7.5V21h-6v-6H9v6H3z"},
	{Name: "journeys", Path: "M5 4h10l4 4v12H5zM15 4v4h4M8 12h8M8 16h6"},
	{Name: "work", Path: "M9 5h6m-7-2h8v4H8zM5 5h14v16H5zM8 11h8M8 15h8"},
	{Name: "history", Path: "M3 12a9 9 0 1 0 3-6.7M3 4v5h5M12 7v5l3 2"},
	{Name: "people", Path: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75"},
	{Name: "organization", Path: "M12 3v6M5 21v-5h14v5M5 16v-3h14v3M12 9v4"},
	{Name: "insights", Path: "M4 20V10M10 20V4M16 20v-7M22 20H2"},
	{Name: "admin", Path: "M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7zM19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.12 2.12-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1 1.56V20.3h-3v-.08a1.7 1.7 0 0 0-1-1.56 1.7 1.7 0 0 0-1.88.34l-.06.06-2.12-2.12.06-.06A1.7 1.7 0 0 0 7.08 15a1.7 1.7 0 0 0-1.56-1H5.4v-3h.12a1.7 1.7 0 0 0 1.56-1 1.7 1.7 0 0 0-.34-1.88l-.06-.06L8.8 5.94l.06.06a1.7 1.7 0 0 0 1.88.34 1.7 1.7 0 0 0 1-1.56V4.7h3v.08a1.7 1.7 0 0 0 1 1.56 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.12 2.12-.06.06A1.7 1.7 0 0 0 19.4 10a1.7 1.7 0 0 0 1.56 1h.12v3h-.12a1.7 1.7 0 0 0-1.56 1z"},
	{Name: "studio", Path: "M4 4h16v16H4zM4 9h16M9 9v11"},
	{Name: "help", Path: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM9.5 9a2.5 2.5 0 1 1 3.7 2.2c-.8.5-1.2 1-1.2 2M12 17h.01"},
	{Name: "settings", Path: "M4 7h10M18 7h2M4 17h2M10 17h10M14 4v6M6 14v6"},
	{Name: "notifications", Path: "M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"},
	{Name: "palette", Path: "M12 3a9 9 0 1 0 0 18h1.2a2 2 0 0 0 0-4H12a1.8 1.8 0 0 1 0-3.6c0 .4-.3.8-.8.8A3.6 3.6 0 0 1 4 14.2 9 9 0 0 1 12 3zM7.5 9h.01M11 6.5h.01M15.5 7.5h.01M17 12h.01"},
	{Name: "collapse", Path: "M15 18l-6-6 6-6"},
	{Name: "expand", Path: "M9 18l6-6-6-6"},
	{Name: "menu", Path: "M4 6h16M4 12h16M4 18h16"},
	{Name: "close", Path: "M6 6l12 12M18 6 6 18"},
	{Name: "search", Path: "M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zM17 17l5 5"},
	{Name: "favorite", Path: "m12 2 3.1 6.3 7 .9-5.1 5 .9 7-6.9-3.6-6.9 3.6.9-7-5.1-5 7-.9z"},
	{Name: "privacy", Path: "M6 10V7a6 6 0 0 1 12 0v3M5 10h14v11H5zM12 14v3"},
	{Name: "launch", Path: "M5 19 19 5M9 5h10v10"},
	{Name: "check", Path: "M20 6 9 17l-5-5"},
	{Name: "history-back", Path: "M20 12H4M10 18l-6-6 6-6"},
	{Name: "history-forward", Path: "M4 12h16M14 6l6 6-6 6"},
}

const fallbackIconPath = "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z"

// IconDefinitions returns an immutable copy of the governed vocabulary.
func IconDefinitions() []IconDefinition {
	return append([]IconDefinition(nil), registeredIcons...)
}

func iconPath(name string) string {
	for _, definition := range registeredIcons {
		if definition.Name == name {
			return definition.Path
		}
	}
	return fallbackIconPath
}
