package productui

// CHATBUG-080. At tablet width the bar above every page showed a wide empty
// bordered box holding only the language code, with back, forward, search and
// go-to-page on a second row. The bar is a grid of five children: the brand
// cluster, the navigation tools (history, search, go to a page), the language
// menu, notifications and the account. Its tablet rule gave the tools a row of
// their own and left the language menu to be placed into the one flexible
// column, where it stretched.
//
// From 700 to 1000 px the bar is one row. Each child has its own column; the
// tools take the one flexible column, where search is the icon it already is
// at this width, and the language menu, notifications and account are as wide
// as what they hold.

// chatbug080TopbarColumns is the grid of that row: every column but the tools'
// is as wide as its content.
const chatbug080TopbarColumns = "auto minmax(0,1fr) auto auto auto"

func chatbug080TopbarStylesheet() string {
	return `@media(min-width:700px) and (max-width:1000px){` +
		`.app-shell .topbar,.app-shell.nav-collapsed .topbar{grid-template-columns:` + chatbug080TopbarColumns + `;grid-template-rows:auto;column-gap:8px;row-gap:0;padding-inline:16px;align-items:center}` +
		`.app-shell .topbar>.brand-cluster{grid-column:1;grid-row:1}` +
		`.app-shell .topbar>.header-navigation-tools{grid-column:2;grid-row:1;min-width:0;padding:0;flex-wrap:nowrap}` +
		`.app-shell .topbar>.locale-menu{grid-column:3;grid-row:1;flex:none;width:auto;min-width:44px}` +
		`.app-shell .topbar>:nth-child(4){grid-column:4;grid-row:1}` +
		`.app-shell .topbar>:nth-child(5){grid-column:5;grid-row:1}` +
		`}`
}
