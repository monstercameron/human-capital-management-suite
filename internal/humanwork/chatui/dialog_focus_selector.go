package chatui

// chatFocusDialogSelector finds the open Chat dialog whose focus the page manages:
// focus moves into it when it opens, Tab stays inside it, and focus goes back
// to the control that opened it. A dialog missing from this list opens with the
// cursor wherever it was, so typing goes nowhere.
const chatFocusDialogSelector = ".create-dialog, .browse-dialog, .add-members-dialog, .agent-profile-dialog"
