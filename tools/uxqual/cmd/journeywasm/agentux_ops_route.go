package main

import "net/url"

const agentOperationsPath = "/workspace/app/admin/agents"

func agentOperationsRoute(path string) bool {
	return path == agentOperationsPath
}

func agentOperationsSelectedTab(rawQuery string) string {
	values, err := url.ParseQuery(rawQuery)
	if err == nil {
		switch values.Get("tab") {
		case "running", "rollout", "move", "announcements":
			return values.Get("tab")
		}
	}
	return "running"
}
