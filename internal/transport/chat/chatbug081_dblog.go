package chat

import (
	"errors"
	"log/slog"
	"reflect"
	"regexp"
)

// CHATBUG-081. A write the database refused was logged as
// error_type=DATABASE_FAILURE and nothing more, so finding that a trigger
// function had no DELETE privilege on a table it writes took a database
// session. A refused database call now writes one line naming what the
// database named: its SQLSTATE and the schema, table, column, constraint and
// function involved.
//
// The line never carries a row value. PostgreSQL puts values in an error's
// detail, hint and statement text and often in its message, so none of those
// is logged. The object names come from the error's own name fields, and from
// its message and context only through patterns that match an identifier in a
// sentence PostgreSQL writes about an object ("permission denied for table x",
// "PL/pgSQL function f() line 3"), and only for the conditions listed here.

// databaseFailure is what the log line says about one refused database call.
type databaseFailure struct {
	SQLState, Schema, Table, Column, Constraint, Function string
}

// The messages PostgreSQL writes for these conditions name an object and hold
// no value: 42501 insufficient privilege, 42P01 undefined table, 42703
// undefined column, 42883 undefined function.
var (
	databaseObjectStates = map[string]bool{"42501": true, "42P01": true, "42703": true, "42883": true}
	databaseDeniedObject = regexp.MustCompile(`^permission denied for (?:table|view|materialized view|sequence|relation) ("?)([A-Za-z_][A-Za-z0-9_$.]*)("?)$`)
	databaseMissingTable = regexp.MustCompile(`^relation "([A-Za-z_][A-Za-z0-9_$.]*)" does not exist$`)
	databaseFunctionName = regexp.MustCompile(`PL/pgSQL function ([A-Za-z_][A-Za-z0-9_$.]*)\(`)
	databaseIdentifier   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$.]*$`)
)

// databaseFailureOf describes err when a database error is its cause. The
// transport may not import the database driver, so the driver's error is read
// through the SQLState method every driver error here has and through the
// string fields PostgreSQL's error carries, by name; a field the error does not
// have is empty.
func databaseFailureOf(err error) (databaseFailure, bool) {
	var state interface{ SQLState() string }
	if !errors.As(err, &state) || state.SQLState() == "" {
		return databaseFailure{}, false
	}
	field := func(name string) string {
		value := reflect.Indirect(reflect.ValueOf(state))
		if value.Kind() != reflect.Struct {
			return ""
		}
		found := value.FieldByName(name)
		if !found.IsValid() || found.Kind() != reflect.String {
			return ""
		}
		return found.String()
	}
	// A name field is an identifier or it is not logged at all.
	name := func(value string) string {
		if len(value) > 128 || !databaseIdentifier.MatchString(value) {
			return ""
		}
		return value
	}
	out := databaseFailure{
		SQLState: state.SQLState(), Schema: name(field("SchemaName")), Table: name(field("TableName")),
		Column: name(field("ColumnName")), Constraint: name(field("ConstraintName")),
	}
	if out.Table == "" && databaseObjectStates[out.SQLState] {
		message := field("Message")
		if match := databaseDeniedObject.FindStringSubmatch(message); match != nil && match[1] == match[3] {
			out.Table = match[2]
		} else if match := databaseMissingTable.FindStringSubmatch(message); match != nil {
			out.Table = match[1]
		}
	}
	// The context of an error raised inside a trigger names the function that
	// ran the statement, which is where a missing privilege has to be granted.
	if context := field("Where"); len(context) <= 4096 {
		if match := databaseFunctionName.FindStringSubmatch(context); match != nil {
			out.Function = match[1]
		}
	}
	return out, true
}

// attrs is the log line's fields. Empty names are left out.
func (f databaseFailure) attrs() []any {
	out := []any{"sqlstate", f.SQLState}
	for _, pair := range [][2]string{{"schema", f.Schema}, {"table", f.Table}, {"column", f.Column}, {"constraint", f.Constraint}, {"function", f.Function}} {
		if pair[1] != "" {
			out = append(out, pair[0], pair[1])
		}
	}
	return out
}

// logDatabaseFailure writes the line for an error the caller is only told
// "could not be completed" about. It writes nothing for any other error.
func logDatabaseFailure(logger *slog.Logger, err error) {
	failure, ok := databaseFailureOf(err)
	if !ok {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("hcmnext.chat.database_failure", failure.attrs()...)
}
