package application

import (
	"errors"
	"fmt"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

// chatAuthorityDenial reports whether err is the authority saying no, as against
// the authority not answering.
func chatAuthorityDenial(err error) bool {
	return errors.Is(err, chatcore.ErrPermissionDenied) || errors.Is(err, chatcore.ErrNotFound) ||
		errors.Is(err, chatcore.ErrUnauthenticated) || errors.Is(err, chatcore.ErrInvalidArgument)
}

// chatAuthorityError names the outcome of a membership or conversation lookup the
// stream relies on. A refusal stays a refusal (the caller reports it as
// permission denied, which is what the page shows as lost access). Anything else
// is a lookup that did not finish, a database that did not answer, and is named
// so the stream ends without telling the person their access is gone.
func chatAuthorityError(err error) error {
	switch {
	case err == nil:
		return nil
	case chatAuthorityDenial(err):
		return chatcore.ErrPermissionDenied
	}
	return fmt.Errorf("%w: %w", chatstream.ErrAuthorityUnavailable, err)
}
