package graphcanvas

import (
	"math"
	"strconv"
)

// sqrt is math.Sqrt behind a local name so model.go reads without imports.
func sqrt(v float64) float64 { return math.Sqrt(v) }

// formatNum renders a coordinate deterministically: integers without a
// decimal point, others trimmed to two decimals.
func formatNum(v float64) string {
	rounded := math.Round(v*100) / 100
	if rounded == math.Trunc(rounded) && math.Abs(rounded) < 1e15 {
		return strconv.FormatInt(int64(rounded), 10)
	}
	return strconv.FormatFloat(rounded, 'f', 2, 64)
}

// formatPx renders a pixel length.
func formatPx(v float64) string { return formatNum(v) + "px" }
