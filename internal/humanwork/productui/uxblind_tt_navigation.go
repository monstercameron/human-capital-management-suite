package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func navigationCurrentKey(props NavigationSidebarProps) string {
	for _, items := range [][]NavigationItemProps{props.Favorites, props.Items, props.Support} {
		if page := navigationCurrentItemKey(items); page != "" {
			return page
		}
	}
	return ""
}

func navigationCurrentItemKey(items []NavigationItemProps) string {
	for _, item := range items {
		if page := navigationCurrentItemKey(item.Children); page != "" {
			return page
		}
		if item.Active {
			return string(item.Page)
		}
	}
	return ""
}

func useCurrentNavigationScroll(key string) {
	ui.UseEffectOf(func() func() {
		scrollCurrentNavigationItem(key)
		return nil
	}, key)
}
