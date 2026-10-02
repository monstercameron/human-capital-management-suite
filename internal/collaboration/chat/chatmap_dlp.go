package chat

// PostCarriesLocation reports whether a message carries a shared location.
func PostCarriesLocation(post Post) bool {
	for _, ref := range post.References {
		if ref.Kind == LocationReference {
			return true
		}
	}
	return false
}

// RaiseForLocation lifts the data class a message's text was given to the
// location's class when the message carries a shared location, so every reader
// of the class (the audience floor, the outbound verifier, exports and agents)
// treats the message as special-category data whatever its words say. The
// classes are strings here because this package does not import the DLP one.
func RaiseForLocation(post Post, class string) string {
	if PostCarriesLocation(post) {
		return LocationDLPClass
	}
	return class
}
