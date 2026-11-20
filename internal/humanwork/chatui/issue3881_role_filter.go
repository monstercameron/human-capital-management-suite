package chatui

// Issue 3881: the Role labels filter keeps its own text. It used to read and
// write the Members filter's query, so typing in either box changed both lists.
// Its input is handled by the details panel's input handler under this action,
// and the text lives in localUI.roleQuery.
const issue3881RoleFilterAction = "role-filter-input"
