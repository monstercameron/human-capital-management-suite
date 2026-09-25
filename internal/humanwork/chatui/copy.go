package chatui

// Catalog keys for every visible or accessible string in the workspace. The
// product shell resolves them through its locale catalog (Model.Text); the
// English values here are the reviewed source copy and the fallback.
const (
	KeySkip                           = "chat.skip"
	KeyNav                            = "chat.nav"
	KeyRailTitle                      = "chat.rail_title"
	KeyRailEmpty                      = "chat.rail_empty"
	KeyNewConversation                = "chat.new_conversation"
	KeySearch                         = "chat.search"
	KeySearchPlaceholder              = "chat.search_placeholder"
	KeySearchResults                  = "chat.search_results"
	KeySearchChannels                 = "chat.search_channels"
	KeySearchPeople                   = "chat.search_people"
	KeySearchMessages                 = "chat.search_messages"
	KeySearchLoading                  = "chat.search_loading"
	KeySearchCount                    = "chat.search_count"
	KeySearchCountOne                 = "chat.search_count_one"
	KeySearchError                    = "chat.search_error"
	KeySearchNoResults                = "chat.search_no_results"
	KeySearchMessageIn                = "chat.search_message_in"
	KeySearchOpenMessage              = "chat.search_open_message"
	KeySearchMore                     = "chat.search_more"
	KeySearchLoadingMore              = "chat.search_loading_more"
	KeySearchMoreError                = "chat.search_more_error"
	KeySearchMoreChannels             = "chat.search_more_channels"
	KeySearchLoadingMoreChannels      = "chat.search_loading_more_channels"
	KeySearchMoreChannelsError        = "chat.search_more_channels_error"
	KeySearchBrowseChannel            = "chat.search_browse_channel"
	KeySearchChannelUnavailable       = "chat.search_channel_unavailable"
	KeySearchMessageUnavailable       = "chat.search_message_unavailable"
	KeySectionChannels                = "chat.section_channels"
	KeySectionDirect                  = "chat.section_direct"
	KeyNewSection                     = "chat.new_section"
	KeySectionName                    = "chat.section_name"
	KeyRemoveSection                  = "chat.remove_section"
	KeyMoveToSection                  = "chat.move_to_section"
	KeyMoveSectionUp                  = "chat.move_section_up"
	KeyMoveSectionDown                = "chat.move_section_down"
	KeyMoveConversationUp             = "chat.move_conversation_up"
	KeyMoveConversationDown           = "chat.move_conversation_down"
	KeyBrowse                         = "chat.browse"
	KeyBrowseTitle                    = "chat.browse_title"
	KeyBrowseEmpty                    = "chat.browse_empty"
	KeyJoin                           = "chat.join"
	KeyJoined                         = "chat.joined"
	KeyJoinChannelTitle               = "chat.join_channel_title"
	KeyJoinChannelBody                = "chat.join_channel_body"
	KeyJoinChannelConfirm             = "chat.join_channel_confirm"
	KeyJoinChannelDismiss             = "chat.join_channel_dismiss"
	KeyJoinPending                    = "chat.join_pending"
	KeyOpenConversations              = "chat.open_conversations"
	KeyCloseConversations             = "chat.close_conversations"
	KeyConversation                   = "chat.conversation"
	KeyKindPublic                     = "chat.kind_public"
	KeyKindPrivate                    = "chat.kind_private"
	KeyKindDirect                     = "chat.kind_direct"
	KeyKindGroup                      = "chat.kind_group"
	KeyMemberCount                    = "chat.member_count"
	KeyMemberCountOne                 = "chat.member_count_one"
	KeyUnreadCount                    = "chat.unread_count"
	KeyMentionCount                   = "chat.mention_count"
	KeyDetails                        = "chat.details"
	KeyCloseDetails                   = "chat.close_details"
	KeyMembers                        = "chat.members"
	KeyRefreshMembers                 = "chat.refresh_members"
	KeyOnline                         = "chat.online"
	KeyOffline                        = "chat.offline"
	KeyNotifications                  = "chat.notifications"
	KeyNotifyAll                      = "chat.notify_all"
	KeyNotifyMentions                 = "chat.notify_mentions"
	KeyNotifyMute                     = "chat.notify_mute"
	KeyQuietHours                     = "chat.quiet_hours"
	KeyQuietHoursOn                   = "chat.quiet_hours_on"
	KeyQuietTimezone                  = "chat.quiet_timezone"
	KeyQuietStart                     = "chat.quiet_start"
	KeyQuietEnd                       = "chat.quiet_end"
	KeyMessagesRegion                 = "chat.messages_region"
	KeyMessageActions                 = "chat.message_actions"
	KeyLoading                        = "chat.loading"
	KeyErrorTitle                     = "chat.error_title"
	KeyRetry                          = "chat.retry"
	KeyEmptyTitle                     = "chat.empty_title"
	KeyEmptyBody                      = "chat.empty_body"
	KeyNoneTitle                      = "chat.none_title"
	KeyNoneBody                       = "chat.none_body"
	KeyNoMessages                     = "chat.no_messages"
	KeyLoadOlder                      = "chat.load_older"
	KeyLoadNewer                      = "chat.load_newer"
	KeyLoadOlderThread                = "chat.load_older_thread"
	KeyLoadNewerThread                = "chat.load_newer_thread"
	KeyToday                          = "chat.today"
	KeyYesterday                      = "chat.yesterday"
	KeyEdited                         = "chat.edited"
	KeyPinned                         = "chat.pinned"
	KeyPinJump                        = "chat.pin_jump"
	KeyPinCopy                        = "chat.pin_copy"
	KeyPinCopyGuestUnavailable        = "chat.pin_copy_guest_unavailable"
	KeyTodoTitle                      = "chat.todo_title"
	KeyPollTitle                      = "chat.poll_title"
	KeyPollQuestion                   = "chat.poll_question"
	KeyPollOptions                    = "chat.poll_options"
	KeyPollOptionsHint                = "chat.poll_options_hint"
	KeyPollQuestionPlaceholder        = "chat.poll_question_placeholder"
	KeyPollOptionsPlaceholder         = "chat.poll_options_placeholder"
	KeyPollCreate                     = "chat.poll_create"
	KeyPollVote                       = "chat.poll_vote"
	KeyPollChangeVote                 = "chat.poll_change_vote"
	KeyPollResults                    = "chat.poll_results"
	KeyPollVoteOne                    = "chat.poll_vote_one"
	KeyPollVotes                      = "chat.poll_votes"
	KeyPollVoted                      = "chat.poll_voted"
	KeyPollLoading                    = "chat.poll_loading"
	KeyPollError                      = "chat.poll_error"
	KeyPollNoPoll                     = "chat.poll_no_poll"
	KeyTeamWidget                     = "chat.team_widget"
	KeyTeamPurpose                    = "chat.team_purpose"
	KeyTeamRole                       = "chat.team_role"
	KeyTeamRoles                      = "chat.team_roles"
	KeyChannelManager                 = "chat.channel_manager"
	KeyChannelMember                  = "chat.channel_member"
	KeyTeamNote                       = "chat.team_note"
	KeyProjectWidget                  = "chat.project_widget"
	KeyProjectNote                    = "chat.project_note"
	KeyProjectTitle                   = "chat.project_title"
	KeyProjectSummary                 = "chat.project_summary"
	KeyMilestone                      = "chat.milestone"
	KeyMilestoneStatus                = "chat.milestone_status"
	KeyMilestoneOwner                 = "chat.milestone_owner"
	KeyMilestoneDue                   = "chat.milestone_due"
	KeyPlanned                        = "chat.planned"
	KeyInProgress                     = "chat.in_progress"
	KeyBlocked                        = "chat.blocked"
	KeyDone                           = "chat.done"
	KeyWidgetSave                     = "chat.widget_save"
	KeyWidgetEdit                     = "chat.widget_edit"
	KeyWidgetNotSet                   = "chat.widget_not_set"
	KeyWidgetAdd                      = "chat.widget_add"
	KeyWidgetDelete                   = "chat.widget_delete"
	KeyWidgetPin                      = "chat.widget_pin"
	KeyWidgetUnpin                    = "chat.widget_unpin"
	KeyWidgetLoading                  = "chat.widget_loading"
	KeyWidgetError                    = "chat.widget_error"
	KeyWidgetEmpty                    = "chat.widget_empty"
	KeyTodoLoading                    = "chat.todo_loading"
	KeyTodoError                      = "chat.todo_error"
	KeyTodoSaveError                  = "chat.todo_save_error"
	KeyTodoEmpty                      = "chat.todo_empty"
	KeyTodoNew                        = "chat.todo_new"
	KeyTodoAdd                        = "chat.todo_add"
	KeyTodoComplete                   = "chat.todo_complete"
	KeyTodoReopen                     = "chat.todo_reopen"
	KeyTodoDelete                     = "chat.todo_delete"
	KeyTodoPin                        = "chat.todo_pin"
	KeyTodoUnpin                      = "chat.todo_unpin"
	KeyTodoOpen                       = "chat.todo_open"
	KeyTodoRemaining                  = "chat.todo_remaining"
	KeyTodoNoOpen                     = "chat.todo_no_open"
	KeyTodoAttachPin                  = "chat.todo_attach_pin"
	KeyTodoNoPin                      = "chat.todo_no_pin"
	KeyTodoSource                     = "chat.todo_source"
	KeyTodoCompletedBy                = "chat.todo_completed_by"
	KeyTodoMemberFallback             = "chat.todo_member_fallback"
	KeyTodoModeEveryone               = "chat.todo_mode_everyone"
	KeyTodoModeMe                     = "chat.todo_mode_me"
	KeyTodoModeSelected               = "chat.todo_mode_selected"
	KeyTodoModeLabel                  = "chat.todo_mode_label"
	KeyTodoAddMember                  = "chat.todo_add_member"
	KeyTodoRemoveMember               = "chat.todo_remove_member"
	KeyTodoOnlyCreator                = "chat.todo_only_creator"
	KeyTodoOnlySelected               = "chat.todo_only_selected"
	KeyTodoSelectedMember             = "chat.todo_selected_member"
	KeyReplies                        = "chat.replies"
	KeyReactions                      = "chat.reactions"
	KeyReply                          = "chat.reply"
	KeyReact                          = "chat.react"
	KeyRemoveReaction                 = "chat.remove_reaction"
	KeyPin                            = "chat.pin"
	KeyUnpin                          = "chat.unpin"
	KeyEdit                           = "chat.edit"
	KeyDelete                         = "chat.delete"
	KeySaveEdit                       = "chat.save_edit"
	KeyCancel                         = "chat.cancel"
	KeyClose                          = "chat.close"
	KeyThread                         = "chat.thread"
	KeyThreadRegion                   = "chat.thread_region"
	KeyThreadEmpty                    = "chat.thread_empty"
	KeyThreadLoading                  = "chat.thread_loading"
	KeyCloseThread                    = "chat.close_thread"
	KeyFollow                         = "chat.follow"
	KeyFollowing                      = "chat.following"
	KeyComposeRegion                  = "chat.compose_region"
	KeyMessage                        = "chat.message"
	KeyComposePlaceholder             = "chat.compose_placeholder"
	KeyComposeUnselected              = "chat.compose_unselected"
	KeyComposeHint                    = "chat.compose_hint"
	KeyEmojiPicker                    = "chat.emoji_picker"
	KeyEmojiPickerTitle               = "chat.emoji_picker_title"
	KeyEmojiItem                      = "chat.emoji_item"
	KeyGiphyPicker                    = "chat.giphy_picker"
	KeyGiphyPickerTitle               = "chat.giphy_picker_title"
	KeyGiphySearch                    = "chat.giphy_search"
	KeyGiphyMore                      = "chat.giphy_more"
	KeyGiphyUnavailable               = "chat.giphy_unavailable"
	KeyGiphyLoading                   = "chat.giphy_loading"
	KeyGiphyLoadError                 = "chat.giphy_load_error"
	KeyGiphyNoResults                 = "chat.giphy_no_results"
	KeyGiphyClose                     = "chat.giphy_close"
	KeyAttach                         = "chat.attach"
	KeySend                           = "chat.send"
	KeyCreateTitle                    = "chat.create_title"
	KeyName                           = "chat.name"
	KeyNamePlaceholder                = "chat.name_placeholder"
	KeyKind                           = "chat.kind"
	KeyMembersOptional                = "chat.members_optional"
	KeyMembersPlaceholder             = "chat.members_placeholder"
	KeyCreate                         = "chat.create"
	KeyRefresh                        = "chat.refresh"
	KeyOn                             = "chat.on"
	KeyOff                            = "chat.off"
	KeyReplyPlaceholder               = "chat.reply_placeholder"
	KeyReplySend                      = "chat.reply_send"
	KeyBrowseCreate                   = "chat.browse_create"
	KeyIntroTitle                     = "chat.intro_title"
	KeyIntroTitleDirect               = "chat.intro_title_direct"
	KeyIntroPublic                    = "chat.intro_public"
	KeyIntroPrivate                   = "chat.intro_private"
	KeyIntroDirect                    = "chat.intro_direct"
	KeyDMEmpty                        = "chat.dm_empty"
	KeyBrowseFilter                   = "chat.browse_filter"
	KeyAttachmentLoading              = "chat.attachment_loading"
	KeyAttachmentUnavailable          = "chat.attachment_unavailable"
	KeyDownloadAttachment             = "chat.download_attachment"
	KeyOpenImage                      = "chat.open_image"
	KeyImageViewer                    = "chat.image_viewer"
	KeyCloseImageViewer               = "chat.close_image_viewer"
	KeyImageActualSize                = "chat.image_actual_size"
	KeyImageFitToScreen               = "chat.image_fit_to_screen"
	KeyGIF                            = "chat.gif"
	KeyRepliesOne                     = "chat.replies_one"
	KeyNew                            = "chat.new"
	KeyJumpNewest                     = "chat.jump_newest"
	KeyMore                           = "chat.more"
	KeyConversationMore               = "chat.conversation_more"
	KeyCopyConversationReference      = "chat.copy_conversation_reference"
	KeyCopyConversationAPICurl        = "chat.copy_conversation_api_curl"
	KeyMenuDeveloperSection           = "chat.menu_developer_section"
	KeyMenuDeveloperSectionHint       = "chat.menu_developer_section_hint"
	KeyIntegrationsTitle              = "chat.integrations_title"
	KeyIntegrationsHint               = "chat.integrations_hint"
	KeyCopyConversationAPICurlSuccess = "chat.copy_conversation_api_curl_success"
	KeyCopyConversationAPICurlFailure = "chat.copy_conversation_api_curl_failure"
	KeyCopyConversationSuccess        = "chat.copy_conversation_success"
	KeyCopyConversationFailure        = "chat.copy_conversation_failure"
	KeyConversationLinkUnavailable    = "chat.conversation_link_unavailable"
	KeyCopyLink                       = "chat.copy_link"
	KeyCopyContents                   = "chat.copy_contents"
	KeyCopyContentsEmpty              = "chat.copy_contents_empty"
	KeyCopyContentsSuccess            = "chat.copy_contents_success"
	KeyCopyContentsFailure            = "chat.copy_contents_failure"
	KeyShareToChannel                 = "chat.share_to_channel"
	KeyShareTitle                     = "chat.share_title"
	KeyShareDestination               = "chat.share_destination"
	KeyShareFilter                    = "chat.share_filter"
	KeyShareEmpty                     = "chat.share_empty"
	KeyShareLoading                   = "chat.share_loading"
	KeyShareDisclosure                = "chat.share_disclosure"
	KeyShareAttachments               = "chat.share_attachments"
	KeyShareSubmit                    = "chat.share_submit"
	KeySharePending                   = "chat.share_pending"
	KeyShareLoadError                 = "chat.share_load_error"
	KeyShareError                     = "chat.share_error"
	KeyShareSuccess                   = "chat.share_success"
	KeyShareUnavailable               = "chat.share_unavailable"
	KeyForwardedFrom                  = "chat.forwarded_from"
	KeyEmbedTitle                     = "chat.embed_title"
	KeyEmbedLoading                   = "chat.embed_loading"
	KeyEmbedUnavailable               = "chat.embed_unavailable"
	KeyEmbedOpen                      = "chat.embed_open"
	KeyEmbedAttachments               = "chat.embed_attachments"
	KeyDocEmbedTitle                  = "chat.doc_embed_title"
	KeyDocEmbedLoading                = "chat.doc_embed_loading"
	KeyDocLinkPending                 = "chat.doc_link_pending"
	KeyDocRestricted                  = "chat.doc_restricted"
	KeyOpenDocument                   = "chat.open_document"
	KeyProjectTaskRestricted          = "chat.project_task_restricted"
	KeyOpenProjectTask                = "chat.open_project_task"
	KeyOpenProjectBoard               = "chat.open_project_board"
	KeyProjectTaskEmbed               = "chat.project_task_embed"
	KeyProjectBoardEmbed              = "chat.project_board_embed"
	KeyProjectEmbedLoading            = "chat.project_embed_loading"
	KeyProjectBoardProgress           = "chat.project_board_progress"
	KeyProjectBoardEmpty              = "chat.project_board_empty"
	KeyProjectDue                     = "chat.project_due"
	KeyProjectPriorityLow             = "chat.project_priority_low"
	KeyProjectPriorityNormal          = "chat.project_priority_normal"
	KeyProjectPriorityHigh            = "chat.project_priority_high"
	KeyProjectPriorityUrgent          = "chat.project_priority_urgent"
	KeyJourneyLinkLabel               = "chat.journey_link_label"
	KeyOpenJourney                    = "chat.open_journey"
	KeyJourneyRestricted              = "chat.journey_restricted"
	KeyJourneyPromotion               = "chat.journey_promotion"
	KeyJourneyEmbed                   = "chat.journey_embed"
	KeyJourneyEffective               = "chat.journey_effective"
	KeyJourneyApprover                = "chat.journey_approver"
	KeyJourneyStageProposed           = "chat.journey_stage_proposed"
	KeyJourneyStageBlocked            = "chat.journey_stage_blocked"
	KeyJourneyStageAwaiting           = "chat.journey_stage_awaiting"
	KeyJourneyStageCompleted          = "chat.journey_stage_completed"
	KeyJourneyStageRejected           = "chat.journey_stage_rejected"
	KeyJourneyStageFailed             = "chat.journey_stage_failed"
	KeyJourneyStageFinance            = "chat.journey_stage_finance"
	KeyJourneyStageManager            = "chat.journey_stage_manager"
	KeyJourneyStageWaiting            = "chat.journey_stage_waiting"
	KeyJourneyStageRevalidation       = "chat.journey_stage_revalidation"
	KeyJourneyStageInProgress         = "chat.journey_stage_in_progress"
	KeyDocSuggestTitle                = "chat.doc_suggest_title"
	KeyDocSuggestNone                 = "chat.doc_suggest_none"
	KeyPickReaction                   = "chat.pick_reaction"
	KeyReactionChip                   = "chat.reaction_chip"
	KeyReactWith                      = "chat.react_with"
	KeyResizeRail                     = "chat.resize_rail"
	KeyResizeSide                     = "chat.resize_side"
	KeyActiveJustNow                  = "chat.active_just_now"
	KeyActiveHoursAgo                 = "chat.active_hours_ago"
	KeyActiveDaysAgo                  = "chat.active_days_ago"
	KeyActiveOn                       = "chat.active_on"
	KeyNoActivity                     = "chat.no_activity"
	KeyYou                            = "chat.you"
	KeyMemberFilter                   = "chat.member_filter"
	KeyThreadIn                       = "chat.thread_in"
	KeyBrowseCount                    = "chat.browse_count"
	KeyPersonDetails                  = "chat.person_details"
	KeyClosePerson                    = "chat.close_person"
	KeyViewPerson                     = "chat.view_person"
	KeyStartDirectMessage             = "chat.start_direct_message"
	KeyManager                        = "chat.manager"
	KeyDirectReports                  = "chat.direct_reports"
	KeyViewOrgChart                   = "chat.view_org_chart"
	KeyPersonLoading                  = "chat.person_loading"
	KeyPersonUnavailable              = "chat.person_unavailable"
	KeyDepartment                     = "chat.department"
	KeyJobTitle                       = "chat.job_title"
	KeyPhone                          = "chat.phone"
	KeyEmail                          = "chat.email"
	KeyLocation                       = "chat.location"
	KeyCompany                        = "chat.company"
	KeyBusinessUnit                   = "chat.business_unit"
	KeyNotAvailable                   = "chat.not_available"
	KeyMentionTitle                   = "chat.mention_title"
	KeyMentionNone                    = "chat.mention_none"
	KeyMentionNotMember               = "chat.mention_not_member"
	KeySelfName                       = "chat.self_name"
	KeyIntroSelf                      = "chat.intro_self"
	KeyAddChannels                    = "chat.add_channels"
	KeyFormatToolbar                  = "chat.format_toolbar"
	KeyFormatBold                     = "chat.format_bold"
	KeyFormatItalic                   = "chat.format_italic"
	KeyFormatStrike                   = "chat.format_strike"
	KeyFormatCode                     = "chat.format_code"
	KeyFormatLink                     = "chat.format_link"
	KeyFormatBullets                  = "chat.format_bullets"
	KeyFormatQuote                    = "chat.format_quote"
	KeyMentionHint                    = "chat.mention_hint"
	KeyViewInChannel                  = "chat.view_in_channel"
	KeyTodoMoreOptions                = "chat.todo_more_options"
	KeySearchInChannel                = "chat.search_in_channel"
	KeySearchFilterHint               = "chat.search_filter_hint"
	KeyKindPublicDesc                 = "chat.kind_public_desc"
	KeyKindPrivateDesc                = "chat.kind_private_desc"
	KeyKindGroupDesc                  = "chat.kind_group_desc"
	KeyKindDirectDesc                 = "chat.kind_direct_desc"
	KeyChannelNameHint                = "chat.channel_name_hint"
	KeyAddPeople                      = "chat.add_people"
	KeyAddPeoplePlaceholder           = "chat.add_people_placeholder"
	KeyAddPeopleLabel                 = "chat.add_people_label"
	KeyAddMembersTitle                = "chat.add_members_title"
	KeyAddMembersSubmit               = "chat.add_members_submit"
	KeyAddMembersPending              = "chat.add_members_pending"
	KeyRemovePerson                   = "chat.remove_person"
	KeyStartConversation              = "chat.start_conversation"
	KeyCreateChannel                  = "chat.create_channel"
	KeyBrowseNoMatch                  = "chat.browse_no_match"
	KeyBrowseOpen                     = "chat.browse_open"
	KeyTodoProgress                   = "chat.todo_progress"
	KeyChannelTools                   = "chat.channel_tools"
	KeyQuietSummary                   = "chat.quiet_summary"
	KeyQuietHelp                      = "chat.quiet_help"
	KeyTimezoneDevice                 = "chat.timezone_device"
	KeyPeopleCount                    = "chat.people_count"
)

// englishCopy is the reviewed en-US source for every key above.
var englishCopy = map[string]string{
	KeyPersonDetails:                  "Person details",
	KeyClosePerson:                    "Close person details",
	KeyViewPerson:                     "View {name}'s details",
	KeyStartDirectMessage:             "Message",
	KeyManager:                        "Manager",
	KeyDirectReports:                  "Direct reports",
	KeyViewOrgChart:                   "View in org chart",
	KeyPersonLoading:                  "Loading person details…",
	KeyPersonUnavailable:              "Person details are unavailable.",
	KeyDepartment:                     "Department",
	KeyJobTitle:                       "Job title",
	KeyPhone:                          "Phone",
	KeyEmail:                          "Email",
	KeyLocation:                       "Location",
	KeyCompany:                        "Company",
	KeyBusinessUnit:                   "Business unit",
	KeyNotAvailable:                   "Not available",
	KeySkip:                           "Skip to conversation",
	KeyNav:                            "Chat navigation",
	KeyRailTitle:                      "Conversations",
	KeyRailEmpty:                      "You are not in any conversation yet. Browse channels or start one.",
	KeyNewConversation:                "New conversation",
	KeySearch:                         "Search channels, people, and messages",
	KeySearchPlaceholder:              "Search messages",
	KeySearchResults:                  "Results for “{query}”",
	KeySearchChannels:                 "Channels",
	KeySearchPeople:                   "People",
	KeySearchMessages:                 "Messages",
	KeySearchLoading:                  "Searching…",
	KeySearchCount:                    "{n} results",
	KeySearchCountOne:                 "1 result",
	KeySearchError:                    "Search is unavailable. Try again.",
	KeySearchNoResults:                "No results found.",
	KeySearchMessageIn:                "in {channel}",
	KeySearchOpenMessage:              "Open message in {channel} by {author}",
	KeySearchMore:                     "Load more messages",
	KeySearchLoadingMore:              "Loading more messages…",
	KeySearchMoreError:                "More messages could not be loaded. Try again.",
	KeySearchMoreChannels:             "Load more channels",
	KeySearchLoadingMoreChannels:      "Loading more channels…",
	KeySearchMoreChannelsError:        "More channels could not be loaded. Try again.",
	KeySearchBrowseChannel:            "Browse {channel} and join",
	KeySearchChannelUnavailable:       "This channel is no longer available.",
	KeySearchMessageUnavailable:       "This message is no longer available.",
	KeySectionChannels:                "Channels",
	KeySectionDirect:                  "Direct messages",
	KeyNewSection:                     "New section",
	KeySectionName:                    "Section name",
	KeyRemoveSection:                  "Remove section",
	KeyMoveToSection:                  "Move to {name}",
	KeyMoveSectionUp:                  "Move section up",
	KeyMoveSectionDown:                "Move section down",
	KeyMoveConversationUp:             "Move conversation up",
	KeyMoveConversationDown:           "Move conversation down",
	KeyBrowse:                         "Browse channels",
	KeyBrowseTitle:                    "Browse channels",
	KeyBrowseEmpty:                    "No channels to join yet.",
	KeyJoin:                           "Join",
	KeyJoined:                         "Joined",
	KeyJoinChannelTitle:               "Join #{name}?",
	KeyJoinChannelBody:                "Join this channel to keep it in your left sidebar. You can leave whenever you like.",
	KeyJoinChannelConfirm:             "Join channel",
	KeyJoinChannelDismiss:             "Not now",
	KeyJoinPending:                    "Joining…",
	KeyOpenConversations:              "Open conversations",
	KeyCloseConversations:             "Close conversations",
	KeyConversation:                   "Conversation",
	KeyKindPublic:                     "Public channel",
	KeyKindPrivate:                    "Private channel",
	KeyKindDirect:                     "Direct message",
	KeyKindGroup:                      "Private group",
	KeyMemberCount:                    "{n} members",
	KeyMemberCountOne:                 "1 member",
	KeyUnreadCount:                    "{n} unread",
	KeyMentionCount:                   "{n} mentions",
	KeyDetails:                        "Conversation details",
	KeyConversationMore:               "More options for {name}",
	KeyCopyConversationReference:      "Copy name and link",
	KeyCopyConversationAPICurl:        "Copy API curl",
	KeyMenuDeveloperSection:           "Developer",
	KeyMenuDeveloperSectionHint:       "Visible to this conversation's owner only.",
	KeyIntegrationsTitle:              "Integrations",
	KeyIntegrationsHint:               "Let an installed app read and post here. An admin creates the app token.",
	KeyCopyConversationAPICurlSuccess: "API curl copied",
	KeyCopyConversationAPICurlFailure: "Could not copy the API curl. Check clipboard access and try again.",
	KeyCopyConversationSuccess:        "Conversation link copied",
	KeyCopyConversationFailure:        "Could not copy the conversation link. Check clipboard access and try again.",
	KeyConversationLinkUnavailable:    "Conversation unavailable",
	KeyCloseDetails:                   "Close details",
	KeyMembers:                        "Members",
	KeyRefreshMembers:                 "Refresh members",
	KeyOnline:                         "Online",
	KeyOffline:                        "Offline",
	KeyNotifications:                  "Notifications",
	KeyNotifyAll:                      "All messages",
	KeyNotifyMentions:                 "Mentions only",
	KeyNotifyMute:                     "Muted",
	KeyQuietHours:                     "Quiet hours",
	KeyQuietHoursOn:                   "Pause notifications overnight",
	KeyQuietTimezone:                  "Time zone",
	KeyQuietStart:                     "From",
	KeyQuietEnd:                       "Until",
	KeyMessagesRegion:                 "Conversation messages",
	KeyMessageActions:                 "Message actions",
	KeyLoading:                        "Loading conversation…",
	KeyErrorTitle:                     "Conversation unavailable",
	KeyRetry:                          "Try again",
	KeyEmptyTitle:                     "Start the conversation",
	KeyEmptyBody:                      "There are no messages here yet. Send the first message when you’re ready.",
	KeyNoneTitle:                      "Choose a conversation",
	KeyNoneBody:                       "Pick a channel or a direct message from the list, or start a new one.",
	KeyNoMessages:                     "No messages yet",
	KeyLoadOlder:                      "Show earlier messages",
	KeyLoadNewer:                      "Show newer messages",
	KeyLoadOlderThread:                "Show earlier replies",
	KeyLoadNewerThread:                "Show newer replies",
	KeyToday:                          "Today",
	KeyYesterday:                      "Yesterday",
	KeyEdited:                         "edited",
	KeyPinned:                         "Pinned",
	KeyPinJump:                        "Jump to message",
	KeyPinCopy:                        "Copy reference",
	KeyPinCopyGuestUnavailable:        "References cannot be copied from a guest channel",
	KeyTodoTitle:                      "To-do list",
	KeyPollTitle:                      "Channel poll",
	KeyPollQuestion:                   "Question",
	KeyPollOptions:                    "Options",
	KeyPollOptionsHint:                "2 to 10, one per line",
	KeyPollQuestionPlaceholder:        "What should we decide?",
	KeyPollOptionsPlaceholder:         "Option A\nOption B",
	KeyPollCreate:                     "Create poll",
	KeyPollVote:                       "Vote",
	KeyPollChangeVote:                 "Change your vote",
	KeyPollResults:                    "Poll results",
	KeyPollVoteOne:                    "{n} vote",
	KeyPollVotes:                      "{n} votes",
	KeyPollVoted:                      "Your selection",
	KeyPollLoading:                    "Loading poll…",
	KeyPollError:                      "Could not load or save the poll. Refresh and try again.",
	KeyPollNoPoll:                     "No poll yet",
	KeyTeamWidget:                     "Channel team", KeyTeamPurpose: "Purpose", KeyTeamRole: "Channel role label", KeyTeamRoles: "Role labels", KeyTeamNote: "Channel labels are informal and do not change organization roles.", KeyChannelManager: "Channel manager", KeyChannelMember: "Channel member",
	KeyProjectWidget: "Channel project", KeyProjectNote: "Planning notes for this channel", KeyProjectTitle: "Project title", KeyProjectSummary: "Summary",
	KeyMilestone: "Milestone", KeyMilestoneStatus: "Status", KeyMilestoneOwner: "Owner", KeyMilestoneDue: "Due date",
	KeyPlanned: "Planned", KeyInProgress: "In progress", KeyBlocked: "Blocked", KeyDone: "Done", KeyWidgetSave: "Save", KeyWidgetEdit: "Edit", KeyWidgetNotSet: "Not set", KeyWidgetAdd: "Add milestone", KeyWidgetDelete: "Delete milestone", KeyWidgetPin: "Pin widget", KeyWidgetUnpin: "Unpin widget", KeyWidgetLoading: "Loading channel widgets…", KeyWidgetError: "Could not save or load channel widgets. Refresh and try again.", KeyWidgetEmpty: "No milestones yet",
	KeyTodoLoading:              "Loading to-do list…",
	KeyTodoError:                "Could not load the to-do list. Try again.",
	KeyTodoSaveError:            "Could not save the to-do list. Refresh it and try again.",
	KeyTodoEmpty:                "No tasks yet",
	KeyTodoNew:                  "New task",
	KeyTodoAdd:                  "Add task",
	KeyTodoComplete:             "Complete task",
	KeyTodoReopen:               "Reopen task",
	KeyTodoDelete:               "Delete task",
	KeyTodoPin:                  "Pin to-do list",
	KeyTodoUnpin:                "Unpin to-do list",
	KeyTodoOpen:                 "Open to-do list",
	KeyTodoRemaining:            "{n} open",
	KeyTodoNoOpen:               "No open tasks",
	KeyTodoAttachPin:            "Attach pinned message",
	KeyTodoNoPin:                "No linked message",
	KeyTodoSource:               "Linked message",
	KeyTodoCompletedBy:          "Completed by {name}",
	KeyTodoMemberFallback:       "channel member",
	KeyTodoModeEveryone:         "Everyone",
	KeyTodoModeMe:               "Me",
	KeyTodoModeSelected:         "Me and selected",
	KeyTodoModeLabel:            "Who can complete or reopen this task",
	KeyTodoAddMember:            "Add member",
	KeyTodoRemoveMember:         "Remove {name}",
	KeyTodoOnlyCreator:          "Only the task creator can complete or reopen this task",
	KeyTodoOnlySelected:         "Only the task creator and selected members can complete or reopen this task",
	KeyTodoSelectedMember:       "Selected member",
	KeyReplies:                  "{n} replies",
	KeyReactions:                "{n} reactions",
	KeyReply:                    "Reply in thread",
	KeyReact:                    "Add reaction",
	KeyRemoveReaction:           "Remove your reaction",
	KeyPin:                      "Pin message",
	KeyUnpin:                    "Unpin message",
	KeyEdit:                     "Edit message",
	KeyDelete:                   "Delete message",
	KeySaveEdit:                 "Save changes",
	KeyCancel:                   "Cancel",
	KeyClose:                    "Close",
	KeyThread:                   "Thread",
	KeyThreadRegion:             "Thread replies",
	KeyThreadEmpty:              "No replies yet. Your reply will start this thread.",
	KeyThreadLoading:            "Loading replies…",
	KeyCloseThread:              "Close thread",
	KeyFollow:                   "Follow",
	KeyFollowing:                "Following",
	KeyComposeRegion:            "Send a message",
	KeyMessage:                  "Message",
	KeyComposePlaceholder:       "Message {name}",
	KeyComposeUnselected:        "Choose a conversation to start writing",
	KeyComposeHint:              "Enter to send, Shift+Enter for a new line",
	KeyEmojiPicker:              "Insert emoji",
	KeyEmojiPickerTitle:         "Choose an emoji",
	KeyGiphyPicker:              "Insert GIF",
	KeyGiphyPickerTitle:         "Choose a GIF",
	KeyGiphySearch:              "Search GIFs",
	KeyGiphyMore:                "Load more GIFs",
	KeyGiphyUnavailable:         "GIFs need a GIPHY key configured by an admin",
	KeyGiphyLoading:             "Loading GIFs…",
	KeyGiphyLoadError:           "GIFs could not be loaded.",
	KeyGiphyNoResults:           "No GIFs found.",
	KeyGiphyClose:               "Close GIF picker",
	KeyEmojiItem:                "Emoji {emoji}",
	KeyAttach:                   "Add attachment",
	KeySend:                     "Send",
	KeyCreateTitle:              "Create conversation",
	KeyName:                     "Name",
	KeyNamePlaceholder:          "For example, design-reviews",
	KeyKind:                     "Conversation type",
	KeyMembersOptional:          "Members (optional)",
	KeyMembersPlaceholder:       "Names or IDs, separated by commas",
	KeyCreate:                   "Create",
	KeyRefresh:                  "Refresh",
	KeyOn:                       "On",
	KeyOff:                      "Off",
	KeyReplyPlaceholder:         "Reply…",
	KeyReplySend:                "Reply",
	KeyBrowseCreate:             "Create a channel",
	KeyIntroTitle:               "This is the beginning of {name}",
	KeyIntroTitleDirect:         "This is the beginning of your conversation with {name}",
	KeyIntroPublic:              "Anyone in the company can find and join this channel. Messages here are visible to everyone in it.",
	KeyIntroPrivate:             "Only invited members can see this channel and its messages.",
	KeyIntroDirect:              "This conversation is private to the people in it.",
	KeyDMEmpty:                  "No direct messages yet",
	KeyBrowseFilter:             "Filter channels",
	KeyAttachmentLoading:        "Loading attachment…",
	KeyAttachmentUnavailable:    "Preview unavailable",
	KeyDownloadAttachment:       "Download",
	KeyOpenImage:                "Open image",
	KeyImageViewer:              "Image viewer",
	KeyCloseImageViewer:         "Close image viewer",
	KeyImageActualSize:          "Show image at actual size",
	KeyImageFitToScreen:         "Fit image to screen",
	KeyGIF:                      "GIF",
	KeyRepliesOne:               "1 reply",
	KeyNew:                      "New",
	KeyJumpNewest:               "Jump to newest",
	KeyMore:                     "More actions",
	KeyCopyLink:                 "Copy link",
	KeyCopyContents:             "Copy message contents",
	KeyCopyContentsEmpty:        "No message text to copy",
	KeyCopyContentsSuccess:      "Message contents copied",
	KeyCopyContentsFailure:      "Could not copy message contents. Check clipboard access and try again.",
	KeyShareToChannel:           "Share to channel",
	KeyShareTitle:               "Share message to a channel",
	KeyShareDestination:         "Destination channel",
	KeyShareFilter:              "Search your channels",
	KeyShareEmpty:               "No eligible channels found",
	KeyShareLoading:             "Loading your channels…",
	KeyShareDisclosure:          "Members of the destination channel will see this message.",
	KeyShareAttachments:         "Only the message text will be shared. Attachments are not included.",
	KeyShareSubmit:              "Share",
	KeySharePending:             "Sharing…",
	KeyShareLoadError:           "Could not load channels. Try again.",
	KeyShareError:               "Could not share this message. Check your access and try again.",
	KeyShareSuccess:             "Message shared to channel",
	KeyShareUnavailable:         "Sharing is unavailable. Try again.",
	KeyForwardedFrom:            "Forwarded from {name}",
	KeyEmbedTitle:               "Linked message",
	KeyEmbedLoading:             "Loading message preview…",
	KeyEmbedUnavailable:         "Message preview unavailable",
	KeyEmbedOpen:                "Open source channel",
	KeyEmbedAttachments:         "Attachments: {count}",
	KeyDocEmbedTitle:            "Linked document",
	KeyDocEmbedLoading:          "Loading document…",
	KeyDocLinkPending:           "document",
	KeyDocRestricted:            "Document unavailable",
	KeyOpenDocument:             "Open document: {title}",
	KeyProjectTaskRestricted:    "Project task unavailable",
	KeyOpenProjectTask:          "Open project task: {title}",
	KeyOpenProjectBoard:         "Open project board: {title}",
	KeyProjectTaskEmbed:         "Project task",
	KeyProjectBoardEmbed:        "Project board",
	KeyProjectEmbedLoading:      "Loading project preview",
	KeyProjectBoardProgress:     "{done} of {total} done · {open} open",
	KeyProjectBoardEmpty:        "No tasks yet",
	KeyProjectDue:               "Due {date}",
	KeyProjectPriorityLow:       "Low",
	KeyProjectPriorityNormal:    "Normal",
	KeyProjectPriorityHigh:      "High",
	KeyProjectPriorityUrgent:    "Urgent",
	KeyJourneyLinkLabel:         "{workflow}: {name}",
	KeyOpenJourney:              "Open workflow: {title}",
	KeyJourneyRestricted:        "Workflow unavailable",
	KeyJourneyPromotion:         "Promotion",
	KeyJourneyEmbed:             "Workflow · {workflow}",
	KeyJourneyEffective:         "Effective {date}",
	KeyJourneyApprover:          "Approver: {name}",
	KeyJourneyStageProposed:     "Proposed",
	KeyJourneyStageBlocked:      "Blocked",
	KeyJourneyStageAwaiting:     "Awaiting approval",
	KeyJourneyStageCompleted:    "Completed",
	KeyJourneyStageRejected:     "Rejected",
	KeyJourneyStageFailed:       "Stopped",
	KeyJourneyStageFinance:      "Finance approval",
	KeyJourneyStageManager:      "Manager approval",
	KeyJourneyStageWaiting:      "Waiting for effective date",
	KeyJourneyStageRevalidation: "Rechecking",
	KeyJourneyStageInProgress:   "In progress",
	KeyDocSuggestTitle:          "Documents",
	KeyDocSuggestNone:           "No documents you can open match",
	KeyPickReaction:             "Choose a reaction",
	KeyReactionChip:             "{n} reacted with {emoji}",
	KeyReactWith:                "React with {emoji}",
	KeyResizeRail:               "Resize the conversation list. Drag, or use the arrow keys; Home restores the default width.",
	KeyResizeSide:               "Resize the side panel. Drag, or use the arrow keys; Home restores the default width.",
	KeyActiveJustNow:            "active just now",
	KeyActiveHoursAgo:           "active {n}h ago",
	KeyActiveDaysAgo:            "active {n}d ago",
	KeyActiveOn:                 "active {date}",
	KeyNoActivity:               "no messages yet",
	KeyYou:                      "you",
	KeyMemberFilter:             "Filter members",
	KeyThreadIn:                 "Thread · {name}",
	KeyBrowseCount:              "{n} channels",
	KeyMentionTitle:             "People",
	KeyMentionNone:              "No one matches “{query}”",
	KeyMentionNotMember:         "Not in this conversation",
	KeySelfName:                 "{name} (you)",
	KeyIntroSelf:                "This is your space. Draft messages, keep notes and park links here. Nobody else can see it.",
	KeyAddChannels:              "Add channels",
	KeyFormatToolbar:            "Formatting",
	KeyFormatBold:               "Bold",
	KeyFormatItalic:             "Italic",
	KeyFormatStrike:             "Strikethrough",
	KeyFormatCode:               "Code",
	KeyFormatLink:               "Link",
	KeyFormatBullets:            "Bulleted list",
	KeyFormatQuote:              "Quote",
	KeyMentionHint:              "↑↓ choose · Enter insert · Esc close",
	KeyViewInChannel:            "View in channel",
	KeyTodoMoreOptions:          "More options",
	KeySearchInChannel:          "In {channel}",
	KeySearchFilterHint:         "Narrow with in:#channel or from:@name",
	KeyKindPublicDesc:           "Anyone in the company can find and join it.",
	KeyKindPrivateDesc:          "Only people you invite can see it.",
	KeyKindGroupDesc:            "A private conversation with a few people.",
	KeyKindDirectDesc:           "A one-to-one conversation.",
	KeyChannelNameHint:          "Lowercase, no spaces. Use dashes, like design-reviews.",
	KeyAddPeople:                "Add people",
	KeyAddPeoplePlaceholder:     "Search by name",
	KeyAddPeopleLabel:           "People",
	KeyAddMembersTitle:          "Add people to {name}",
	KeyAddMembersSubmit:         "Add",
	KeyAddMembersPending:        "Adding…",
	KeyRemovePerson:             "Remove {name}",
	KeyStartConversation:        "Start conversation",
	KeyCreateChannel:            "Create channel",
	KeyBrowseNoMatch:            "No channels match “{query}”",
	KeyBrowseOpen:               "Open",
	KeyTodoProgress:             "{done} of {total} done",
	KeyChannelTools:             "Channel tools",
	KeyQuietSummary:             "Paused {from}–{until}",
	KeyQuietHelp:                "Messages still arrive; you just aren't notified.",
	KeyTimezoneDevice:           "{zone} (this device)",
	KeyPeopleCount:              "{n} people",
}

// EnglishCopy returns the reviewed source strings keyed by catalog key so the
// product catalog can register them once and translation audits can diff.
func EnglishCopy() map[string]string {
	out := make(map[string]string, len(englishCopy))
	for k, v := range englishCopy {
		out[k] = v
	}
	return out
}
