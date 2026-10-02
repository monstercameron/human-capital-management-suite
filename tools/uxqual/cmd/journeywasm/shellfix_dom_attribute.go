package main

// normalizedDOMAttribute makes the absent/null browser attribute value safe
// for callers that require a string. Keeping it untagged gives the native
// shell test the same contract as the WASM helper.
func normalizedDOMAttribute(value string, isString bool) string {
	if !isString {
		return ""
	}
	return value
}
