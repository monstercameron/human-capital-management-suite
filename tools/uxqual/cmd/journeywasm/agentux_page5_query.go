package main

import (
	"net/url"
	"strings"
)

// selectedAgentTaskID returns only a non-empty task selector. A missing query
// key is not a value; this keeps URLSearchParams/null differences out of the
// page state used by both server-first and hydrated renders.
func selectedAgentTaskID(rawQuery string) string {
	values, err := url.ParseQuery(strings.TrimPrefix(rawQuery, "?"))
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(values.Get("task"))
	// A browser API that has no value for a key answers null, which a careless
	// read turns into text. Those words never name a task.
	switch strings.ToLower(id) {
	case "<null>", "null", "undefined", "<undefined>":
		return ""
	}
	return id
}

func preferredAgentTaskFilter(stored, selected string, counts map[string]int) string {
	if selected != "" && counts[selected] > 0 {
		return selected
	}
	if counts[stored] > 0 {
		return stored
	}
	for _, category := range []string{"active", "completed", "failed"} {
		if counts[category] > 0 {
			return category
		}
	}
	return "active"
}
