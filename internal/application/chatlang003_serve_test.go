package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

func TestTodo_CHATLANG_003_Serve(t *testing.T) {
	rig := newChatlangRig(t)
	rig.governance.BindEngine(ChatlangEngineInfo{})
	env := func(values map[string]string) func(string) string { return func(k string) string { return values[k] } }
	local := ServeConfig{Profile: ServeProfileLocalDev}

	// Nothing provisioned: translation is absent, not broken, and says why.
	runtime, reason, err := composeServedChatlang(context.Background(), chatlangServeInput{Config: local, Chat: rig.chat, Env: env(nil), Tenants: []string{"host"}})
	if err != nil || runtime != nil || !strings.Contains(reason, EnvChatTranslationModelConfigFile) || rig.governance.Engine().Ready {
		t.Fatalf("not provisioned: %v %q %+v", err, reason, rig.governance.Engine())
	}
	// The test engine is a local development convenience and nothing else.
	for _, profile := range []string{"", ServeProfileStandard} {
		runtime, reason, err = composeServedChatlang(context.Background(), chatlangServeInput{Config: ServeConfig{Profile: profile}, Chat: rig.chat, Env: env(map[string]string{EnvChatTranslationEngine: "fixture"}), Tenants: []string{"host"}})
		if err != nil || runtime != nil || !strings.Contains(reason, "local development") || rig.governance.Engine().Ready {
			t.Fatalf("the test engine was composed outside local development: %q %v", reason, err)
		}
	}
	// A key without a deployment document is not enough.
	runtime, reason, err = composeServedChatlang(context.Background(), chatlangServeInput{Config: local, Chat: rig.chat, Env: env(map[string]string{"MODEL_API_KEY": "key"}), Tenants: []string{"host"}})
	if err != nil || runtime != nil || reason == "" {
		t.Fatalf("a key alone composed an engine: %q %v", reason, err)
	}
	// A chat that is not composed.
	if runtime, reason, _ = composeServedChatlang(context.Background(), chatlangServeInput{Config: local, Env: env(nil)}); runtime != nil || reason == "" {
		t.Fatal("composed without chat")
	}

	runtime, reason, err = composeServedChatlang(context.Background(), chatlangServeInput{Config: local, Chat: rig.chat, Env: env(map[string]string{EnvChatTranslationEngine: "fixture"}), Tenants: []string{"host"}})
	if err != nil || runtime == nil || reason != "" || !rig.governance.Engine().Ready || !rig.governance.Engine().External {
		t.Fatalf("local test engine: %v %q %+v", err, reason, rig.governance.Engine())
	}
	if chatlangBackgroundWorkload(runtime) == nil || chatlangBackgroundWorkload(nil) != nil || chatlangBackgroundWorkload(&ChatlangRuntime{}) != nil {
		t.Fatal("the worker workload is composed only when there is a runtime and tenants")
	}

	// Run it as served: a message arrives, and a German reader has it in German
	// within a conversation's pace, with no one driving the worker by hand.
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	ctx, stop := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() { _ = runtime.Run(ctx); close(finished) }()
	defer func() { stop(); <-finished }()
	started := time.Now()
	post := rig.send("served", englishSentence)
	var text, state string
	for time.Since(started) < 5*time.Second {
		got, mark := rig.read("bruno", post)
		text, state = got.Text, mark.State
		if state == "ready" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if state != "ready" || text != "[de] "+englishSentence {
		t.Fatalf("the served worker did not translate: %q %q", state, text)
	}
	t.Logf("sent to translated rendering: %v", time.Since(started))
	// A burst: a page of messages is translated in parallel, one rendering each.
	for i := 0; i < 12; i++ {
		rig.send("burst"+string(rune('a'+i)), englishSentence+" Item "+string(rune('a'+i))+" today.")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && rig.count(`SELECT count(*) FROM chatrender_rendering WHERE language='de'`) != 13 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := rig.count(`SELECT count(*) FROM chatrender_rendering WHERE language='de'`); n != 13 {
		t.Fatalf("burst translated %d of 13", n)
	}
	if n := rig.count(`SELECT count(*) FROM chatrender_job WHERE language='de'`); n != 13 {
		t.Fatalf("one job per message per language: %d jobs for 13 messages", n)
	}
}
