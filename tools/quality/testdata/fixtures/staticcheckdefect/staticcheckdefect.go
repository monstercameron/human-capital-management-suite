// Package staticcheckdefect is a TOOL-011 fixture containing a self-comparison
// which staticcheck's SA4000 reports.
package staticcheckdefect

func AlwaysTrue(value int) bool {
	return value == value
}
