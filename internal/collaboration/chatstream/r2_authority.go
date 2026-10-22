package chatstream

import "errors"

// ErrAuthorityUnavailable is what an Authorizer returns when it could not decide:
// the database or a policy source did not answer. It is not a refusal. A stream
// that treated it as one told the person their access had been taken away and
// the page stopped watching for good, when a reconnect a moment later would have
// worked. The subscription ends without being marked revoked, and the periodic
// recheck simply tries again at its next tick. Events are still never delivered
// on an undecided answer.
var ErrAuthorityUnavailable = errors.New("chatstream: authority unavailable")

// refuse ends the subscription over an authorizer error and returns the terminal
// error the consumer sees: the undecided answer as it is, any other as revoked.
func (s *Subscription) refuse(err error) error {
	if authorityUnavailable(err) {
		s.close(err)
		return err
	}
	s.close(ErrRevoked)
	return ErrRevoked
}

// authorityUnavailable reports whether err says the authorizer could not decide.
func authorityUnavailable(err error) bool {
	return errors.Is(err, ErrAuthorityUnavailable)
}
