package main

// chatbug061HeightLimit bounds how many card heights one page keeps. The page
// keeps them in memory only: browser storage is not used for chat (WEB-031).
const chatbug061HeightLimit = 400

// chatbug061MergeHeights returns the heights the page should keep after a
// measurement, and whether anything changed. The map it returns is a new one
// when it changed, so a render in flight never sees it half written. A
// measurement of zero (a card not laid out yet) is ignored.
func chatbug061MergeHeights(known, measured map[string]int) (map[string]int, bool) {
	changed := false
	for post, height := range measured {
		if post != "" && height > 0 && known[post] != height {
			changed = true
			break
		}
	}
	if !changed {
		return known, false
	}
	next := make(map[string]int, len(known)+len(measured))
	for post, height := range known {
		next[post] = height
	}
	for post, height := range measured {
		if post == "" || height <= 0 {
			continue
		}
		if _, held := next[post]; !held && len(next) >= chatbug061HeightLimit {
			continue
		}
		next[post] = height
	}
	return next, true
}
