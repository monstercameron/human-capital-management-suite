package roleaccess

import (
	"errors"
	"reflect"
	"testing"
)

func TestFeaturePermissionNormalizesAndValidatesStableIDs(t *testing.T) {
	got := NormalizeFeaturePermission(FeaturePermission{
		RoleID:    " MANAGER ",
		PageID:    " SETTINGS ",
		FeatureID: " Profile_Summary ",
	})
	want := FeaturePermission{RoleID: "manager", PageID: "settings", FeatureID: "profile_summary"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized feature permission = %#v, want %#v", got, want)
	}

	tests := []FeaturePermission{
		{RoleID: "bad role", PageID: "settings", FeatureID: "profile_summary", View: true},
		{RoleID: "manager", PageID: "bad page", FeatureID: "profile_summary", View: true},
		{RoleID: "manager", PageID: "settings", FeatureID: "bad-feature", View: true},
		{RoleID: "manager", PageID: "settings", FeatureID: "profile_summary", Create: true},
		{RoleID: "manager", PageID: "settings", FeatureID: "profile_summary", Update: true},
		{RoleID: "manager", PageID: "settings", FeatureID: "profile_summary", Delete: true},
	}
	for _, permission := range tests {
		if !errors.Is(ValidateFeaturePermission(permission), ErrInvalid) {
			t.Errorf("invalid feature permission accepted: %#v", permission)
		}
	}
	valid := FeaturePermission{RoleID: "manager", PageID: "settings", FeatureID: "profile_summary", View: true, Update: true}
	if err := ValidateFeaturePermission(valid); err != nil {
		t.Fatalf("valid feature permission rejected: %v", err)
	}
}

func TestEffectiveFeaturePermissionsAreAdditivePerPageAndFeature(t *testing.T) {
	snapshot := Snapshot{FeaturePermissions: []FeaturePermission{
		{RoleID: "manager", PageID: "settings", FeatureID: "profile_summary", View: true},
		{RoleID: "manager", PageID: "settings", FeatureID: "profile_summary", View: true, Update: true},
		{RoleID: "report_author", PageID: "settings", FeatureID: "profile_summary", View: true, Create: true},
		{RoleID: "report_author", PageID: "settings", FeatureID: "access_log", View: true, Delete: true},
		{RoleID: "other_role", PageID: "settings", FeatureID: "profile_summary", View: true, Delete: true},
	}}
	got := EffectiveFeaturePermissions(snapshot, []string{"MANAGER", "report_author", "manager"})
	want := []FeaturePermission{
		{PageID: "settings", FeatureID: "access_log", View: true, Delete: true},
		{PageID: "settings", FeatureID: "profile_summary", View: true, Create: true, Update: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("effective feature permissions = %#v, want %#v", got, want)
	}
	if CanFeatureAction([]PagePermission{{PageID: "settings", View: true, Create: true, Update: true}}, got, "settings", "profile_summary", ActionDelete) {
		t.Fatal("delete grant from an unassigned role leaked into effective feature permissions")
	}
}

func TestCanFeatureActionRequiresContainingPageAction(t *testing.T) {
	pagePermissions := []PagePermission{{RoleID: "manager", PageID: "settings", View: true, Update: false}}
	featurePermissions := []FeaturePermission{{PageID: "settings", FeatureID: "profile_summary", View: true, Update: true}}
	if !CanFeatureAction(pagePermissions, featurePermissions, "settings", "profile_summary", ActionView) {
		t.Fatal("view was denied despite page and feature view grants")
	}
	if CanFeatureAction(pagePermissions, featurePermissions, "settings", "profile_summary", ActionUpdate) {
		t.Fatal("feature update bypassed containing page update denial")
	}

	pagePermissions[0].Update = true
	if !CanFeatureAction(pagePermissions, featurePermissions, "settings", "profile_summary", ActionUpdate) {
		t.Fatal("update was denied despite page and feature update grants")
	}
	if CanFeatureAction(pagePermissions, featurePermissions, "settings", "missing_feature", ActionView) {
		t.Fatal("unknown feature was allowed")
	}
}

func TestCanFeatureActionDefaultsToDeny(t *testing.T) {
	page := []PagePermission{{PageID: "settings", View: true, Create: true, Update: true, Delete: true}}
	feature := []FeaturePermission{{PageID: "other-page", FeatureID: "profile_summary", View: true, Create: true, Update: true, Delete: true}}
	for _, action := range []string{ActionView, ActionCreate, ActionUpdate, ActionDelete, "unknown"} {
		if CanFeatureAction(page, feature, "settings", "profile_summary", action) {
			t.Errorf("default-deny failed for action %q", action)
		}
	}
}

func TestDefaultFeaturePermissionsCannotExceedPageOrFeatureCeilings(t *testing.T) {
	definitions := []FeatureDefinition{
		{PageID: "people", FeatureID: "directory", View: true},
		{PageID: "people", FeatureID: "workflow_actions", View: true, Create: true, Update: true},
		{PageID: "ignored", FeatureID: "bad-feature", View: true},
	}
	pages := []PagePermission{{RoleID: "manager", PageID: "people", View: true, Create: true}}
	got := DefaultFeaturePermissions(definitions, pages)
	want := []FeaturePermission{
		{RoleID: "manager", PageID: "people", FeatureID: "directory", View: true},
		{RoleID: "manager", PageID: "people", FeatureID: "workflow_actions", View: true, Create: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default feature permissions = %#v, want %#v", got, want)
	}
	if err := ValidateFeatureDefinition(FeatureDefinition{PageID: "people", FeatureID: "directory"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-viewable feature definition accepted: %v", err)
	}
}
