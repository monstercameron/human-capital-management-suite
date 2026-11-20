package productclient

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Issue 3733: the chat items of the shell bell. The browser reads them from the
// chat notification route (the server computes the list from messages the
// person can read and keeps their read state) and writes a read choice back.
// A failed read never fails the page: the bell says chat notifications are
// unavailable instead of showing items it can no longer vouch for.

// ChatNotificationFeed is what the bell needs from the chat route.
type ChatNotificationFeed struct {
	// List returns the signed-in person's chat notifications, newest first.
	List func(context.Context) ([]productui.ChatNotification, error)
	// MarkRead marks the named notifications read, or every one when all is true.
	MarkRead func(ctx context.Context, ids []string, all bool) error
}

// loadChatNotifications reads the feed. It returns whether the feed could not
// be read; a missing feed is simply empty. The server decides who may read the feed;
// the bell shows the section only to people who may open Chat.
func loadChatNotifications(ctx context.Context, feed *ChatNotificationFeed) ([]productui.ChatNotification, bool) {
	if feed == nil || feed.List == nil {
		return nil, false
	}
	items, err := feed.List(ctx)
	if err != nil {
		return nil, true
	}
	return items, false
}

// nameChatNotificationAuthors fills in authors the feed could not name from the
// people this page already knows. An identifier is never written in a name's
// place: an unknown author stays empty and the bell says "Someone".
func nameChatNotificationAuthors(items []productui.ChatNotification, people []productui.Person) {
	for i := range items {
		if strings.TrimSpace(items[i].AuthorName) != "" {
			continue
		}
		id := strings.TrimSpace(items[i].AuthorID)
		for _, person := range people {
			if id == "" || (person.ID != id && person.WorkerID != id) {
				continue
			}
			name := strings.TrimSpace(person.PreferredName)
			if name == "" {
				name = strings.TrimSpace(person.Name)
			}
			items[i].AuthorName = name
			break
		}
	}
}

func chatNotificationReadHandlers(ctx context.Context, feed *ChatNotificationFeed) (func(string, func(error)), func(func(error))) {
	if feed == nil || feed.MarkRead == nil {
		return nil, nil
	}
	run := func(ids []string, all bool, done func(error)) {
		go func() {
			operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			err := feed.MarkRead(operationCtx, ids, all)
			if done != nil {
				done(err)
			}
		}()
	}
	return func(id string, done func(error)) { run([]string{id}, false, done) },
		func(done func(error)) { run(nil, true, done) }
}
