package main

// The seeded conversation. It is ordinary HR-company talk on purpose: a demo
// whose messages are "lorem ipsum 1..400" shows the layout and nothing about
// whether the product reads like a workplace.

var generalTopics = []string{
	"Morning all. Reminder that the Boston office closes early on Friday for the facilities audit.",
	"Coffee machine on the third floor is fixed. It took two vendors and a week.",
	"Welcome to the six people who started Monday — introductions are in #onboarding if you want to say hello.",
	"The all-hands deck is final. Questions go in the doc, not in the meeting, so we can actually get through them.",
	"Parking garage is resurfacing next week. Street parking is validated at the front desk.",
	"Small thing that saves everyone time: put the region in the ticket title. \"Slow\" tells us nothing.",
	"Anyone else getting double calendar invites for the regional sync? Pretty sure that is me. Sorry.",
	"Badge readers at the north entrance are back up.",
	"If you are travelling to the Portland site next month, book before the 15th or the rate goes up.",
	"Thanks to everyone who covered while the coordination team was at the conference. It showed.",
	"Fire drill Thursday at 10. It is a drill, but please actually leave the building.",
}

var announcementTopics = []string{
	"Open enrollment runs from the 6th to the 20th. Nothing changes automatically; if you do nothing you keep your current elections.",
	"We are adding two paid days for community care volunteering, effective next quarter. Details in #benefits.",
	"Leadership update: the care coordination and clinical operations teams now report through one operating review.",
	"Q3 results are out. Revenue is up eleven percent and member retention held at ninety-four.",
	"New expense policy: receipts required over fifty dollars, and approvals move to your manager of record rather than cost-center owner.",
	"The Portland clinic opens the 14th. Nineteen roles filled, four still open.",
	"Security training is due by month end. It is twenty minutes and it is not optional.",
	"We have signed the accessibility audit remediation plan. The full report is attached to the policy page.",
	"Payroll calendar for next year is published. Two pay dates shift because of bank holidays.",
	"Reminder that the anonymous feedback line is genuinely anonymous. We see themes, not names.",
}

var peopleOpsTopics = []string{
	"Performance calibration starts the week of the 9th. Managers, have your ratings drafted before the session, not during it.",
	"Two things we keep seeing in exit interviews: unclear promotion criteria and too many one-on-ones cancelled.",
	"Job architecture update: we collapsed the two senior coordinator levels into one. Nobody's pay changes.",
	"Headcount plan for Q4 is approved at fourteen net new. Backfills are not counted against it.",
	"Please stop putting compensation numbers in DMs. Use the comp tooling so there is a record.",
	"New manager cohort starts next month. Six sessions, and yes, attendance is tracked.",
	"Reminder on references: we confirm dates and title only. Anything else goes through legal.",
	"The engagement survey closes Friday. Seventy-one percent so far, which is our best yet.",
	"We are piloting a four-day onboarding week for clinical roles. Feedback after the first cohort.",
	"Leave balances now show accrued and projected separately, which should end the most common ticket we get.",
}

var compTopics = []string{
	"Comp cycle timeline: recommendations due the 12th, calibration the 16th, approvals the 20th, letters the 28th.",
	"Bands moved by market: clinical coordination up four percent, platform engineering up six, everything else held.",
	"If a recommendation puts somebody above band maximum it needs a written justification. No exceptions this cycle.",
	"Budget is 3.4 percent of base across the company, weighted toward the bottom two quartiles.",
	"Please use the band chart rather than last year's spreadsheet. Three bands were re-pointed.",
	"One pattern worth flagging: our strongest retention risk is the third quartile of the senior coordinator band.",
	"Equity refresh is separate from this cycle and lands in February.",
	"Reminder that a promotion and a merit increase are two decisions. Recording them as one loses the history.",
	"Off-cycle requests pause during calibration. They resume the 21st.",
}

var engineeringTopics = []string{
	"The workflow engine's retry path is now idempotent by execution key, which kills the duplicate-notice class of bugs.",
	"Chat watches were dying after thirty seconds. Turned out the unary deadline cap applied to server streams.",
	"We are moving media out of post bodies entirely. Posts reference an artifact; the bytes stay behind a scanned, granted download.",
	"Reminder: no new package-level mutable state. Put it on the value that owns it.",
	"Migration 0041 adds the backward index for post paging. It is online, no lock.",
	"Postgres RLS is on every tenant-scoped table now. If a query forgets the tenant it returns nothing instead of everything.",
	"Load test: eight hundred concurrent watches on one conversation, no shedding, p99 delivery under 300 ms.",
	"Please stop using the outbox as a queue for derived work. It is the canonical event log.",
	"The route directory and the chat shard are separate databases and they stay separate. No joins, ever.",
	"Code review note: an error that reaches the edge unowned logs as an unclassified failure, which tells an operator nothing.",
}

var designTopics = []string{
	"New empty states for the conversation list. A room with no posts should invite a first message, not look broken.",
	"We are dropping the third accent colour. Two is enough and the third never passed contrast.",
	"Thread replies now indent once and only once. Deeper nesting tested badly with everyone.",
	"Attachment previews: fixed aspect box, alt text required, and no layout shift when the image loads.",
	"The unread divider moves to a sticky row rather than a bold room name. Bold on everything means bold on nothing.",
	"Reduced-motion users get the first frame of an animated attachment, not a still of frame four.",
	"Pinned posts get a rail rather than a banner. Banners get dismissed and then nobody finds the pin.",
	"Mention chips are the same component in the composer and the message. They were two components and they drifted.",
}

var salesTopics = []string{
	"Closed the regional health network — twelve hundred seats, two-year term.",
	"Pipeline review moved to Tuesdays. Bring the number you believe, not the number you hope.",
	"The security questionnaire is the longest part of every deal now. We are pre-filling eighty percent of it.",
	"Lost the municipal bid on implementation timeline, not price. Worth reading the debrief.",
	"Two renewals at risk this quarter, both over reporting gaps rather than product.",
	"New one-pager for the benefits module is in the shared drive. Please use it instead of the old deck.",
	"Reminder that we do not commit to roadmap dates in a deal. Ever.",
}

var benefitsTopics = []string{
	"Open enrollment: the high-deductible plan adds a company HSA contribution of nine hundred a year.",
	"Dental network is unchanged. Vision moves carriers and the new one covers lenses annually rather than biennially.",
	"Parental leave goes to sixteen weeks for all parents, effective January, and it is not tied to tenure.",
	"The commuter benefit now covers bike maintenance up to two hundred a year.",
	"Mental-health visits: eight fully covered per year per person, no referral needed.",
	"If you are adding a dependent you have thirty days from the qualifying event, not thirty days from enrollment.",
	"Retirement match stays at four percent with immediate vesting.",
	"Reminder that the wellbeing stipend expires at year end and does not roll over.",
}

var onboardingTopics = []string{
	"Welcome to the new cohort. Day one is equipment and access, day two is safety and quality, day three is your team.",
	"Buddy assignments are posted. Buddies, please actually have the first coffee in week one.",
	"Everyone starting in a clinical role needs the credential check done before day three. It blocks scheduling.",
	"Laptop pickup is the front desk at the Boston office or courier for remote starters.",
	"If your access is missing something, file it rather than asking a neighbour to share. Shared credentials are a fireable thing.",
	"Thirty-day check-ins are on the calendar. They are a conversation, not a review.",
	"The onboarding survey is two questions long. Please answer both honestly.",
}

var randomTopics = []string{
	"The office plant survived the long weekend. Nobody knows who watered it.",
	"Lunch spot recommendations near the Portland site? Somebody said there is a good noodle place.",
	"Photo from the volunteering day. There were more of us than I expected.",
	"Somebody left a very nice umbrella in the fourth-floor kitchen.",
	"Friday puzzle: two coordinators, three regions, one on-call rota. Go.",
	"My cat joined the regional sync. She had notes.",
	"Team trivia Thursday. Last quarter's winners are not allowed to form the same team.",
}

var leadershipTopics = []string{
	"Holding the Portland opening to the 14th. Slipping it again costs more in credibility than in cash.",
	"We need a decision on the coordination reorg before comp letters go out, not after.",
	"Attrition in the third quartile is the number I keep coming back to. Comp alone will not fix it.",
	"The accessibility remediation plan is signed and funded. It is not negotiable against roadmap.",
	"Board materials are due the 8th. Two slides on retention, not six.",
	"Let us be honest in the all-hands about the municipal loss. People already know.",
}

var payrollTopics = []string{
	"Close checklist for this period: timecards locked, two retro adjustments, one garnishment change.",
	"Two pay dates shift next year for bank holidays. Communications go out with the calendar.",
	"The retro for the coordinator re-levelling is in this run, back to the first of the month.",
	"Variance report is clean apart from the Portland cost centre, which is new and expected.",
	"Reminder: no manual bank detail changes without a second approver. None.",
	"Filing deadlines for the quarter are on the shared calendar with a two-day buffer.",
}

var incidentTopics = []string{
	"Timeline for last night: alerting fired at 22:14, on-call acknowledged at 22:17, mitigated at 22:41.",
	"Root cause was a deadline cap applied to a streaming path. No member data was exposed.",
	"Action items: one code fix, one test at the transport boundary, one dashboard that would have shown this in a minute.",
	"We are writing this one up publicly for the customer. They asked good questions.",
	"No blame in the review. The system let one change do this, and that is the finding.",
}

var dmTopics = []string{
	"Do you have ten minutes before calibration? I want a sanity check on two of my recommendations.",
	"Sent you the band chart. The third row is the one I am worried about.",
	"Thanks for covering the regional sync. I owe you.",
	"Are you okay to take the new starter's day-three session? I am at the clinic opening.",
	"Quick one: is the retro in this run or next?",
	"I think we are saying the same thing in two different ways. Call?",
	"Reading your draft now. One comment, otherwise it is good.",
	"Heads up that I am out Thursday afternoon.",
}

var threadReplies = []string{
	"Agreed. I will take the first part.",
	"One correction: the date is the 16th, not the 14th.",
	"Can you say more about why we are holding the band rather than re-pointing it?",
	"This matches what I heard in the regional review.",
	"I will own the write-up and have a draft by Friday.",
	"Small risk: that timing collides with open enrollment comms.",
	"Done — link is in the thread above.",
	"Not sure I follow. Is this per region or company-wide?",
	"Company-wide. Regions can go earlier, not later.",
	"Thanks, that answers it.",
}

var multilineTail = []string{
	"Three things I need from each of you:\n- the recommendation\n- the justification in one sentence\n- whether you would counter if they resigned tomorrow",
	"For context, the numbers are:\n- 3.4 percent budget\n- 71 percent survey participation\n- 14 net new roles approved",
	"Sequence matters here:\n1. lock timecards\n2. apply retros\n3. run variance\n4. approve",
	"What I heard in the room:\n- two teams want the earlier date\n- one wants a week of slack\n- nobody wants to move the town hall",
	"Open questions before Friday:\n- who signs the exception\n- whether the band table is final\n- how we tell the affected managers",
	"Reminders for the new starters:\n- badge photo by Wednesday\n- benefits election closes on day 30\n- the buddy lunch is on us",
	"Quick status:\n- migration dry run passed\n- two flaky checks quarantined\n- rollback rehearsed twice",
	"Decisions from today:\n1. keep the Q4 freeze\n2. exceptions go through Priya\n3. revisit in the January review",
	"Numbers I want on the slide:\n- time to fill\n- offer acceptance\n- first-90-day attrition",
}

// chatterLines are the ordinary replies that fill a room once its topic lines
// have each been said once. Without them a two-week history repeated every
// topic six or seven times, and a search returned the same sentence from a
// dozen people.
var chatterLines = []string{
	"On it. Will update here when it is done.",
	"Can someone take a look at the doc before three?",
	"Moving this to Thursday so the right people can be in the room.",
	"Good catch. Fixed.",
	"I have a conflict at ten, can we push by thirty minutes?",
	"Adding the notes from this morning to the shared folder now.",
	"Who owns the follow-up on this one?",
	"That lines up with what finance told me yesterday.",
	"Quick reminder that the survey closes tonight.",
	"I am out tomorrow afternoon; Priya has the handover.",
	"Just checked, the numbers in the draft are the latest ones.",
	"Happy to pair on it after lunch.",
	"Agree with the direction, one question on timing.",
	"Can we keep this thread for decisions and take the debate to a call?",
	"The template is updated. Old copies are archived.",
	"Heads up: the room for the review moved to 4B.",
	"Nice work on this, everyone.",
	"I will send the summary by end of day.",
	"Does anyone have the contact at the Portland site?",
	"Looping in the right owner here.",
	"Blocked on access to the reporting workspace. Ticket is filed.",
	"This is ready for a second pair of eyes.",
	"The link in the calendar invite is the old one; use the one in the doc.",
	"Confirmed with legal, we are fine to proceed.",
	"Let us revisit after the numbers come in on Friday.",
	"Sharing the recording for anyone who missed it.",
	"I can cover that shift if nobody else has.",
	"Small update: the vendor pushed delivery to next week.",
	"Please add your availability to the poll by noon.",
	"Thanks for the quick turnaround.",
	"Worth noting this affects the Boston team first.",
	"First draft is up. Rip it apart.",
	"We decided to hold this until after open enrollment.",
	"Reminder that the office is closed Monday.",
	"I have three open questions; putting them in the doc.",
	"Signed off from my side.",
	"Can we get a second approver on this before it goes out?",
	"The dashboard refreshed and the gap closed overnight.",
	"Anyone know why the badge reader on two is beeping?",
	"Let me know if you need anything from me before the deadline.",
}

var seedEmoji = []string{"👍", "🎉", "❤️", "👀", "🙏", "✅", "😂", "🔥", "💡", "🚀"}
