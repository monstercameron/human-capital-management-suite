package chat

import (
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Issue 3795: a form card. An agent run (or a workflow step) that is missing
// inputs posts one card with typed fields instead of a paragraph of questions.
// The card names the one person it asks; that person fills it in once, the
// server checks every answer against the card's own field definitions, and the
// answers go back to whoever asked. The body holds the form and never an
// answer: what was answered is read from the submission, so no answer is ever
// echoed to the other people in the conversation.

// FormCardMarker opens the body of a form card. Like the other system cards it
// starts with an invisible separator (U+2063), so a client that does not know
// it shows an unremarkable line rather than markup, and a person cannot type
// one in (see sendPost).
const FormCardMarker = "⁣form-card:"

// The field types of a form card.
const (
	FormFieldText   = "text"
	FormFieldDate   = "date"
	FormFieldSelect = "select"
	FormFieldPerson = "person"
)

// Limits of a form card. A card is a short question, not a survey.
const (
	FormCardMaxFields       = 12
	FormCardMaxOptions      = 20
	FormCardMaxTextLength   = 2000
	formCardDefaultTextSize = 500
	formCardMaxBody         = 16000
)

// FormOption is one choice of a select field. Value is what the run receives;
// Label is what the person reads.
type FormOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// FormField is one typed input of a form card.
type FormField struct {
	ID       string       `json:"id"`
	Label    string       `json:"label"`
	Type     string       `json:"type"`
	Required bool         `json:"required,omitempty"`
	Options  []FormOption `json:"options,omitempty"`
	// MaxLength bounds a text answer in characters; zero means 500.
	MaxLength int `json:"max_length,omitempty"`
}

// FormCard is the form an agent asks a person to fill in. Addressee is the
// chat subject who may answer it. FormRef names the definition the fields came
// from, and RunRef the run or step waiting for the answer; both are opaque to
// Chat and handed back with the submission.
type FormCard struct {
	Title     string      `json:"title"`
	Addressee string      `json:"addressee"`
	FormRef   string      `json:"form_ref,omitempty"`
	RunRef    string      `json:"run_ref,omitempty"`
	Fields    []FormField `json:"fields"`
}

var formFieldID = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func formCardText(text string, max int) bool {
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > max || !utf8.ValidString(text) || strings.Contains(text, FormCardMarker) || strings.Contains(text, Chatcmd002BodyMarker) {
		return false
	}
	for _, c := range text {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func formCardRef(ref string) bool {
	return ref == "" || (len(ref) <= 128 && ref == strings.TrimSpace(ref) && !strings.ContainsAny(ref, "\r\n\x00") && !strings.Contains(ref, FormCardMarker))
}

// Validate reports whether the card is a form that can be asked.
func (c FormCard) Validate() error {
	if !formCardText(c.Title, 120) || !formCardText(c.Addressee, 128) || c.Addressee != strings.TrimSpace(c.Addressee) || !formCardRef(c.FormRef) || !formCardRef(c.RunRef) {
		return ErrInvalidArgument
	}
	if len(c.Fields) == 0 || len(c.Fields) > FormCardMaxFields {
		return ErrInvalidArgument
	}
	ids := map[string]bool{}
	for _, f := range c.Fields {
		if !formFieldID.MatchString(f.ID) || ids[f.ID] || !formCardText(f.Label, 100) || f.MaxLength < 0 || f.MaxLength > FormCardMaxTextLength {
			return ErrInvalidArgument
		}
		ids[f.ID] = true
		switch f.Type {
		case FormFieldText:
			if len(f.Options) != 0 {
				return ErrInvalidArgument
			}
		case FormFieldDate, FormFieldPerson:
			if len(f.Options) != 0 || f.MaxLength != 0 {
				return ErrInvalidArgument
			}
		case FormFieldSelect:
			if len(f.Options) < 2 || len(f.Options) > FormCardMaxOptions || f.MaxLength != 0 {
				return ErrInvalidArgument
			}
			values := map[string]bool{}
			for _, o := range f.Options {
				if !formCardText(o.Value, 60) || o.Value != strings.TrimSpace(o.Value) || !formCardText(o.Label, 100) || values[o.Value] {
					return ErrInvalidArgument
				}
				values[o.Value] = true
			}
		default:
			return ErrInvalidArgument
		}
	}
	return nil
}

// FormCardBody is the post body that records c.
func FormCardBody(c FormCard) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return FormCardMarker + string(raw), nil
}

// ParseFormCard reports whether body is a form card, and what it asks.
func ParseFormCard(body string) (FormCard, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(body), FormCardMarker)
	if !found || len(rest) > formCardMaxBody {
		return FormCard{}, false
	}
	var c FormCard
	decoder := json.NewDecoder(strings.NewReader(rest))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || decoder.Decode(new(any)) != io.EOF || c.Validate() != nil {
		return FormCard{}, false
	}
	return c, true
}

func carriesFormCardMarker(body string) bool { return strings.Contains(body, FormCardMarker) }

type formCardAuthorityKey struct{}

func hasFormCardAuthority(ctx context.Context) bool {
	granted, _ := ctx.Value(formCardAuthorityKey{}).(bool)
	return granted
}

// Reasons a form answer is refused, one per field. They are codes the page
// turns into a sentence in the reader's language.
const (
	FormErrRequired     = "required"
	FormErrTooLong      = "too_long"
	FormErrBadText      = "bad_text"
	FormErrBadDate      = "bad_date"
	FormErrBadOption    = "bad_option"
	FormErrUnknownField = "unknown_field"
	FormErrUnknownUser  = "unknown_person"
)

// FormValidationError carries the refusal of each field. It is an
// ErrInvalidArgument, so the edges report it as a bad request.
type FormValidationError struct{ Fields map[string]string }

func (e *FormValidationError) Error() string { return "chat: the form answers were not accepted" }
func (e *FormValidationError) Unwrap() error { return ErrInvalidArgument }

// CheckAnswers validates answers against the card's own fields and returns the
// cleaned values: text trimmed, empty optional fields left out. isPerson says
// whether a subject id may be chosen in a person field (a member of the
// conversation); it is not called for any other kind of field. Every field is
// checked, so one refusal names all the fields that need fixing.
func (c FormCard) CheckAnswers(answers map[string]string, isPerson func(subject string) (bool, error)) (map[string]string, error) {
	problems := map[string]string{}
	known := map[string]bool{}
	clean := map[string]string{}
	for _, f := range c.Fields {
		known[f.ID] = true
		value, given := answers[f.ID]
		value = strings.TrimSpace(value)
		if !given || value == "" {
			if f.Required {
				problems[f.ID] = FormErrRequired
			}
			continue
		}
		switch f.Type {
		case FormFieldText:
			max := f.MaxLength
			if max == 0 {
				max = formCardDefaultTextSize
			}
			if !utf8.ValidString(value) {
				problems[f.ID] = FormErrBadText
			} else if utf8.RuneCountInString(value) > max {
				problems[f.ID] = FormErrTooLong
			} else if strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
				problems[f.ID] = FormErrBadText
			}
		case FormFieldDate:
			if at, err := time.Parse("2006-01-02", value); err != nil || at.Format("2006-01-02") != value {
				problems[f.ID] = FormErrBadDate
			}
		case FormFieldSelect:
			allowed := false
			for _, o := range f.Options {
				allowed = allowed || o.Value == value
			}
			if !allowed {
				problems[f.ID] = FormErrBadOption
			}
		case FormFieldPerson:
			ok := false
			if isPerson != nil && len(value) <= 128 {
				var err error
				if ok, err = isPerson(value); err != nil {
					return nil, err
				}
			}
			if !ok {
				problems[f.ID] = FormErrUnknownUser
			}
		}
		clean[f.ID] = value
	}
	for id := range answers {
		if !known[id] {
			problems[id] = FormErrUnknownField
		}
	}
	if len(problems) > 0 {
		return nil, &FormValidationError{Fields: problems}
	}
	return clean, nil
}

// FormCardPost is one card asked of one person in a conversation. Sender is the
// agent (or system identity) asking; it must be allowed to post there.
type FormCardPost struct {
	TenantID       string
	Sender         Principal
	ConversationID string
	Card           FormCard
	IdempotencyKey string
}

// PostFormCard writes the card into the conversation. The sender's right to
// post is checked by the same path as any post; this method owns the shape: a
// valid card whose addressee is somebody else who is in the conversation, and a
// body no person could have typed.
func (s *Service) PostFormCard(ctx context.Context, r FormCardPost) (Post, error) {
	if err := validatePrincipal(r.Sender, r.TenantID); err != nil {
		return Post{}, err
	}
	key := strings.TrimSpace(r.IdempotencyKey)
	if r.ConversationID == "" || key == "" || len(key) > 160 || r.Card.Addressee == r.Sender.SubjectID {
		return Post{}, ErrInvalidArgument
	}
	body, err := FormCardBody(r.Card)
	if err != nil {
		return Post{}, err
	}
	// The sender must be able to see the room before it learns who is in it.
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Sender, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return Post{}, err
	}
	m, err := s.store.GetMembership(ctx, r.TenantID, r.ConversationID, r.TenantID, r.Card.Addressee)
	if err != nil || m.JoinedAt == nil || m.LeftAt != nil {
		return Post{}, ErrInvalidArgument
	}
	return s.sendPost(context.WithValue(ctx, formCardAuthorityKey{}, true),
		SendPostRequest{Principal: r.Sender, TenantID: r.TenantID, ConversationID: r.ConversationID, Body: body, IdempotencyKey: "form-card:" + key}, false)
}
