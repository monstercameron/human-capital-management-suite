package inbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
)

type pageFailureExecutor struct {
	inbox.Executor
	rows *pageFailureRows
	err  error
}

func (e pageFailureExecutor) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return e.rows, e.err
}

type pageFailureRows struct {
	scanErr, iterationErr error
	closed                bool
}

func (r *pageFailureRows) Next() bool        { return r.scanErr != nil }
func (r *pageFailureRows) Scan(...any) error { return r.scanErr }
func (r *pageFailureRows) Err() error        { return r.iterationErr }
func (r *pageFailureRows) Close()            { r.closed = true }

func TestTodo_NAAS_003_Failure(t *testing.T) {
	failure := errors.New("database failure")
	for _, kind := range []string{"query", "scan", "iteration", "cancel"} {
		for _, workflow := range []bool{false, true} {
			rows := &pageFailureRows{}
			ex := pageFailureExecutor{rows: rows}
			want := failure
			switch kind {
			case "query":
				ex.err = failure
			case "scan":
				rows.scanErr = failure
			case "iteration":
				rows.iterationErr = failure
			case "cancel":
				ex.err, want = context.Canceled, context.Canceled
			}
			var err error
			if workflow {
				var page inbox.WorkflowPage
				page, err = (inbox.Store{}).WorkflowNoticesPage(context.Background(), ex, uuid.New(), "owner", inbox.WorkflowPageQuery{})
				if len(page.Records) != 0 || page.Next != nil {
					t.Fatal("partial workflow page escaped after error")
				}
			} else {
				var page inbox.Page
				page, err = (inbox.Store{}).ListPage(context.Background(), ex, uuid.New(), "owner", inbox.PageQuery{})
				if len(page.Records) != 0 || page.Next != nil {
					t.Fatal("partial page escaped after error")
				}
			}
			if !errors.Is(err, want) {
				t.Fatalf("%s workflow=%t: got %v want %v", kind, workflow, err, want)
			}
			if ex.err == nil && !rows.closed {
				t.Fatal("failed cursor was not closed")
			}
		}
	}
}
