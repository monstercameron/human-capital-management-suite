package chatui

// Constants retain translations without compiling a large per-key switch into wasm.
func integrate1Copy(table, locale, key string) string {
	return chatbug039CompactText(table, locale, key)
}
