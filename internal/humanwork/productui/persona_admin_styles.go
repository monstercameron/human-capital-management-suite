package productui

// personaAdminStylesheet gives the persona catalog and editors a scoped,
// responsive layout. Every selector stays under the persona administration
// page so workspace controls keep their existing visual contract.
func personaAdminStylesheet() string {
	return `.persona-admin-page{display:grid;gap:1.5rem;min-width:0;width:100%;padding-block:1.5rem;padding-inline:clamp(1rem,3vw,2rem)}
.persona-admin-page .persona-admin-hero,.persona-admin-page .persona-admin-catalog,.persona-admin-page .persona-admin-editor,.persona-admin-page .persona-admin-preview{min-width:0}
.persona-admin-page .persona-admin-catalog,.persona-admin-page .persona-admin-editor,.persona-admin-page .persona-admin-preview{display:grid;gap:1rem}
.persona-admin-page .persona-admin-catalog{order:2}
.persona-admin-page .persona-admin-editor{order:1;padding:clamp(1rem,2.5vw,1.5rem);border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control,var(--radius));background:var(--surface)}
.persona-admin-page .persona-admin-preview{order:3}
.persona-admin-page .persona-admin-cards{display:grid;gap:1rem;min-width:0}
.persona-admin-page .persona-admin-card{display:grid;gap:1rem;min-width:0;padding:clamp(1rem,2.5vw,1.5rem);overflow-wrap:anywhere}
.persona-admin-page .persona-admin-card-heading,.persona-admin-page .persona-admin-actions{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.75rem}
.persona-admin-page .persona-admin-actions{justify-content:flex-start}
.persona-admin-page .persona-admin-card h3,.persona-admin-page .persona-admin-card h4,.persona-admin-page .persona-admin-card p,.persona-admin-page .persona-admin-card ul{margin:0}
.persona-admin-page .persona-admin-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.75rem;min-width:0}
.persona-admin-page .persona-admin-fact{display:grid;gap:.25rem;min-width:0;overflow-wrap:anywhere}
.persona-admin-page .persona-admin-review{display:grid;gap:.5rem;min-width:0}
.persona-admin-page .persona-admin-editor h2,.persona-admin-page .persona-admin-editor p{margin:0}
.persona-admin-page .persona-admin-editor-form,.persona-admin-page .persona-admin-version-editor{display:grid;gap:1rem;min-width:0}
.persona-admin-page .persona-admin-editor-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:1rem;min-width:0}
.persona-admin-page .persona-admin-editor-field{display:grid;align-content:start;gap:.45rem;min-width:0}
.persona-admin-page .persona-admin-editor-field label{font-weight:600;line-height:1.4;overflow-wrap:anywhere}
.persona-admin-page .persona-admin-editor-field input,.persona-admin-page .persona-admin-editor-field select,.persona-admin-page .persona-admin-editor-field textarea{display:block;box-sizing:border-box;width:100%;max-width:100%;min-width:0;min-height:2.75rem;padding:.65rem .75rem;border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control,var(--radius));background:var(--canvas);color:var(--ink);font:inherit;line-height:1.4}
.persona-admin-page .persona-admin-editor-field textarea{min-height:7rem;resize:vertical;white-space:pre-wrap}
.persona-admin-page .persona-admin-editor-field input[readonly],.persona-admin-page .persona-admin-editor-field textarea[readonly]{background:var(--surface-subtle,var(--canvas));color:var(--muted)}
.persona-admin-page .persona-admin-channel-choices{display:flex;flex-wrap:wrap;align-items:center;gap:.75rem 1.25rem;min-width:0;margin:0;padding:.75rem 1rem;border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control,var(--radius))}
.persona-admin-page .persona-admin-channel-choices legend{padding-inline:.25rem;font-weight:600}
.persona-admin-page .persona-admin-channel{display:inline-flex;align-items:center;gap:.5rem;min-height:2.75rem;max-width:100%;overflow-wrap:anywhere}
.persona-admin-page .persona-admin-editor-form .button,.persona-admin-page .persona-admin-version-editor .button{justify-self:start;min-height:2.75rem;max-width:100%;white-space:normal}
.persona-admin-page .persona-admin-version-editor{margin-block-start:1rem;padding-block-start:1rem;border-block-start:1px solid var(--control-border,var(--line))}
.persona-admin-page .persona-admin-version-fields{display:grid;gap:1rem;border:0;padding:0;margin:0;min-width:0}
.persona-admin-page .persona-admin-version-editor .persona-admin-channel-choices{margin-block-end:.25rem}
.persona-admin-page .persona-admin-preview-controls{display:grid;grid-template-columns:max-content minmax(0,1fr) max-content minmax(0,1fr);align-items:center;gap:.5rem .75rem;min-width:0}
.persona-admin-page .persona-admin-preview-controls label{font-weight:600;line-height:1.4;overflow-wrap:anywhere}
.persona-admin-page .persona-admin-preview-controls select{box-sizing:border-box;width:100%;min-width:0;min-height:2.75rem;padding:.65rem .75rem;border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control,var(--radius));background:var(--canvas);color:var(--ink);font:inherit;line-height:1.4}
.persona-admin-page .persona-admin-preview-summary{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:.75rem;min-width:0}
.persona-admin-page .persona-admin-preview-summary .persona-admin-fact{min-width:0;padding:.75rem;border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control,var(--radius));background:var(--surface-subtle,var(--canvas))}
.persona-admin-page .persona-admin-preview-summary .persona-admin-fact small{display:block;line-height:1.35;overflow-wrap:anywhere}
.persona-admin-page .persona-admin-preview-summary .persona-admin-fact strong{display:block;margin-block-start:.25rem;overflow-wrap:anywhere;word-break:break-word}
@media(max-width:48rem){.persona-admin-page{gap:1rem;padding-block:1rem;padding-inline:1rem}.persona-admin-page .persona-admin-editor-grid{grid-template-columns:minmax(0,1fr)}.persona-admin-page .persona-admin-editor{padding:1rem}}
@media(max-width:36rem){.persona-admin-page .persona-admin-preview-controls{grid-template-columns:minmax(0,1fr);gap:.4rem}.persona-admin-page .persona-admin-preview-controls select{margin-block-end:.35rem}.persona-admin-page .persona-admin-preview-summary,.persona-admin-page .persona-admin-facts{grid-template-columns:minmax(0,1fr)}}
@media(max-width:22rem){.persona-admin-page .persona-admin-editor-form .button,.persona-admin-page .persona-admin-version-editor .button{width:100%}}`
}
