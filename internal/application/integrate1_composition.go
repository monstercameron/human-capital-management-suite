package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportchat "github.com/monstercameron/human-capital-management-suite/internal/transport/chatextensions"
	"net/http"
	"strings"
)

func integrate1ChatExtensions(runtime composedChat) transportchat.Service {
	if runtime.extensions == nil {
		return nil
	}
	return &ChatModerationExtensions{ChatExtensions: runtime.extensions, Moderation: runtime.moderation}
}

func overlayIntegrate1AgentIcons(next http.Handler, surface transport.AgentIconSurface, admission transport.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, transport.AgentIconPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			http.Error(w, "request denied", denied.HTTPStatus())
			return
		}
		transport.AgentIconHandler{Surface: surface}.ServeHTTP(w, r.WithContext(ctx))
	})
}

func overlayIntegrate1Chat(next http.Handler, runtime composedChat, voice VoiceService, admission transport.Config, directories ...ChatModerationDirectory) http.Handler {
	var directory ChatModerationDirectory
	if len(directories) > 0 {
		directory = directories[0]
	}
	next = OverlaySavedMessages(next, integrate2SavedPort(runtime), admission)
	// Issue 3792: decisions recorded with /decide and listed per conversation.
	next = OverlayChatDecisions(next, chatDecisionsPort(runtime), admission)
	// Issue 3804: what the agents of a channel remember, with Forget.
	next = OverlayChatMemory(next, chatMemoryPort(runtime), admission)
	next = OverlayFollowedThreads(next, followedThreadsPort(runtime), admission)
	// Issue 3787: what is waiting on me and what I am waiting on.
	next = OverlayFollowUps(next, followUpsPort(runtime), admission)
	// Issue 3733: mentions, followed-thread replies and direct messages for the bell.
	next = OverlayChatNotifications(next, chatNotificationsPort(runtime), admission)
	// Issue 3786: the personal digest of what was held back.
	next = OverlayChatDigest(next, chatDigestPort(runtime), admission)
	// Issue 3920: the weekly quiet-hours schedule and one-off pause.
	next = OverlayChatQuietSchedule(next, chatQuietSchedulePort(runtime), admission)
	// Issue 3788: messages their author chose to send later.
	next = OverlayScheduledPosts(next, scheduledPostsPort(runtime), admission)
	next = OverlayChatClientTelemetry(next, admission, nil)
	next = OverlayChatModeration(next, ChatModerationHTTP{Service: runtime.moderation, Reports: runtime.extensions, Permissions: runtime.moderationPermissions, PageContext: runtime.service, Directory: directory}, admission)
	if runtime.service != nil {
		voice.Messages, _ = runtime.service.(VoiceMessageWriter)
	}
	if runtime.moderationStore != nil {
		voice.Transcripts = runtime.moderationStore
	}
	next = OverlayAgentReactions(next, agentReactionServiceFor(runtime), admission)
	if runtime.extensions != nil {
		next = OverlayChatAudit(next, runtime.extensions, admission)
		// Issue 3783: information barriers between groups, set by administrators.
		next = OverlayChatBarriers(next, runtime.extensions.Barriers, admission)
		// Issue 3812: conversations bound to a governed case.
		next = OverlayChatCase(next, runtime.extensions.Cases, admission)
	}
	return OverlayChatVoice(next, voice, admission)
}
