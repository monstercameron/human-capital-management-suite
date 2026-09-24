package docsdiagram

import (
	"math"
	"strconv"
	"strings"
)

// seriesTokens is the categorical palette, ordered so neighbouring series
// differ in hue before they repeat a token. No slot may sit near the brand
// teal: success green did, and two pie slices read as one. The theme has no
// magenta token, so slot 5 names one per scheme; TestSeriesPaletteIsDistinct
// holds every pair apart in both light and dark.
var seriesTokens = [8]string{
	"var(--accent)",
	"var(--hcm-color-info)",
	"var(--hcm-color-warning)",
	"var(--hcm-color-danger)",
	"color-mix(in srgb,var(--hcm-color-info) 55%,var(--hcm-color-danger))",
	"light-dark(#a3348f,#e58bd3)",
	"color-mix(in srgb,var(--hcm-color-warning) 50%,var(--hcm-color-danger))",
	"color-mix(in srgb,var(--muted) 50%,var(--hcm-color-warning))",
}

// minWidthRatio keeps drawn text at three quarters of its natural size or
// larger; a narrower screen scrolls the canvas instead of shrinking it.
const minWidthRatio = 0.75

// Stylesheet returns the CSS for every class Render emits. It only reads the
// product theme tokens, so it follows light, dark and customer themes.
func Stylesheet() string {
	var b strings.Builder
	b.WriteString(baseCSS)
	for i, tok := range seriesTokens {
		n := strconv.Itoa(i)
		b.WriteString(".docs-diagram .series-" + n + "{--dg-series:" + tok + ";fill:color-mix(in srgb," + tok + " 78%,var(--surface));stroke:" + tok + "}")
	}
	b.WriteString(markCSS)
	for w := 160; w <= maxWidthBucket; w += 80 {
		n := strconv.Itoa(w)
		minW := strconv.Itoa(int(math.Round(float64(w) * minWidthRatio)))
		b.WriteString(".docs-diagram-w-" + n + "{max-width:" + n + "px;min-width:" + minW + "px}")
	}
	return b.String()
}

const baseCSS = `.docs-diagram{--dg-series:var(--accent);direction:ltr;margin:1rem 0;max-width:100%;min-width:0}` +
	`.docs-diagram-caption{margin:0 0 .5rem;color:var(--ink);font-weight:600;unicode-bidi:plaintext;text-align:start}` +
	// Scroll shadows: the two local covers travel with the content and hide
	// the fixed shadows at either end, so a shadow shows only where more of
	// a wide chart is scrolled out of view.
	`.docs-diagram-canvas{overflow-x:auto;overflow-y:hidden;padding:8px;border:1px solid var(--line);border-radius:8px;background:linear-gradient(to right,var(--surface) 40%,transparent) left/2.5rem 100% no-repeat local,linear-gradient(to left,var(--surface) 40%,transparent) right/2.5rem 100% no-repeat local,radial-gradient(farthest-side at 0 50%,color-mix(in srgb,var(--ink) 22%,transparent),transparent) left/.9rem 100% no-repeat scroll,radial-gradient(farthest-side at 100% 50%,color-mix(in srgb,var(--ink) 22%,transparent),transparent) right/.9rem 100% no-repeat scroll,var(--surface)}` +
	`.docs-diagram-canvas:focus-visible{outline:var(--hcm-focus-ring-width,2px) solid var(--hcm-color-focus);outline-offset:2px}` +
	`.docs-diagram-svg{display:block;width:100%;height:auto;margin-inline:auto;overflow:visible;font-family:inherit}` +
	`.docs-diagram-sr{position:absolute;width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);clip-path:inset(50%);white-space:nowrap;border:0}` +
	`.docs-diagram .dg-fo{overflow:visible}` +
	`.docs-diagram .dg-label{display:flex;flex-direction:column;justify-content:center;align-items:center;box-sizing:border-box;height:100%;margin:0;color:var(--ink);font-family:inherit;font-size:14px;line-height:18px;text-align:center}` +
	`.docs-diagram .dg-label span{display:block;white-space:nowrap}` +
	`.docs-diagram .dg-label-start{align-items:flex-start;text-align:start}` +
	`.docs-diagram .dg-label-end{align-items:flex-end;text-align:end}` +
	`.docs-diagram .dg-label-small{font-size:12px;line-height:15px}` +
	`.docs-diagram .dg-label-tiny{font-size:10px;line-height:12px}` +
	`.docs-diagram .dg-label-muted{color:var(--muted)}` +
	`.docs-diagram .dg-label-strong{font-weight:600}` +
	// Flowchart.
	`.docs-diagram .dg-node{fill:color-mix(in srgb,var(--accent) 10%,var(--surface));stroke:color-mix(in srgb,var(--accent) 60%,var(--line));stroke-width:1.5}` +
	`.docs-diagram .dg-node-decision{fill:color-mix(in srgb,var(--hcm-color-warning) 16%,var(--surface));stroke:color-mix(in srgb,var(--hcm-color-warning) 75%,var(--line))}` +
	`.docs-diagram .dg-node-terminal{fill:color-mix(in srgb,var(--hcm-color-success) 14%,var(--surface));stroke:color-mix(in srgb,var(--hcm-color-success) 70%,var(--line))}` +
	`.docs-diagram .dg-node-detail{fill:none;stroke:color-mix(in srgb,var(--accent) 60%,var(--line));stroke-width:1.2}` +
	`.docs-diagram .dg-cluster-box{fill:var(--surface-subtle);stroke:var(--line);stroke-width:1.2;stroke-dasharray:5 4}` +
	`.docs-diagram .dg-cluster-title{color:var(--muted);font-weight:600}` +
	`.docs-diagram .dg-edge{fill:none;stroke:var(--muted);stroke-width:1.5}` +
	`.docs-diagram .dg-edge-dotted{stroke-dasharray:5 4}` +
	`.docs-diagram .dg-edge-thick{stroke:var(--ink);stroke-width:3}` +
	`.docs-diagram .dg-edge-label-bg{fill:var(--surface);stroke:var(--line);stroke-width:1}` +
	`.docs-diagram .dg-arrowhead{fill:var(--muted);stroke:none}` +
	`.docs-diagram .dg-arrowhead-cross,.docs-diagram .dg-arrowhead-open{fill:none;stroke:var(--muted);stroke-width:1.8}` +
	// Sequence.
	`.docs-diagram-sequence .dg-arrowhead{fill:var(--ink)}` +
	`.docs-diagram-sequence .dg-arrowhead-cross,.docs-diagram-sequence .dg-arrowhead-open{stroke:var(--ink)}` +
	`.docs-diagram .dg-participant-box{fill:color-mix(in srgb,var(--accent) 10%,var(--surface));stroke:color-mix(in srgb,var(--accent) 60%,var(--line));stroke-width:1.5}` +
	`.docs-diagram .dg-actor-figure{fill:none;stroke:var(--ink);stroke-width:1.5}` +
	`.docs-diagram .dg-lifeline{stroke:var(--line);stroke-width:1.2;stroke-dasharray:4 4}` +
	`.docs-diagram .dg-msg{fill:none;stroke:var(--ink);stroke-width:1.4}` +
	`.docs-diagram .dg-msg-dotted{stroke-dasharray:5 4}` +
	`.docs-diagram .dg-note{fill:color-mix(in srgb,var(--hcm-color-warning) 18%,var(--surface));stroke:color-mix(in srgb,var(--hcm-color-warning) 65%,var(--line));stroke-width:1}` +
	`.docs-diagram .dg-frame{fill:none;stroke:var(--muted);stroke-width:1.2}` +
	`.docs-diagram .dg-frame-tab{fill:var(--surface-subtle);stroke:var(--muted);stroke-width:1.2}` +
	`.docs-diagram .dg-frame-kind{font-weight:600}` +
	`.docs-diagram .dg-frame-label-bg{fill:var(--surface);stroke:none}` +
	`.docs-diagram .dg-frame-divider{stroke:var(--muted);stroke-width:1.2;stroke-dasharray:5 4}` +
	`.docs-diagram .dg-activation{fill:var(--surface-subtle);stroke:var(--muted);stroke-width:1}` +
	`.docs-diagram .dg-seqnum-dot{fill:var(--accent);stroke:none}` +
	`.docs-diagram .dg-seqnum-text{color:var(--surface);font-weight:600}` +
	// Charts.
	`.docs-diagram .dg-grid{stroke:var(--line);stroke-width:1}` +
	`.docs-diagram .dg-axis{stroke:var(--muted);stroke-width:1.2}` +
	`.docs-diagram .dg-tick{stroke:var(--muted);stroke-width:1}`

// markCSS follows the series rules so a mark's own fill wins over the
// series fill at equal specificity.
const markCSS = `.docs-diagram .dg-bar{stroke-width:1}` +
	`.docs-diagram .dg-line{fill:none;stroke:var(--dg-series);stroke-width:2.5;stroke-linejoin:round;stroke-linecap:round}` +
	`.docs-diagram .dg-point{fill:var(--surface);stroke:var(--dg-series);stroke-width:2}` +
	`.docs-diagram .dg-slice{stroke:var(--surface);stroke-width:2}` +
	`.docs-diagram .dg-legend-swatch{stroke-width:1}` +
	// Gantt.
	`.docs-diagram .dg-section-band{fill:transparent;stroke:none}` +
	`.docs-diagram .dg-section-band-alt{fill:var(--surface-subtle)}` +
	`.docs-diagram .dg-task{stroke-width:1.2}` +
	`.docs-diagram .dg-task-planned{fill:color-mix(in srgb,var(--accent) 45%,var(--surface));stroke:var(--accent)}` +
	`.docs-diagram .dg-task-active{fill:color-mix(in srgb,var(--hcm-color-info) 35%,var(--surface));stroke:var(--hcm-color-info);stroke-width:2}` +
	`.docs-diagram .dg-task-done{fill:color-mix(in srgb,var(--muted) 35%,var(--surface));stroke:var(--muted)}` +
	`.docs-diagram .dg-task-crit{stroke:var(--hcm-color-danger);stroke-width:2}` +
	`.docs-diagram .dg-task-crit:not(.dg-task-done){fill:color-mix(in srgb,var(--hcm-color-danger) 45%,var(--surface))}` +
	`.docs-diagram .dg-milestone{fill:var(--ink);stroke:var(--surface);stroke-width:1.5}` +
	`.docs-diagram .dg-milestone.dg-task-crit{fill:var(--hcm-color-danger)}` +
	`.docs-diagram .dg-milestone-label{font-weight:600}` +
	// Timeline and journey.
	`.docs-diagram .dg-period,.docs-diagram .dg-event,.docs-diagram .dg-section-head{fill:color-mix(in srgb,var(--dg-series) 18%,var(--surface));stroke:var(--dg-series);stroke-width:1.2}` +
	`.docs-diagram .dg-event{fill:var(--surface)}` +
	`.docs-diagram .dg-event-link{stroke:var(--line);stroke-width:1.5;stroke-dasharray:3 3}` +
	`.docs-diagram .dg-timeline-axis{stroke-width:2}` +
	`.docs-diagram .dg-journey-path{fill:none;stroke:var(--muted);stroke-width:2;stroke-dasharray:6 4}` +
	`.docs-diagram .dg-face-dot{stroke-width:1.5}` +
	`.docs-diagram .dg-score-1 .dg-face-dot{fill:color-mix(in srgb,var(--hcm-color-danger) 30%,var(--surface));stroke:var(--hcm-color-danger)}` +
	`.docs-diagram .dg-score-2 .dg-face-dot{fill:color-mix(in srgb,color-mix(in srgb,var(--hcm-color-danger) 50%,var(--hcm-color-warning)) 30%,var(--surface));stroke:color-mix(in srgb,var(--hcm-color-danger) 50%,var(--hcm-color-warning))}` +
	`.docs-diagram .dg-score-3 .dg-face-dot{fill:color-mix(in srgb,var(--hcm-color-warning) 30%,var(--surface));stroke:var(--hcm-color-warning)}` +
	`.docs-diagram .dg-score-4 .dg-face-dot{fill:color-mix(in srgb,color-mix(in srgb,var(--hcm-color-warning) 40%,var(--hcm-color-success)) 30%,var(--surface));stroke:color-mix(in srgb,var(--hcm-color-warning) 40%,var(--hcm-color-success))}` +
	`.docs-diagram .dg-score-5 .dg-face-dot{fill:color-mix(in srgb,var(--hcm-color-success) 30%,var(--surface));stroke:var(--hcm-color-success)}` +
	// Forced colours: keep every line and mark visible.
	`@media (forced-colors:active){.docs-diagram .dg-edge,.docs-diagram .dg-msg,.docs-diagram .dg-axis,.docs-diagram .dg-line,.docs-diagram .dg-node,.docs-diagram .dg-participant-box{stroke:CanvasText}.docs-diagram .dg-arrowhead{fill:CanvasText}.docs-diagram .dg-label{color:CanvasText}}`
