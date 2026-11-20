package productclient

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Issue 3786: the digest of the shell bell. The browser reads the person's
// digest choice, and the digest when its time has come, from the chat digest
// route; the server works it out from messages the person can read right now.
// A failed read says the digest is unavailable and shows nothing it cannot
// vouch for.

// ChatDigestFeed is what the bell needs from the digest route.
type ChatDigestFeed struct {
	// Read returns the person's choice and, when due, the digest.
	Read func(context.Context) (*productui.ChatDigest, error)
	// Save stores a new choice.
	Save func(ctx context.Context, cadence string, minute int) error
	// Seen records that the digest was seen through the given moment.
	Seen func(ctx context.Context, through time.Time) error
}

func loadChatDigest(ctx context.Context, feed *ChatDigestFeed) (*productui.ChatDigest, bool) {
	if feed == nil || feed.Read == nil {
		return nil, false
	}
	digest, err := feed.Read(ctx)
	if err != nil || digest == nil {
		return nil, true
	}
	return digest, false
}

// nameChatDigestAuthors fills in authors from the people this page knows. An
// identifier is never written in a name's place.
func nameChatDigestAuthors(digest *productui.ChatDigest, people []productui.Person) {
	if digest == nil {
		return
	}
	for i := range digest.Items {
		if strings.TrimSpace(digest.Items[i].AuthorName) != "" {
			continue
		}
		id := strings.TrimSpace(digest.Items[i].AuthorID)
		for _, person := range people {
			if id == "" || (person.ID != id && person.WorkerID != id) {
				continue
			}
			name := strings.TrimSpace(person.PreferredName)
			if name == "" {
				name = strings.TrimSpace(person.Name)
			}
			digest.Items[i].AuthorName = name
			break
		}
	}
}

func chatDigestHandlers(ctx context.Context, feed *ChatDigestFeed) (func(string, int, func(error)), func(time.Time, func(error))) {
	if feed == nil {
		return nil, nil
	}
	run := func(call func(context.Context) error, done func(error)) {
		go func() {
			operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			err := call(operationCtx)
			if done != nil {
				done(err)
			}
		}()
	}
	var save func(string, int, func(error))
	if feed.Save != nil {
		save = func(cadence string, minute int, done func(error)) {
			run(func(c context.Context) error { return feed.Save(c, cadence, minute) }, done)
		}
	}
	var seen func(time.Time, func(error))
	if feed.Seen != nil {
		seen = func(through time.Time, done func(error)) {
			run(func(c context.Context) error { return feed.Seen(c, through) }, done)
		}
	}
	return save, seen
}
