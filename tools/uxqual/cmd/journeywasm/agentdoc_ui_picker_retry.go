package main

// personaDocumentPickerRetrySelector matches the retry button only. The picker
// root also carries a data-agentdoc-retry attribute (the button's label), so a
// bare attribute selector with closest() matched every click inside the
// picker and turned choosing a document into "search again".
const personaDocumentPickerRetrySelector = "button[data-agentdoc-retry]"
