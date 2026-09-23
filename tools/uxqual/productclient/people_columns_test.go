package productclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UICOLS_001_Preferences(t *testing.T) {
	table := &journeyv1.TablePreferences{Filters: map[string]string{"columns": "name,company,cost_center"}}
	for _, query := range []string{"", "columns=name,job_code&sort=job_code", "columns="} {
		state, err := ParseState("/workspace/app/people", query)
		if err != nil {
			t.Fatal(err)
		}
		applyTableDefaults(&state.Request, state.Provided, table, false)
		view := productui.ApplyRequest(productui.View{Page: productui.PagePeople}, state.Request)
		want := "name,company,cost_center"
		if query == "columns=name,job_code&sort=job_code" {
			want = "name,job_code"
		}
		if query == "columns=" {
			want = "name,role,team,manager,location"
		}
		if view.PeopleColumns != want {
			t.Fatalf("%s columns=%s want=%s", query, view.PeopleColumns, want)
		}
	}
}
