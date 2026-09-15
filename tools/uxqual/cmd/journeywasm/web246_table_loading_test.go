package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestPeopleDirectorySortUsesAuthoritativeRoute(t *testing.T) {
	var got []string
	navigatePeopleDirectoryChange(func(href string) { got = append(got, href) }, productui.PeopleDirectoryChange{
		Href: "/workspace/app/people?sort=role&dir=desc", Sort: "role", Descending: true,
	})
	if len(got) != 1 || got[0] != "/workspace/app/people?sort=role&dir=desc" {
		t.Fatalf("directory sort navigations = %v, want one authoritative route", got)
	}
	navigatePeopleDirectoryChange(func(href string) { got = append(got, href) }, productui.PeopleDirectoryChange{})
	navigatePeopleDirectoryChange(nil, productui.PeopleDirectoryChange{Href: "/workspace/app/people?page=2"})
	if len(got) != 1 {
		t.Fatalf("invalid directory changes navigated: %v", got)
	}
}
