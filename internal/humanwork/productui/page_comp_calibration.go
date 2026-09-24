package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// compCalibrationPage renders scoped calibration rows and submits governed intents.
func compCalibrationPage(view View) ui.Node {
	return managerCompensationPage(view, "comp_calibration", "calibration")
}
