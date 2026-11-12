package chatui

// Chat3703Styles marks the required Basis field on the admin country form with
// a visible asterisk. The input already carries the required attribute; the
// label text is shared with the other admin fields, so the mark is drawn here.
const Chat3703Styles = `
.chatlangadmin-form label[for="chatmapadmin-basis"]::after{content:" *";color:var(--hcm-color-danger)}
`
