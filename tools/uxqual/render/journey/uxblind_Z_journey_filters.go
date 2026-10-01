package journey

// journeyFilterCollapsible keeps the secondary controls quiet on a short
// tracker. Once the list is large enough, sorting, grouping, and date bounds
// stay visible because the reader is more likely to need them while scanning
// a longer result set.
func journeyFilterCollapsible(f *JourneyFilterView) bool {
	return f != nil && f.Total <= 5
}
