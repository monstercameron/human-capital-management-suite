package main

import "strings"

// generalAgentSelection maps the General agent's radio value to the empty
// agent id the task service expects. A radio rendered with an empty value
// attribute reports the browser default "on", which was sent as the id of an
// agent that does not exist, so every General agent question was refused as
// "This agent is not available".
func generalAgentSelection(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "", "on", "general-agent", "<null>", "<undefined>":
		return ""
	}
	return value
}
