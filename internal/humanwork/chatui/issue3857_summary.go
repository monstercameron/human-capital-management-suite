package chatui

// Issue3857SummaryID names the one labelled summary a settled search outcome
// carries (#3857, #3896): "Results for ...", or the failure line. The browser
// client moves keyboard focus to it when the control a person activated was
// replaced by the update, so focus never falls to the page.
const Issue3857SummaryID = "chatsearch-summary"
