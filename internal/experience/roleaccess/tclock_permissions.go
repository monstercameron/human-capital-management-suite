package roleaccess

const (
	// PageTimeClock is the stable role-access identity for the governed clock page.
	PageTimeClock = "time-clock"
	// These page IDs are the registered time-clock wave surfaces.
	PageTimecard     = "time-timecard"
	PageCrewSchedule = "time-schedule"
	PageClockDevices = "time-devices"
	// FeatureTimeClockStatus scopes read-only current status on the clock page.
	FeatureTimeClockStatus = "clock_status"
	// FeatureTimeClockActions scopes clock-in and clock-out actions.
	FeatureTimeClockActions = "clock_actions"
	// FeatureTimeClockKiosk scopes kiosk enrollment and launch actions.
	FeatureTimeClockKiosk = "kiosk_access"
)

// DefaultTimeClockPagePermissions returns the narrow page grants for the
// governed clock surface. The caller explicitly appends these to the durable
// default catalogue when the page is admitted; this helper does not admit a
// route or grant finance access.
func DefaultTimeClockPagePermissions() []PagePermission {
	roles := []string{"hcm_admin", "manager", "hr_partner", "payroll_manager", "worker_self"}
	result := make([]PagePermission, 0, len(roles))
	for _, role := range roles {
		result = append(result, PagePermission{RoleID: role, PageID: PageTimeClock, View: true, Create: true, Update: true})
	}
	return result
}

// DefaultTimePagePermissions returns the page grants for the timecard,
// schedule and device-fleet routes. It is kept separate from the clock grant
// helper so the clock's existing narrow contract remains stable.
func DefaultTimePagePermissions() []PagePermission {
	result := make([]PagePermission, 0, 15)
	for _, role := range []string{"hcm_admin", "manager", "hr_partner", "hiring_manager", "payroll_manager", "worker_self"} {
		result = append(result, PagePermission{RoleID: role, PageID: PageTimecard, View: true, Create: true, Update: true})
	}
	for _, role := range []string{"hcm_admin", "manager", "hr_partner", "hiring_manager", "payroll_manager"} {
		result = append(result, PagePermission{RoleID: role, PageID: PageCrewSchedule, View: true, Create: true, Update: true})
	}
	for _, role := range []string{"hcm_admin", "comp_admin"} {
		result = append(result, PagePermission{RoleID: role, PageID: PageClockDevices, View: true, Create: true, Update: true})
	}
	return result
}

// DefaultTimeClockFeaturePermissions returns feature grants whose ceiling is
// intentionally narrower than the page grant. Kiosk controls are withheld
// from worker_self and finance_partner entirely.
func DefaultTimeClockFeaturePermissions() []FeaturePermission {
	statusRoles := []string{"hcm_admin", "manager", "hr_partner", "payroll_manager", "worker_self"}
	actionRoles := statusRoles
	kioskRoles := []string{"hcm_admin", "manager", "hr_partner", "payroll_manager"}
	result := make([]FeaturePermission, 0, len(statusRoles)+len(actionRoles)+len(kioskRoles))
	for _, role := range statusRoles {
		result = append(result, FeaturePermission{RoleID: role, PageID: PageTimeClock, FeatureID: FeatureTimeClockStatus, View: true})
	}
	for _, role := range actionRoles {
		result = append(result, FeaturePermission{RoleID: role, PageID: PageTimeClock, FeatureID: FeatureTimeClockActions, View: true, Create: true, Update: true})
	}
	for _, role := range kioskRoles {
		result = append(result, FeaturePermission{RoleID: role, PageID: PageTimeClock, FeatureID: FeatureTimeClockKiosk, View: true, Create: true, Update: true})
	}
	return result
}

// ConstrainDefaultTimeClockFeaturePermissions replaces the generic feature
// expansion for the clock page with the narrower clock policy. The generic
// generator derives actions from page ceilings and would otherwise give
// worker_self the kiosk feature. If the page is not present, the input is
// returned unchanged so an unpublished page is never admitted by this helper.
func ConstrainDefaultTimeClockFeaturePermissions(generated []FeaturePermission) []FeaturePermission {
	strict := make(map[string]FeaturePermission)
	for _, permission := range DefaultTimeClockFeaturePermissions() {
		strict[permission.RoleID+"\x00"+permission.FeatureID] = permission
	}
	for _, permission := range DefaultTimeClockPagePermissions() {
		strict[permission.RoleID+"\x00content"] = FeaturePermission{RoleID: permission.RoleID, PageID: PageTimeClock, FeatureID: "content", View: true}
	}
	result := make([]FeaturePermission, 0, len(generated))
	for _, permission := range generated {
		if permission.PageID != PageTimeClock {
			result = append(result, permission)
			continue
		}
		allowed, ok := strict[permission.RoleID+"\x00"+permission.FeatureID]
		if !ok {
			continue
		}
		// Intersect with the generated page/feature ceiling. This helper
		// narrows unsafe generic grants without creating grants absent from
		// the caller's catalog.
		permission.View = permission.View && allowed.View
		permission.Create = permission.Create && allowed.Create
		permission.Update = permission.Update && allowed.Update
		permission.Delete = permission.Delete && allowed.Delete
		result = append(result, permission)
	}
	return result
}
