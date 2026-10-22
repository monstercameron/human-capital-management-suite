package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// chatremoveOpenReports is the optional half of the report admission that knows
// whether the person has already reported a message and nobody has decided it.
type chatremoveOpenReports interface {
	HasOpenReport(context.Context, chat.Principal, string, string, string) (bool, error)
}

func (s *chatremoveRoutedStore) HasOpenReport(ctx context.Context, p chat.Principal, t, cid, post string) (bool, error) {
	if open, ok := s.Permissions.(chatremoveOpenReports); ok {
		return open.HasOpenReport(ctx, p, t, cid, post)
	}
	return false, nil
}
