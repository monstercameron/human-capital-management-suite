package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// salaryComparisonPage renders only authorized compensation and range facts.
func salaryComparisonPage(view View) ui.Node {
	return managerCompensationPage(view, "salary_comparison", "")
}
