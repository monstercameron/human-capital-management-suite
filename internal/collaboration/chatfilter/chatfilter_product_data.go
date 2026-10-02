package chatfilter

// Builtins are the product-owned word lists, one definition per language and
// category (see chatmod002_lists.go for the data, the version history and the
// posture on slurs). They have no enablement row and are therefore off by
// default; the default action is block.
func Builtins() []Definition {
	lists := builtinLists()
	out := make([]Definition, 0, len(lists))
	for _, l := range lists {
		out = append(out, Definition{ID: "builtin-" + l.Language + "-" + l.Category, Name: l.Category, Version: BuiltinListVersion, Language: l.Language, Kind: "words", Match: l.Terms, Action: "block", Product: true})
	}
	return out
}
