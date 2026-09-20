package workspace

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeJourneyNoteTrimsAndNormalizesLineEndings(t *testing.T) {
	got, err := NormalizeJourneyNote(JourneyNoteInput{Body: "  Budget confirmed.\r\nSee Q4 plan.\r  ", IdempotencyKey: " key-1 "})
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "Budget confirmed.\nSee Q4 plan." || got.IdempotencyKey != "key-1" {
		t.Fatalf("normalized = %+v", got)
	}
}

func TestNormalizeJourneyNoteRefusesWithTypedReasons(t *testing.T) {
	cases := []struct {
		name  string
		in    JourneyNoteInput
		field string
		want  string
	}{
		{"empty", JourneyNoteInput{Body: " \n\t ", IdempotencyKey: "k"}, "body", JourneyNoteReasonEmpty},
		{"too long", JourneyNoteInput{Body: strings.Repeat("é", MaxJourneyNoteRunes+1), IdempotencyKey: "k"}, "body", JourneyNoteReasonTooLong},
		{"control", JourneyNoteInput{Body: "ok\x00no", IdempotencyKey: "k"}, "body", JourneyNoteReasonInvalidText},
		{"escape", JourneyNoteInput{Body: "ok\x1b[31mred", IdempotencyKey: "k"}, "body", JourneyNoteReasonInvalidText},
		{"bidi override", JourneyNoteInput{Body: "approve \u202eevorppa", IdempotencyKey: "k"}, "body", JourneyNoteReasonInvalidText},
		{"invalid utf-8", JourneyNoteInput{Body: "bad \xff", IdempotencyKey: "k"}, "body", JourneyNoteReasonInvalidText},
		{"no key", JourneyNoteInput{Body: "fine"}, "idempotency_key", JourneyNoteReasonKeyInvalid},
		{"long key", JourneyNoteInput{Body: "fine", IdempotencyKey: strings.Repeat("k", MaxJourneyNoteKeyRunes+1)}, "idempotency_key", JourneyNoteReasonKeyInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeJourneyNote(tc.in)
			var input *JourneyInputError
			if !errors.As(err, &input) || !errors.Is(err, ErrJourneyInput) {
				t.Fatalf("err = %v, want a JourneyInputError", err)
			}
			if input.FieldPath != tc.field || input.ReasonRef != tc.want {
				t.Fatalf("refusal = %s/%s, want %s/%s", input.FieldPath, input.ReasonRef, tc.field, tc.want)
			}
		})
	}
}

func TestNormalizeJourneyNoteAcceptsBoundaryAndOrdinaryScripts(t *testing.T) {
	for _, body := range []string{
		strings.Repeat("a", MaxJourneyNoteRunes),
		"Die Finanzprüfung ist abgeschlossen.\tSiehe Anhang.",
		"تمت مراجعة الميزانية.",
	} {
		if _, err := NormalizeJourneyNote(JourneyNoteInput{Body: body, IdempotencyKey: "k"}); err != nil {
			t.Errorf("refused %q: %v", body[:min(len(body), 20)], err)
		}
	}
}
