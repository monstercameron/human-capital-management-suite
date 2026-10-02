package chatui

// AgentUX075Styles draws the formatted parts of an agent's answer: tables that
// scroll inside the message instead of widening the page, struck-through text,
// and the small numbered marker of a citation, which stays in its sentence.
const AgentUX075Styles = `.message-body .md-table-wrap,.agent-reply-answer .md-table-wrap{max-width:100%;overflow-x:auto;margin-block:8px}` +
	`.md-table{border-collapse:collapse;min-width:min(100%,20rem);font-size:.9375rem}` +
	`.md-table th,.md-table td{border:1px solid var(--hcm-color-border);padding:6px 10px;text-align:start;vertical-align:top}` +
	`.md-table th{background:var(--hcm-color-brand-soft);font-weight:650}` +
	`.md-table .md-align-end{text-align:end}.md-table .md-align-center{text-align:center}` +
	`.message-body del,.agent-reply-answer del{opacity:.7}` +
	`.agent-cite{display:inline;margin-inline-start:2px;font-size:.75em;line-height:1;vertical-align:super;text-decoration:none;font-weight:650;white-space:nowrap}` +
	`.agent-cite:hover,.agent-cite:focus-visible{text-decoration:underline}`
