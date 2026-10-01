package productui

import (
	"fmt"
	"testing"
)

func TestDebugWeb039(t *testing.T) {
	view := ApplyNavigationProjection(testView(PageHome), web039NavigationProjection())
	view.MenuQuery = ""
	fmt.Printf("nav len=%d support len=%d\n", len(view.Navigation), len(view.NavigationSupport))
	for _, n := range view.Navigation {
		fmt.Printf("item %+v\n", n)
	}
	favorites, items := projectNavigation(view)
	fmt.Println("favorites", favorites)
	fmt.Println("items", items)
}
