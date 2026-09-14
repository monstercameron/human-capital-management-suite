package productui

import (
	"reflect"
	"testing"
)

func TestTodo_UIPOLISH_004_Regression_MergedUX(t *testing.T) {
	var calls []string
	view := testView(PagePeople)
	view.Navigate = func(href string) { calls = append(calls, "navigate:"+href) }
	props := navigationDrawerSidebarProps(view, true, nil, func() { calls = append(calls, "close") })
	if len(props.Items) == 0 || props.Items[0].OnSelect == nil {
		t.Fatal("drawer navigation items do not receive a close action")
	}
	item := props.Items[0]
	navigationSelectNavigate(item.Navigate, item.OnSelect)(item.Href)
	if want := []string{"close", "navigate:" + item.Href}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("selection calls = %v, want %v", calls, want)
	}

	filtered := props.Search("history")
	if len(filtered.Items) == 0 || len(filtered.Items[0].Children) == 0 || filtered.Items[0].Children[0].OnSelect == nil {
		t.Fatal("filtered submenu lost its drawer close action")
	}
	if got := navigationSelectNavigate(nil, func() { t.Fatal("close called without a navigation destination") }); got != nil {
		t.Fatal("SSR navigation unexpectedly intercepted a native link")
	}
}
