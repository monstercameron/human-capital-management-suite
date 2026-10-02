package productui

// agentUX039PickerStyles fixes what made the document picker read as broken.
// Its "Try again" button carries the hidden attribute until a search fails, but
// the button rule's display outranked the browser's rule for hidden, so the
// button showed beside the idle sentence as if loading had failed. Its search
// field was filled with the page's canvas grey, the fill of a disabled control.
// A hidden element on an agent page is not displayed, whatever its class; an
// editable field is drawn on the surface colour and only a disabled one is grey.
const agentUX039PickerStyles = `
.agent-page-frame [hidden]{display:none!important}
.persona-admin-page .agentdoc-picker-combobox input,.persona-admin-page .agentdoc-picker-row select{background:var(--hcm-color-surface)}
.agent-announcements input:not([type=radio]):not([type=checkbox]),.agent-announcements select,.agent-announcements textarea{background:var(--hcm-color-surface)}
.agent-page-frame .agentdoc-picker-combobox input:disabled{background:var(--hcm-color-canvas);color:var(--hcm-color-text-muted);cursor:not-allowed}
.agent-page-frame .agentdoc-picker-state p{margin:0;max-inline-size:72ch}
.agent-page-frame .agentdoc-picker-error{display:flex;align-items:center;flex-wrap:wrap;gap:.5rem}
`
