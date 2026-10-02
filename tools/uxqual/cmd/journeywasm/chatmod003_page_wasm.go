//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type modPageProps struct {
	Config journeyclient.Config
	Model  chatui.Model
	// Workspace opens the workspace's own settings instead of the channel's.
	Workspace bool
}

// modPageState is what the panel shows between reads. A read that fails never
// clears Definitions or Enablements: the controls the server already delivered
// stay, marked as the last ones that loaded.
type modPageState struct {
	Definitions []chatfilter.Definition
	Enablements []chatfilter.Enablement
	Delivered   bool
	Loading     bool
	LoadError   string
	Busy        bool
	Trying      bool
	Status      string
	StatusNote  string
	StatusError bool
	Revision    int
	Result      *chatfilter.Result
	TrySample   string
	TryError    string
	// Hits is what the filters caught, read only when asked for. A read that
	// fails keeps the matches already shown.
	Hits        []chatfilter.Record
	HitsLoaded  bool
	HitsLoading bool
	HitsOlder   bool
	HitsError   string
}

func modAdminPage(props modPageProps) ui.Node {
	state := ui.UseState(modPageState{Loading: true})
	channel := props.Model.SelectedID
	if props.Workspace {
		channel = ""
	}
	bearer := props.Config.Bearer
	client := FilterAPIClient{BaseURL: filterBaseURL(props.Config.TunnelURL), Headers: func() http.Header { return http.Header{"Authorization": []string{"Bearer " + bearer}} }}
	// read replaces what is shown with a fresh reading, or keeps what was shown
	// and says the reading failed.
	read := func(ctx context.Context) bool {
		snapshot, err := client.Snapshot(ctx, channel)
		if ctx.Err() != nil && err != nil {
			return false
		}
		state.Update(func(s modPageState) modPageState {
			s.Loading = false
			if err != nil {
				s.LoadError = chatui.ModErrorKey(filterErrorCode(err))
				return s
			}
			s.Definitions, s.Enablements, s.Delivered, s.LoadError = snapshot.Definitions, snapshot.Enablements, true, ""
			return s
		})
		return err == nil
	}
	ui.UseEffectOf(func() func() {
		ctx, cancel := context.WithCancel(context.Background())
		go read(ctx)
		return cancel
	}, struct {
		Tenant, Subject, Channel string
		Workspace                bool
	}{props.Config.Tenant, props.Config.Subject, channel, props.Workspace})
	// write runs one change, then reads again. A refused change leaves the
	// controls as they were and says why in plain words.
	write := func(failNote string, work func(context.Context) (created bool, err error), done func(bool)) {
		state.Update(func(s modPageState) modPageState {
			s.Busy, s.Status, s.StatusNote, s.StatusError = true, "", "", false
			return s
		})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			created, err := work(ctx)
			if err != nil && !created {
				state.Update(func(s modPageState) modPageState {
					s.Busy, s.Revision, s.Status, s.StatusNote, s.StatusError = false, s.Revision+1, chatui.ModErrorKey(filterErrorCode(err)), failNote, true
					return s
				})
				if done != nil {
					done(false)
				}
				return
			}
			read(ctx)
			state.Update(func(s modPageState) modPageState {
				s.Busy, s.Revision, s.Status, s.StatusNote, s.StatusError = false, s.Revision+1, "st_saved", "", false
				if err != nil {
					s.Status, s.StatusError = "st_saved_not_on", true
				}
				return s
			})
			if done != nil {
				done(true)
			}
		}()
	}
	// loadHits reads the newest matches, or the page after the ones shown.
	loadHits := func(older bool) {
		before := int64(0)
		if held := state.Get().Hits; older {
			before = chatmod003OlderCursor(held)
		}
		state.Update(func(s modPageState) modPageState { s.HitsLoading, s.HitsError = true, ""; return s })
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			records, err := client.SearchHits(ctx, "", channel, before)
			state.Update(func(s modPageState) modPageState {
				s.HitsLoading = false
				if err != nil {
					s.HitsError = chatui.ModErrorKey(filterErrorCode(err))
					return s
				}
				s.Hits, s.HitsOlder, s.HitsLoaded = chatmod003MergeHits(s.Hits, records, before), len(records) >= chatmod003HitsPage, true
				return s
			})
		}()
	}
	snapshot := state.Get()
	return chatui.ModAdminPanel(chatui.ModAdminProps{Model: props.Model, Workspace: props.Workspace, Channel: channel, Definitions: snapshot.Definitions, Enablements: snapshot.Enablements, Now: time.Now(),
		Hits:    &chatui.ModHitsProps{Records: snapshot.Hits, Loaded: snapshot.HitsLoaded, Loading: snapshot.HitsLoading, Older: snapshot.HitsOlder, Error: snapshot.HitsError, Load: loadHits},
		Loading: snapshot.Loading, Busy: snapshot.Busy, Trying: snapshot.Trying, LoadError: snapshot.LoadError, CanManage: true,
		Status: snapshot.Status, StatusNote: snapshot.StatusNote, StatusError: snapshot.StatusError, Revision: snapshot.Revision,
		Result: snapshot.Result, TrySample: snapshot.TrySample, TryError: snapshot.TryError,
		Retry: func() {
			state.Update(func(s modPageState) modPageState { s.Loading, s.LoadError = true, ""; return s })
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				read(ctx)
			}()
		},
		Switch: func(request chatui.ModSwitch) {
			write("sf_switch", func(ctx context.Context) (bool, error) { return false, client.Apply(ctx, request) }, nil)
		},
		Save: func(d chatfilter.Definition, dry bool, done func(bool)) {
			write("sf_save", func(ctx context.Context) (bool, error) { return client.SaveFilter(ctx, d, dry) }, done)
		},
		Try: func(d chatfilter.Definition, sample string) {
			state.Update(func(s modPageState) modPageState {
				s.Trying, s.Result, s.TryError, s.TrySample = true, nil, "", sample
				return s
			})
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				result, err := client.Try(ctx, d, channel, sample)
				state.Update(func(s modPageState) modPageState {
					s.Trying = false
					if err != nil {
						s.TryError = chatui.ModErrorKey(filterErrorCode(err))
						return s
					}
					s.Result = &result
					return s
				})
			}()
		},
	})
}

// ChatFilterPage is the filter panel of the open conversation, drawn inside
// Conversation details.
func ChatFilterPage(cfg journeyclient.Config, model chatui.Model) ui.Node {
	return ui.CreateElement(modAdminPage, modPageProps{Config: cfg, Model: model})
}

// ChatWorkspaceFilterPage is the same panel for the whole workspace, for a
// workspace administrator.
func ChatWorkspaceFilterPage(cfg journeyclient.Config, model chatui.Model) ui.Node {
	return ui.CreateElement(modAdminPage, modPageProps{Config: cfg, Model: model, Workspace: true})
}
