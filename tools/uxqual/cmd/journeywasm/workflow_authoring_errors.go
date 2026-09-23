package main

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// workflowAuthoringErrorKey names the catalog message for a refused edit.
// Every failure used to read "We couldn't save that change. Reload the draft
// and try again", which is wrong advice for a rule the server will refuse
// again after any number of reloads.
func workflowAuthoringErrorKey(err error) string {
	switch status.Code(err) {
	case codes.Aborted:
		return "workflow_editor.error_conflict"
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		return "workflow_editor.error_refused"
	case codes.PermissionDenied, codes.Unauthenticated:
		return "workflow_editor.error_denied"
	case codes.NotFound:
		return "workflow_editor.error_gone"
	case codes.Unavailable, codes.DeadlineExceeded:
		return "workflow_editor.error_offline"
	default:
		return "workflow_draft.save_failed"
	}
}
