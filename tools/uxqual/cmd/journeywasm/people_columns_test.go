package main

import "testing"

func TestPeopleColumnsRefreshOnlyDirectory(t *testing.T) {
	if !peopleDirectoryOnlyRouteChange("/workspace/app/people?sort=company&columns=name,company", "/workspace/app/people?sort=name&dir=asc&columns=name,job_code") {
		t.Fatal("column customization must not reset page focus or replace the whole page")
	}
}
