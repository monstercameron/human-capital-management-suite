package chatui

// Chat3703Styles marks the required Basis field on the admin country form with
// a visible asterisk. The input already carries the required attribute; the
// label text is shared with the other admin fields, so the mark is drawn here.
//
// The Chat settings page sits outside the chat workspace, so the chat sheet
// (which is scoped to the workspace) never reaches it: the page's own sheet,
// TranslationAdminPageStyles, carries this one too. The same goes for the red of
// the country field's refusal message.
const Chat3703Styles = `
.chatlangadmin-form label[for="chatmapadmin-basis"]::after{content:" *";color:var(--hcm-color-danger)}
.chatmapadmin .field-hint.field-error{color:var(--hcm-color-danger)}
`
