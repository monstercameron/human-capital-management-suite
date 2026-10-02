package productui

// agentUX042ProblemStyles lays out the "no access", "could not load" and
// "does not exist" card that the workspace shell draws in its content area.
// The card's two actions sat in an element with no rule of its own, so the
// buttons touched; its heading took the browser's display size.
const agentUX042ProblemStyles = `
.workspace-page-problem{max-inline-size:44rem;gap:12px}
.workspace-page-problem h1{margin:0;font-size:1.375rem;line-height:1.3;overflow-wrap:anywhere}
.workspace-page-problem p{margin:0;max-inline-size:60ch}
.workspace-page-problem .action-row{display:flex;flex-wrap:wrap;align-items:center;gap:var(--hcm-space-2,.5rem);margin-block-start:4px}
.workspace-page-problem .action-row .button{margin:0}
`
