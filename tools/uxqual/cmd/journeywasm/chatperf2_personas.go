package main

import "time"

// CHATBUG-014: opening Chat read the agent directory of the open conversation
// four or five times (GET /api/chat/personas), each a second or more on the
// review machine:
//
//   - the first load started the agent reads for the conversation it had
//     selected and then opened that conversation, which started them again
//     (for the conversation in the address when that was another one) or wiped
//     what the first had put on the page;
//   - the activity watch asked for the directory again as soon as it connected,
//     because the first answer it gets always differs from the nothing it
//     started with;
//   - a newly opened stream replays the conversation's events, and every
//     replayed membership and post asked for the directory, at most once per
//     half second, for as long as the replay lasted.
//
// The first load now leaves the agent reads to the open that follows it, the
// watch's first answer asks for nothing, and the requests of a burst share one
// read after the burst has paused.

const (
	// chatperf2DirectoryQuiet is the pause in requests after which the
	// directory is read, and chatperf2DirectoryLimit the longest a request
	// waits for that pause.
	chatperf2DirectoryQuiet = 500 * time.Millisecond
	chatperf2DirectoryLimit = 4 * time.Second
)

// chatperf2DirectoryPace paces the directory read. The pause is the half second
// a request always waited.
func chatperf2DirectoryPace(time.Time) chatperfRenderPace {
	return chatperfRenderPace{quiet: chatperf2DirectoryQuiet, limit: chatperf2DirectoryLimit}
}

// chatperf2DirectoryStale reports whether an answer of the activity watch means
// the directory must be read again: the set of answer posts changed. The first
// answer of a watch changes nothing: the directory is read when the watch
// starts, from the same state.
func chatperf2DirectoryStale(first bool, known, next string) bool {
	return !first && known != next
}
