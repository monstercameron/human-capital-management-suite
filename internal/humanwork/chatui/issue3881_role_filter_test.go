package chatui

import (
	"regexp"
	"strings"
	"testing"
)

func issue3881Page(t *testing.T, locale, memberQuery, roleQuery string) string {
	t.Helper()
	m := chatux021Model()
	m.Locale = locale
	m.Members = append(m.Members, Member{ID: "amina", HomeTenantID: "t", Name: "Amina Okoye"}, Member{ID: "darius", HomeTenantID: "t", Name: "Darius Pell"})
	m.ChannelTeam.Members = []ChannelTeamMember{
		{HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"},
		{HomeTenantID: "t", SubjectID: "amina", Role: "MEMBERSHIP_ROLE_MEMBER"},
		{HomeTenantID: "t", SubjectID: "darius", Role: "MEMBERSHIP_ROLE_MEMBER"},
	}
	m.MemberQuery = memberQuery
	groups := map[string]bool{"manage": true, chatux027Key(chatux027ManageScope(m), "roles"): true}
	return chatux021Details(t, m, handlers{local: localUI{detailGroups: groups, roleQuery: roleQuery}})
}

func issue3881RoleList(t *testing.T, page string) string {
	t.Helper()
	list := regexp.MustCompile(`(?s)<ul class="manage-role-list".*?</ul>`).FindString(page)
	if list == "" {
		t.Fatalf("no role-label list: %s", page)
	}
	return list
}

// Issue 3881: the Members filter and the Role labels filter are separate.
func TestIssue3881_RoleFilterKeepsItsOwnText(t *testing.T) {
	page := issue3881Page(t, "en-US", "Darius", "Amina")
	roleInput := regexp.MustCompile(`<input[^>]*id="role-filter"[^>]*>`).FindString(page)
	if !strings.Contains(roleInput, `data-chat-value="Amina"`) {
		t.Errorf("the role-label filter shows the Members filter's text: %s", roleInput)
	}
	memberInput := regexp.MustCompile(`<input[^>]*id="member-filter"[^>]*>`).FindString(page)
	if !strings.Contains(memberInput, `data-chat-value="Darius"`) {
		t.Errorf("the Members filter lost its own text: %s", memberInput)
	}
	list := issue3881RoleList(t, page)
	if !strings.Contains(list, "Amina Okoye") || strings.Contains(list, "Darius Pell") {
		t.Errorf("the role-label list does not follow its own filter: %s", list)
	}
}

func TestIssue3881_MembersFilterDoesNotNarrowRoleLabels(t *testing.T) {
	page := issue3881Page(t, "en-US", "Darius", "")
	list := issue3881RoleList(t, page)
	for _, name := range []string{"Walt Brennan", "Amina Okoye", "Darius Pell"} {
		if !strings.Contains(list, name) {
			t.Errorf("filtering Members hid %s from the role-label list", name)
		}
	}
	if strings.Contains(page, `id="role-filter"`) && strings.Contains(regexp.MustCompile(`<input[^>]*id="role-filter"[^>]*>`).FindString(page), `Darius`) {
		t.Errorf("the role-label filter picked up the Members filter's text")
	}
}

func TestIssue3881_RoleFilterWithNoMatchSaysSo(t *testing.T) {
	want := map[string]string{
		"en-US": "No members match “Zed”.",
		"de-DE": "Keine Mitglieder passen zu „Zed“.",
		"ar":    "لا يوجد أعضاء يطابقون “Zed”.",
	}
	for locale, text := range want {
		page := issue3881Page(t, locale, "", "Zed")
		body := regexp.MustCompile(`(?s)id="chat-manage-roles".*`).FindString(page)
		empty := regexp.MustCompile(`(?s)<p[^>]*member-empty[^>]*>.*?</p>`).FindString(body)
		if !strings.Contains(empty, text) || !strings.Contains(empty, `role="status"`) {
			t.Errorf("%s: an empty role-label filter does not say so: %q", locale, empty)
		}
	}
}
