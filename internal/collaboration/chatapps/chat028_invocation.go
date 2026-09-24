package chatapps

import (
	"math"
	"strconv"
)

// validCommandArguments treats the manifest as the command's input schema.
// Arguments are strings at the callback boundary, so each declared type is
// parsed strictly before any external callback can run.
func validCommandArguments(m Manifest, name string, values map[string]string) bool {
	var selected *Command
	for i := range m.Commands {
		if m.Commands[i].Name == name {
			if selected != nil {
				return false
			}
			selected = &m.Commands[i]
		}
	}
	if selected == nil {
		return false
	}
	arguments := make(map[string]Argument, len(selected.Arguments))
	for _, argument := range selected.Arguments {
		if argument.Name == "" || (argument.Type != "string" && argument.Type != "boolean" && argument.Type != "integer" && argument.Type != "number") {
			return false
		}
		if _, duplicate := arguments[argument.Name]; duplicate {
			return false
		}
		arguments[argument.Name] = argument
	}
	for name, value := range values {
		argument, ok := arguments[name]
		if !ok || (argument.Required && value == "") {
			return false
		}
		switch argument.Type {
		case "boolean":
			if value != "true" && value != "false" {
				return false
			}
		case "integer":
			if _, err := strconv.ParseInt(value, 10, 64); err != nil {
				return false
			}
		case "number":
			n, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return false
			}
		}
	}
	for name, argument := range arguments {
		if argument.Required {
			if value, ok := values[name]; !ok || value == "" {
				return false
			}
		}
	}
	return true
}
