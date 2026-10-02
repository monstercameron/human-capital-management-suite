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
	next = OverlayChatModeration(next, ChatModerationHTTP{Service: runtime.moderation, Reports: runtime.extensions, Permissions: runtime.moderationPermissions, PageContext: runtime.service, Directory: directory}, admission)
	if runtime.service != nil {
		voice.Messages, _ = runtime.service.(VoiceMessageWriter)
	}
	if runtime.moderationStore != nil {
		voice.Transcripts = runtime.moderationStore
	}
	return OverlayChatVoice(next, voice, admission)
}
