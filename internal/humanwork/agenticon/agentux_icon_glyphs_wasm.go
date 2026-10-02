//go:build js && wasm

package agenticon

// Glyph is a product-drawn path on a 24 by 24 grid and its en-US, de-DE, ar vocabulary.
type Glyph struct {
	ID, Path string
	Keywords [3]string
}

// Glyphs returns a fresh catalog so callers cannot mutate the generator.
func Glyphs() []Glyph { return glyphs() }

func glyphs() []Glyph {
	return []Glyph{
		{"neutral", "M12 3 21 12 12 21 3 12Z M8 12h8 M12 8v8", [3]string{}},
		{"calendar", "M4 5h16v16H4Z M4 9h16 M8 3v4 M16 3v4 M8 13h2 M14 13h2 M8 17h2", [3]string{}},
		{"cake", "M4 12h16v9H4Z M4 16q2 3 4 0t4 0t4 0t4 0 M8 12V8 M16 12V8 M8 5v1 M16 5v1", [3]string{}},
		{"headset", "M4 14v-3a8 8 0 0 1 16 0v7l-4 3h-4 M4 12h3v6H4Z M17 12h3v6h-3Z", [3]string{}},
		{"checklist", "M4 5l2 2 3-4 M12 5h8 M4 12l2 2 3-4 M12 12h8 M4 19l2 2 3-4 M12 19h8", [3]string{}},
		{"bell", "M5 17h14l-2-3V9a5 5 0 0 0-10 0v5Z M10 21h4 M12 2v2", [3]string{}},
		{"book", "M3 4h5q4 0 4 3v14q-1-3-4-3H3Z M21 4h-5q-4 0-4 3 M21 4v14h-5q-4 0-4 3", [3]string{}},
		{"megaphone", "M3 9h5l12-5v16L8 15H3Z M7 15l2 6h4l-2-5", [3]string{}},
		{"magnifier", "M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14Z M15 15l6 6", [3]string{}},
		{"shield", "M12 2 21 6v6q-1 7-9 10-8-3-9-10V6Z M8 12l3 3 5-6", [3]string{}},
		{"coins", "M3 6c0-4 12-4 12 0s-12 4-12 0Z M3 6v5c0 4 12 4 12 0V6 M3 11v5c0 3 7 4 10 2 M17 10c-6 0-6 11 0 11s6-11 0-11Z", [3]string{}},
		{"people", "M8 3a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z M2 20v-3a6 6 0 0 1 12 0v3 M17 4a3 3 0 0 1 0 6 M17 13q5 0 5 7", [3]string{}},
		{"spark", "M12 2l3 7 7 3-7 3-3 7-3-7-7-3 7-3Z", [3]string{}},
		{"clock", "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Z M12 6v6l4 3", [3]string{}},
		{"briefcase", "M3 7h18v14H3Z M8 7V3h8v4 M3 12h18 M10 12v3h4v-3", [3]string{}},
		{"chart", "M3 3v18h18 M7 17v-5 M12 17V7 M17 17V4", [3]string{}},
		{"heart", "M12 21 3 12C-3 3 9 0 12 7c3-7 15-4 9 5Z", [3]string{}},
		{"medical", "M8 3h8v5h5v8h-5v5H8v-5H3V8h5Z", [3]string{}},
		{"graduation", "M2 8l10-5 10 5-10 5Z M6 11v6q6 5 12 0v-6 M22 8v9", [3]string{}},
		{"pencil", "M3 21l1-6L16 3l5 5L9 20Z M13 6l5 5 M4 15l5 5", [3]string{}},
		{"document", "M5 2h10l5 5v15H5Z M15 2v5h5 M8 11h9 M8 15h9 M8 19h6", [3]string{}},
		{"folder", "M2 6h8l2 3h10v12H2Z M2 9V3h8l2 3h10v3", [3]string{}},
		{"mail", "M2 5h20v14H2Z M2 5l10 8L22 5", [3]string{}},
		{"chat", "M3 3h18v14H9l-6 5Z M7 7h10 M7 11h7", [3]string{}},
		{"globe", "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Z M2 12h20 M12 2q-9 10 0 20 9-10 0-20", [3]string{}},
		{"translate", "M2 4h12 M8 2v2 M4 4q1 8 10 12 M12 4q-1 8-10 12 M13 22l4-12 5 12 M15 18h5", [3]string{}},
		{"plane", "M2 11l8-2 2-7 2 7 8 2v3l-8-1-1 6 3 2H8l3-2-1-6-8 1Z", [3]string{}},
		{"map", "M2 5l7-3 6 3 7-3v17l-7 3-6-3-7 3Z M9 2v17 M15 5v17", [3]string{}},
		{"pin", "M12 22q-8-8-8-13a8 8 0 1 1 16 0q0 5-8 13Z M12 6a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z", [3]string{}},
		{"home", "M2 11 12 2l10 9 M5 9v13h5v-7h4v7h5V9", [3]string{}},
		{"building", "M4 22V2h16v20 M8 6h2 M14 6h2 M8 10h2 M14 10h2 M8 14h2 M14 14h2 M10 22v-4h4v4", [3]string{}},
		{"network", "M9 2h6v6H9Z M2 16h6v6H2Z M16 16h6v6h-6Z M12 8v4 M5 16v-4h14v4", [3]string{}},
		{"key", "M7 3a5 5 0 1 0 0 10 5 5 0 0 0 0-10Z M11 12l10 10 M16 17l3-3 M19 20l3-3", [3]string{}},
		{"lock", "M5 10h14v12H5Z M8 10V6a4 4 0 0 1 8 0v4 M12 15v3", [3]string{}},
		{"scales", "M12 2v19 M6 21h12 M3 6h18 M6 6l-4 8h8Z M18 6l-4 8h8Z", [3]string{}},
		{"contract", "M4 2h16v20H4Z M7 6h10 M7 10h10 M7 17q2-5 4 0t5 0", [3]string{}},
		{"wallet", "M3 5h17v16H3Z M3 5V2h14v3 M15 10h7v7h-7Z M18 13h1", [3]string{}},
		{"receipt", "M5 2l3 2 4-2 4 2 3-2v20l-3-2-4 2-4-2-3 2Z M8 8h8 M8 12h8 M8 16h5", [3]string{}},
		{"bank", "M2 7l10-5 10 5Z M4 10v9 M9 10v9 M15 10v9 M20 10v9 M2 22h20", [3]string{}},
		{"calculator", "M5 2h14v20H5Z M8 5h8v4H8Z M8 13h1 M15 13h1 M8 17h1 M15 17h1", [3]string{}},
		{"gift", "M2 9h20v4H2Z M4 13v9h16v-9 M12 9v13 M12 9C0 9 5-3 12 9c7-12 12 0 0 0", [3]string{}},
		{"star", "M12 2l3 6 7 1-5 5 1 8-6-4-6 4 1-8-5-5 7-1Z", [3]string{}},
		{"trophy", "M7 2h10v9a5 5 0 0 1-10 0Z M7 5H2v4q0 5 5 5 M17 5h5v4q0 5-5 5 M12 16v6 M7 22h10", [3]string{}},
		{"target", "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Z M12 7a5 5 0 1 0 0 10 5 5 0 0 0 0-10Z M12 12l9-9", [3]string{}},
		{"flag", "M5 22V2 M5 3h14l-3 5 3 5H5", [3]string{}},
		{"rocket", "M8 16q0-12 13-14 1 13-11 15Z M8 10l-6 4v5l6-3 M15 16l-1 6h-5l1-5 M5 19l-3 3 M15 6l3 3", [3]string{}},
		{"bulb", "M8 17v-3a7 7 0 1 1 8 0v3Z M8 20h8 M10 23h4 M12 17v-6", [3]string{}},
		{"gear", "M9 2h6v4l4-2 3 5-4 3 4 3-3 5-4-2v4H9v-4l-4 2-3-5 4-3-4-3 3-5 4 2Z M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z", [3]string{}},
		{"wrench", "M14 3q-5 0-5 6L2 16l6 6 7-7q6 0 6-6l-4 3-5-5Z", [3]string{}},
		{"code", "M8 5l-6 7 6 7 M16 5l6 7-6 7 M14 2l-4 20", [3]string{}},
		{"database", "M3 5c0-5 18-5 18 0s-18 5-18 0Z M3 5v14c0 5 18 5 18 0V5 M3 12c0 5 18 5 18 0", [3]string{}},
		{"cloud", "M6 20a5 5 0 0 1-1-10 7 7 0 0 1 13-2 6 6 0 0 1 1 12Z", [3]string{}},
		{"link", "M10 7l3-3a5 5 0 0 1 7 7l-3 3 M14 17l-3 3a5 5 0 0 1-7-7l3-3 M8 16l8-8", [3]string{}},
		{"workflow", "M2 3h7v6H2Z M15 15h7v6h-7Z M9 6h10v9 M5 9v9h10", [3]string{}},
		{"filter", "M2 3h20l-8 9v8l-4 2V12Z", [3]string{}},
		{"compass", "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Z M8 16l3-7 5-1-3 7Z", [3]string{}},
		{"leaf", "M3 21C-3 7 8 1 22 2c0 14-6 20-19 19Z M3 21 17 7", [3]string{}},
		{"sun", "M12 7a5 5 0 1 0 0 10 5 5 0 0 0 0-10Z M12 1v3 M12 20v3 M1 12h3 M20 12h3 M4 4l2 2 M18 18l2 2 M20 4l-2 2 M6 18l-2 2", [3]string{}},
		{"moon", "M20 15A10 10 0 0 1 9 2a10 10 0 1 0 11 13Z", [3]string{}},
		{"handshake", "M2 8l5-4 5 3 5-3 5 4-6 12-4-2-4 2Z M7 4l5 3-4 5 3 2 5-4 6 5", [3]string{}},
		{"clipboard", "M8 5H4v17h16V5h-4 M8 2h8v5H8Z M8 12h8 M8 16h6", [3]string{}},
		{"lifebuoy", "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Z M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z M5 5l4 4 M15 15l4 4 M19 5l-4 4 M9 15l-4 4", [3]string{}},
		{"truck", "M2 5h12v13H2Z M14 9h5l3 5v4h-8 M6 16a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z M18 16a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z", [3]string{}},
	}
}
