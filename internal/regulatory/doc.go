// Package regulatory computes workforce-action compliance facts from
// published legal rule packs (WF-CAP-011): rolling layoff-window counts,
// release-disclosure decisional units, and deadlines resolved as statute
// references a time expression can consume. Timing and thresholds always come
// from the pack; this package only does the calendar math.
package regulatory
