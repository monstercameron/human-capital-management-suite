package messagetemplate

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func promotionTemplate() Template {
	return Template{
		Key: "promotion.notice", Version: 3, Purpose: PurposePromotion, Channel: ChannelEmail,
		Locale: "en-US", Classification: "INTERNAL", LegalBasis: "hr-policy-7",
		Subject:      "Promotion decision for {{worker_name}}",
		Body:         "{{worker_name}}, your {{promotion_title}} promotion is effective {{effective_date}}. Review: {{action_url}}",
		Placeholders: []string{"worker_name", "promotion_title", "effective_date", "action_url"},
	}
}

func promotionRequest() RenderRequest {
	return RenderRequest{
		Purpose: PurposePromotion, Channel: ChannelEmail, Locale: "en-US", Classification: "INTERNAL", LegalBasis: "hr-policy-7",
		Parameters:   map[string]string{"worker_name": "Avery", "promotion_title": "Staff Engineer", "effective_date": "2026-10-01", "action_url": "https://hcm.example/review/42"},
		PersonalData: map[string]string{"worker_name": "Avery"},
	}
}

func TestTodo_MSG_004(t *testing.T) {
	r, err := promotionTemplate().Render(promotionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if r.Digest == "" || r.Subject == "" || r.Body == "" {
		t.Fatalf("incomplete render: %+v", r)
	}
	again, err := promotionTemplate().Render(promotionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if r.Digest != again.Digest || r.Subject != again.Subject || r.Body != again.Body {
		t.Fatal("identical inputs rendered different bytes")
	}
}

func TestTodo_MSG_004_Golden(t *testing.T) {
	r, err := promotionTemplate().Render(promotionRequest())
	if err != nil {
		t.Fatal(err)
	}
	want := "Promotion decision for Avery\nAvery, your Staff Engineer promotion is effective 2026-10-01. Review: https://hcm.example/review/42"
	got := r.Subject + "\n" + r.Body
	if got != want {
		t.Fatalf("golden output = %q, want %q", got, want)
	}
}

func TestTodo_MSG_004_Race(t *testing.T) {
	r := NewRegistry()
	if err := r.Publish(promotionTemplate()); err != nil {
		t.Fatal(err)
	}
	const workers, iterations = 8, 25
	start := make(chan struct{})
	errs := make(chan error, workers*(iterations+1))
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				got, err := r.Render("promotion.notice", 3, promotionRequest())
				if err != nil {
					errs <- err
					return
				}
				if got.Digest == "" || got.Subject != "Promotion decision for Avery" {
					errs <- fmt.Errorf("worker %d render %d was incomplete: %+v", worker, i, got)
					return
				}
			}
			// Concurrent publication contends with the shared registry lock and
			// must preserve the immutable published template snapshot.
			tpl := promotionTemplate()
			tpl.Key = fmt.Sprintf("promotion.notice.%d", worker)
			if err := r.Publish(tpl); err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if _, err := r.Render("promotion.notice", 3, promotionRequest()); err != nil {
		t.Fatalf("base template was lost during concurrent publication: %v", err)
	}
}

func TestTodo_MSG_004_Integration(t *testing.T) {
	r := NewRegistry()
	tpl := promotionTemplate()
	if err := r.Publish(tpl); err != nil {
		t.Fatal(err)
	}
	got, err := r.Render(tpl.Key, tpl.Version, promotionRequest())
	if err != nil || got.Digest == "" {
		t.Fatalf("registry render = %+v, %v", got, err)
	}
}

func TestTodo_MSG_004_Fault(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Template)
		want   error
	}{
		{"unknown placeholder", func(t *Template) { t.Body = "{{not_allowed}}"; t.Placeholders = []string{"not_allowed"} }, ErrUnknownPlaceholder},
		{"unsafe html", func(t *Template) { t.Body = "<script>{{worker_name}}</script>" }, ErrUnsafeContent},
		{"wrong channel", func(t *Template) { t.Channel = Channel("SMS") }, ErrChannelNotAllowed},
		{"missing legal basis", func(t *Template) { t.LegalBasis = "" }, ErrInvalidTemplate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tpl := promotionTemplate()
			tc.mutate(&tpl)
			if err := tpl.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_MSG_004_Security(t *testing.T) {
	tpl := promotionTemplate()
	request := promotionRequest()
	request.Parameters["worker_name"] = "Avery <script>"
	if _, err := tpl.Render(request); !errors.Is(err, ErrUnsafeContent) {
		t.Fatalf("markup parameter accepted: %v", err)
	}
	request = promotionRequest()
	request.Parameters["salary"] = "$100000"
	if _, err := tpl.Render(request); !errors.Is(err, ErrUnknownPlaceholder) {
		t.Fatalf("undeclared personal field accepted: %v", err)
	}
	request = promotionRequest()
	request.PersonalData["salary"] = "$100000"
	if _, err := tpl.Render(request); !errors.Is(err, ErrUnsafeContent) {
		t.Fatalf("personal data outside declaration accepted: %v", err)
	}
}

func TestTodo_MSG_004_Mutation(t *testing.T) {
	r := NewRegistry()
	tpl := promotionTemplate()
	if err := r.Publish(tpl); err != nil {
		t.Fatal(err)
	}
	if err := r.Publish(tpl); !errors.Is(err, ErrAlreadyPublished) {
		t.Fatalf("duplicate publication = %v", err)
	}
	params := promotionRequest()
	params.Parameters["worker_name"] = "Other"
	params.PersonalData["worker_name"] = "Other"
	a, err := promotionTemplate().Render(promotionRequest())
	if err != nil {
		t.Fatal(err)
	}
	b, err := promotionTemplate().Render(params)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest == b.Digest {
		t.Fatal("changed parameter did not change digest")
	}
}

func TestTodo_MSG_004_MissingParameterAndPurposeChannel(t *testing.T) {
	req := promotionRequest()
	delete(req.Parameters, "action_url")
	if _, err := promotionTemplate().Render(req); !errors.Is(err, ErrMissingParameter) {
		t.Fatalf("missing parameter = %v", err)
	}
	req = promotionRequest()
	req.Channel = ChannelSecureInbox
	if _, err := promotionTemplate().Render(req); !errors.Is(err, ErrInvalidTemplate) {
		t.Fatalf("context mismatch = %v", err)
	}
}
