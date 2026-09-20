package storeprivacy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var createTablePattern = regexp.MustCompile(`(?im)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(\w+)\.)?(\w+)\s*\(`)

var alterAddColumnPattern = regexp.MustCompile(`(?im)^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(?:(\w+)\.)?(\w+)\s+ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(\w+)\s+(.*?)\s*(?:,?\s*(?:--[^\n]*)?)?$`)

// constraintKeywords open DDL lines that are not column declarations. The
// logical operators matter because multi-line CHECK constraints continue
// with AND/OR fragments (for example "OR authority_ref IS NOT NULL"),
// which would otherwise parse as columns named "or" or "and".
var constraintKeywords = []string{
	"constraint", "primary", "foreign", "check", "unique", "exclude",
	"like", "partition", "trigger", "and", "or", "not",
}

// predicateKeywords are SQL predicate words that can never be a column
// type. A continuation fragment such as "claim_expires_at IS NULL" has an
// identifier in column position but a predicate in type position; without
// this rule it would parse as a column and even false-flag ("claim").
var predicateKeywords = []string{
	"is", "in", "like", "ilike", "between", "null", "not",
}

// columnLinePattern matches the start of a single-line column declaration:
// an indented name followed by a type. Multi-word types (character varying,
// timestamp with time zone, double precision) are normalized by baseType.
var columnLinePattern = regexp.MustCompile(`^\s+([a-zA-Z][a-zA-Z0-9_]*)\s+([a-zA-Z][^\s,;]*)(.*)$`)

// baseType normalizes a DDL type expression to its first type word, folding
// the multi-word spellings migrations use.
func baseType(rest string) string {
	lower := strings.ToLower(strings.TrimSpace(rest))
	switch {
	case strings.HasPrefix(lower, "character varying"):
		return "varchar"
	case strings.HasPrefix(lower, "timestamp with time zone"):
		return "timestamptz"
	case strings.HasPrefix(lower, "timestamp without time zone"):
		return "timestamp"
	case strings.HasPrefix(lower, "time with time zone"):
		return "timetz"
	case strings.HasPrefix(lower, "double precision"):
		return "float8"
	}
	word := lower
	if i := strings.IndexAny(word, " (,"); i >= 0 {
		word = word[:i]
	}
	return word
}

// isPayloadCapableType reports whether a column type is an opaque envelope
// whose bytes the checker cannot see inside: personal content there needs a
// reviewed declaration or a vault reference.
func isPayloadCapableType(base string) bool {
	switch base {
	case "bytea", "json", "jsonb":
		return true
	}
	return false
}

// scanColumns returns every DDL column declared for the given PERMANENT
// tables (lower-cased names) across all migrations/*.sql files. Companion
// schemas (qualified names) and physical partitions (PARTITION OF, which
// never matches the open-paren pattern) are excluded, matching STORE-001's
// live-schema rule. Columns added later by ALTER TABLE ADD COLUMN are
// included so the inventory cannot miss a field the CREATE TABLE text
// predates.
func scanColumns(migrationsDir string, permanent map[string]bool) ([]Column, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("storeprivacy: read migrations: %w", err)
	}
	seen := map[string]Column{}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return nil, fmt.Errorf("storeprivacy: read migration %s: %w", name, err)
		}
		scanFileColumns(string(data), permanent, seen)
	}
	out := make([]Column, 0, len(seen))
	for _, col := range seen {
		out = append(out, col)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func scanFileColumns(data string, permanent map[string]bool, seen map[string]Column) {
	var current string
	depth := 0
	for _, raw := range strings.Split(data, "\n") {
		line := stripLineComment(raw)
		if m := createTablePattern.FindStringSubmatch(line); m != nil {
			schema, table := m[1], strings.ToLower(m[2])
			if schema != "" && !isPublicSchema(schema) {
				current = ""
				depth = parenDepth(line)
				continue
			}
			if !permanent[table] {
				current = ""
			} else {
				current = table
			}
			depth = parenDepth(line)
			continue
		}
		if m := alterAddColumnPattern.FindStringSubmatch(line); m != nil {
			schema, table, col, typ := m[1], strings.ToLower(m[2]), strings.ToLower(m[3]), baseType(m[4])
			if (schema == "" || isPublicSchema(schema)) && permanent[table] && col != "" && typ != "" {
				key := table + "." + col
				if _, ok := seen[key]; !ok {
					seen[key] = Column{Table: table, Name: col, Type: typ}
				}
			}
			continue
		}
		if current == "" {
			continue
		}
		depth += strings.Count(line, "(") - strings.Count(line, ")")
		if depth <= 0 {
			current = ""
			continue
		}
		name, typ, ok := parseColumnLine(line)
		if !ok {
			continue
		}
		key := current + "." + name
		if _, dup := seen[key]; !dup {
			seen[key] = Column{Table: current, Name: name, Type: typ}
		}
	}
}

// parseColumnLine extracts a column name and normalized base type, or false
// for constraint lines and anything that is not a declaration.
func parseColumnLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", "", false
	}
	first := strings.ToLower(strings.Fields(trimmed)[0])
	for _, kw := range constraintKeywords {
		if first == kw {
			return "", "", false
		}
	}
	m := columnLinePattern.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	typ := baseType(m[2] + m[3])
	for _, kw := range predicateKeywords {
		if typ == kw {
			return "", "", false
		}
	}
	return strings.ToLower(m[1]), typ, true
}

func stripLineComment(line string) string {
	if i := strings.Index(line, "--"); i >= 0 {
		return line[:i]
	}
	return line
}

func parenDepth(line string) int {
	return strings.Count(line, "(") - strings.Count(line, ")")
}

func isPublicSchema(schema string) bool {
	return strings.ToLower(schema) == "public"
}
