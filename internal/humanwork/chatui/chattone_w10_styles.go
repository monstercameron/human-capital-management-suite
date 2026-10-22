package chatui

// ChattoneW10Styles: the writer's "Others see:" block, the "may be inaccurate"
// words, the translation report form and the first-read notice. They sit with
// the translation mark and use its tokens, so a message does not shift when they
// appear: each is a block below the text, not a change to it.
const ChattoneW10Styles = `.chattone-others{display:grid;gap:4px;margin:4px 0 0;padding:6px 10px;border-inline-start:2px solid var(--accent);background:var(--soft);border-radius:var(--hcm-radius-control);font-size:.8125rem;max-inline-size:90ch}` +
	`.chattone-others-label{font-weight:600;color:var(--muted)}` +
	`.chattone-others-text{font-size:.9375rem;line-height:1.45;color:var(--ink);white-space:pre-wrap;overflow-wrap:anywhere;unicode-bidi:plaintext;text-align:start}` +
	`.chattone-others-actions{display:flex;flex-wrap:wrap;gap:4px 12px}` +
	`.chattone-others-note{color:var(--muted)}.chattone-others-error{color:var(--hcm-color-danger)}` +
	`.chatlang-inaccurate{font-weight:600;color:var(--hcm-color-warning)}` +
	`.chatlang-report{margin:4px 0 0;max-inline-size:60ch}.chatlang-report-text{inline-size:100%;box-sizing:border-box;font:inherit;font-size:.875rem;padding:6px 8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);resize:vertical}` +
	`.chatlang-report-status{margin:0;color:var(--muted)}` +
	`.chatlang-notice{display:flex;flex-wrap:wrap;align-items:center;gap:0 10px;margin:2px 0 0;font-size:.75rem;line-height:1.5;color:var(--muted)}` +
	`.chatlang-notice-text{overflow-wrap:anywhere}`
