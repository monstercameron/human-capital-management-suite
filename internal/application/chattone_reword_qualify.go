package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

// ChattoneRewordCase is one labelled heated message. Facts must survive
// exactly; Hostile lists wording that must not.
type ChattoneRewordCase struct {
	ID       string   `json:"id"`
	Language string   `json:"language"`
	Text     string   `json:"text"`
	Facts    []string `json:"facts,omitempty"`
	Hostile  []string `json:"hostile,omitempty"`
}

// ChattoneRewordSuite is the labelled suite a model is scored on before
// rewording is offered. These are synthetic messages in the product's
// languages; none is workplace text. The owner's qualification run uses a
// larger suite of the same shape from a file (see LoadChattoneRewordSuite), so
// the size of this built-in one is not what qualifies a model.
func ChattoneRewordSuite() []ChattoneRewordCase {
	return []ChattoneRewordCase{
		{"en-01", "en", "You idiot, the deploy broke prod again. @dana must fix 42 files by 2026-10-01.", []string{"@dana", "42", "2026-10-01"}, []string{"idiot"}},
		{"en-02", "en", "This report is garbage and nobody read the 3 pages of notes at https://example.com/notes.", []string{"3", "https://example.com/notes"}, []string{"garbage"}},
		{"en-03", "en", "I will not approve the $12,500 budget until Finance explains the gap. Stop wasting my time.", []string{"$12,500", "Finance"}, []string{"wasting my time"}},
		{"en-04", "en", "Shut up about the schedule already. The launch is 2026-11-03 and that is final.", []string{"2026-11-03"}, []string{"shut up"}},
		{"en-05", "en", "Your pathetic status update missed all 4 blockers on the board.", []string{"4"}, []string{"pathetic"}},
		{"en-06", "en", "Fix the typo on slide 7 before the 14:30 meeting, you useless clown.", []string{"7", "14:30"}, []string{"useless", "clown"}},
		{"en-07", "en", "No, this is NOT ready. The tests are a joke and the 12 failing cases prove it!!!", []string{"12"}, []string{"a joke"}},
		{"en-08", "en", "WHO approved this?? Send me the ticket number by 17:00 or I escalate to @morgan.", []string{"17:00", "@morgan"}, nil},
		{"de-01", "de", "Du Idiot, das Deployment ist schon wieder kaputt. Bitte repariere es bis 2026-10-01.", []string{"2026-10-01"}, []string{"idiot"}},
		{"de-02", "de", "Dieser Bericht ist lächerlich, und die 5 Fehler hätte @jonas sehen müssen.", []string{"5", "@jonas"}, []string{"lächerlich"}},
		{"de-03", "de", "Halt die Klappe, der Termin am 2026-12-01 bleibt, und nein, ich verschiebe ihn nicht.", []string{"2026-12-01"}, []string{"halt die klappe"}},
		{"de-04", "de", "Das ist völlig nutzlos! Schick mir die 3 Dateien bis 16:00.", []string{"3", "16:00"}, []string{"nutzlos"}},
		{"es-01", "es", "Eres un inútil, el informe tiene 5 errores otra vez y @lucia lo sabía.", []string{"5", "@lucia"}, []string{"inútil"}},
		{"es-02", "es", "Cállate ya, la reunión es el 2026-11-05 a las 10:00 y no se cambia.", []string{"2026-11-05", "10:00"}, []string{"cállate"}},
		{"es-03", "es", "Qué ridículo, otra vez faltan 8 facturas en https://example.com/facturas.", []string{"8", "https://example.com/facturas"}, []string{"ridículo"}},
		{"es-04", "es", "No, no voy a aprobar los 2.500 euros hasta que Finanzas responda, estúpido.", []string{"2.500", "Finanzas"}, []string{"estúpido"}},
		{"fr-01", "fr", "Tu es un imbécile, le rapport a encore 6 erreurs et @camille le savait.", []string{"6", "@camille"}, []string{"imbécile"}},
		{"fr-02", "fr", "Tais-toi, la livraison est le 2026-11-20 et je ne la reporte pas.", []string{"2026-11-20"}, []string{"tais-toi"}},
		{"fr-03", "fr", "C'est ridicule ! Envoie-moi les 4 fichiers avant 18:00.", []string{"4", "18:00"}, []string{"ridicule"}},
		{"fr-04", "fr", "Non, je n'approuve pas les 900 euros, et ton idée est inutile.", []string{"900"}, []string{"inutile"}},
		{"pt-01", "pt", "Seu idiota, o servidor caiu de novo e o @rui precisa corrigir 9 arquivos hoje.", []string{"@rui", "9"}, []string{"idiota"}},
		{"pt-02", "pt", "Isto é ridículo, a reunião de 2026-10-15 não será adiada.", []string{"2026-10-15"}, []string{"ridículo"}},
		{"ar-01", "ar", "أنت غبي، الخادم تعطل مرة أخرى وعلى @salma إصلاح 7 ملفات اليوم.", []string{"@salma", "7"}, []string{"غبي"}},
		{"ar-02", "ar", "هذا تقرير غبي، أرسل لي الملفات الـ 3 قبل 2026-10-20.", []string{"3", "2026-10-20"}, []string{"غبي"}},
	}
}

// LoadChattoneRewordSuite reads a larger suite of the same shape from JSON.
func LoadChattoneRewordSuite(raw []byte) ([]ChattoneRewordCase, error) {
	var suite []ChattoneRewordCase
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&suite); err != nil || len(suite) == 0 {
		return nil, fmt.Errorf("%w: the reword suite is not a non-empty list of labelled cases", errChattoneBinding)
	}
	seen := map[string]bool{}
	for _, c := range suite {
		if c.ID == "" || seen[c.ID] || strings.TrimSpace(c.Text) == "" || len(c.Facts) == 0 {
			return nil, fmt.Errorf("%w: reword case %q is missing an id, text or facts", errChattoneBinding, c.ID)
		}
		seen[c.ID] = true
	}
	return suite, nil
}

// ChattoneRewordRecord is the record of one scored run. It holds scores and
// digests, never message text.
type ChattoneRewordRecord struct {
	SuiteDigest   string                    `json:"suite_digest"`
	AgentVersion  string                    `json:"agent_version"`
	Model         agentmodel.ModelIdentity  `json:"model"`
	MeasuredAt    time.Time                 `json:"measured_at"`
	Cases         int                       `json:"cases"`
	Reworded      int                       `json:"reworded"`
	FactsListed   int                       `json:"facts_listed"`
	FactsKept     int                       `json:"facts_kept"`
	AddedContent  int                       `json:"cases_with_added_content"`
	HostileLeft   int                       `json:"cases_with_residual_hostility"`
	Unnatural     int                       `json:"unnatural_cases"`
	CostMicros    int64                     `json:"cost_micros"`
	Preservation  float64                   `json:"preservation"`
	Passed        bool                      `json:"passed"`
	Failures      []ChattoneRewordCaseFault `json:"failures,omitempty"`
	ReasonSummary map[string]int            `json:"fallback_reasons,omitempty"`
}

// ChattoneRewordCaseFault names a case that did not pass and why.
type ChattoneRewordCaseFault struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// ChattoneRewordPreservationFloor is the share of listed facts that must
// survive for a model to qualify (CHATTONE-002: at least 99 percent).
const ChattoneRewordPreservationFloor = 0.99

func chattoneRewordSuiteDigest(suite []ChattoneRewordCase) string {
	encoded, _ := json.Marshal(struct {
		Cases       []ChattoneRewordCase `json:"cases"`
		Instruction string               `json:"instruction"`
	}{suite, chatrewrite.InstructionDigest() + chatrewrite.RewordInstruction})
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// RunChattoneRewordQualification runs every case through the real reword
// service (heat screen, placeholders, the shared rewrite call, filters, both
// meaning checks) and scores what the service cannot: that every listed fact
// survives, that nothing was added, that no hostile wording remains, and that
// the result reads as a message. The run passes only when preservation is at
// least the floor and no case adds content or keeps hostility.
func RunChattoneRewordQualification(ctx context.Context, reworder *chatrewrite.Reworder, suite []ChattoneRewordCase, model agentmodel.ModelIdentity, now func() time.Time) ChattoneRewordRecord {
	record := ChattoneRewordRecord{SuiteDigest: chattoneRewordSuiteDigest(suite), AgentVersion: ChattoneAgentVersionDigest(), Model: model, MeasuredAt: now().UTC(), Cases: len(suite), ReasonSummary: map[string]int{}}
	identity := chatrewrite.Identity{Tenant: "qualification", Person: "qualification", Conversation: "qualification"}
	for _, c := range suite {
		record.FactsListed += len(c.Facts)
		result, err := reworder.Reword(ctx, chatrewrite.RewordRequest{Identity: identity, Text: c.Text, Context: []string{"Keep replies short and direct"}})
		fail := func(reason string) {
			record.Failures = append(record.Failures, ChattoneRewordCaseFault{ID: c.ID, Reason: reason})
		}
		switch {
		case err != nil:
			fail("the service refused: " + err.Error())
			continue
		case result.Outcome != chatrewrite.RewordDone:
			record.ReasonSummary[string(result.Outcome)+":"+result.Reason]++
			fail("not reworded (" + string(result.Outcome) + " " + result.Reason + ")")
			continue
		}
		record.Reworded++
		kept := 0
		for _, fact := range c.Facts {
			if strings.Contains(result.Text, fact) {
				kept++
			}
		}
		record.FactsKept += kept
		if kept < len(c.Facts) {
			fail("a fact did not survive")
		}
		if chattoneAddedContent(c.Text, result.Text) {
			record.AddedContent++
			fail("content was added")
		}
		lower := strings.ToLower(result.Text)
		for _, word := range c.Hostile {
			if strings.Contains(lower, strings.ToLower(word)) {
				record.HostileLeft++
				fail("hostile wording remains: " + word)
				break
			}
		}
		if reason := chattoneUnnatural(c.Text, result.Text); reason != "" {
			record.Unnatural++
			fail(reason)
		}
	}
	if record.FactsListed > 0 {
		record.Preservation = float64(record.FactsKept) / float64(record.FactsListed)
	}
	record.Passed = record.Cases > 0 && record.Reworded == record.Cases && record.Preservation >= ChattoneRewordPreservationFloor &&
		record.AddedContent == 0 && record.HostileLeft == 0 && record.Unnatural == 0
	return record
}

// chattoneAddedContent reports a rewrite carrying more apology, promise or
// agreement words, or a negation, than the original: the content a tone-only
// rewrite must not add.
func chattoneAddedContent(original, rewrite string) bool {
	o, r := chattoneWords(original), chattoneWords(rewrite)
	return chattoneAdditionCount(r) > chattoneAdditionCount(o)
}

// chattoneUnnatural is the cheap naturalness check: the rewrite is a different
// message of a plausible length, with no leftover placeholder or markup.
func chattoneUnnatural(original, rewrite string) string {
	o, r := utf8.RuneCountInString(strings.TrimSpace(original)), utf8.RuneCountInString(strings.TrimSpace(rewrite))
	switch {
	case strings.TrimSpace(rewrite) == strings.TrimSpace(original):
		return "the rewrite is the original"
	case strings.Contains(rewrite, "⟦") || strings.Contains(rewrite, "⟧") || strings.Contains(rewrite, "{") || strings.Contains(rewrite, "<untrusted"):
		return "the rewrite carries a placeholder or markup"
	case r*5 < o*2 || r*8 > o*13:
		return "the rewrite's length is implausible"
	}
	return ""
}

// ChattoneRewordRecorded is one recorded answer, kept with its placeholders as
// {n} so a replay is independent of the random placeholder text of each call.
type ChattoneRewordRecorded struct {
	Key              string `json:"key"`
	Text             string `json:"text"`
	MeaningPreserved bool   `json:"meaning_preserved"`
}

var chattoneTokenPattern = regexp.MustCompile(`⟦HCM:[a-z0-9]+:(\d+)⟧`)

// chattoneTemplate turns a prompt's draft, or a model answer, into its
// placeholder-free form: ⟦HCM:nonce:n⟧ becomes {n}.
func chattoneTemplate(text string) string { return chattoneTokenPattern.ReplaceAllString(text, "{$1}") }

// chattonePromptDraft reads the protected draft out of a prompt's data block.
func chattonePromptDraft(prompt chatrewrite.Prompt) string {
	var data struct {
		Draft string `json:"draft"`
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(prompt.Data, "<untrusted_data>\n"), "\n</untrusted_data>")
	_ = json.Unmarshal([]byte(raw), &data)
	return data.Draft
}

// ChattoneRewordRecorder wraps the model under test and records each answer in
// replay form; ChattoneRewordReplay serves those answers back. The live
// qualification records, the tests replay: a model's answers are then a
// checked-in file, not a call.
type ChattoneRewordRecorder struct {
	Model chatrewrite.CheckedModel
	mu    sync.Mutex
	rows  []ChattoneRewordRecorded
}

func (r *ChattoneRewordRecorder) Rewrite(ctx context.Context, p chatrewrite.Prompt) (string, error) {
	checked, err := r.RewriteChecked(ctx, p)
	if err != nil {
		return "", err
	}
	return checked.Text, nil
}

func (r *ChattoneRewordRecorder) RewriteChecked(ctx context.Context, p chatrewrite.Prompt) (chatrewrite.Checked, error) {
	checked, err := r.Model.RewriteChecked(ctx, p)
	if err == nil {
		r.mu.Lock()
		r.rows = append(r.rows, ChattoneRewordRecorded{Key: chattoneTemplate(chattonePromptDraft(p)), Text: chattoneTemplate(checked.Text), MeaningPreserved: checked.MeaningPreserved})
		r.mu.Unlock()
	}
	return checked, err
}

// Recorded returns what was recorded, in call order.
func (r *ChattoneRewordRecorder) Recorded() []ChattoneRewordRecorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ChattoneRewordRecorded(nil), r.rows...)
}

// ChattoneRewordReplay answers a prompt from recorded answers: the same draft
// gets the recorded text with its own placeholders put back.
type ChattoneRewordReplay struct {
	Answers map[string]ChattoneRewordRecorded
}

// NewChattoneRewordReplay indexes recorded answers by draft; the last answer
// recorded for a draft wins.
func NewChattoneRewordReplay(rows []ChattoneRewordRecorded) *ChattoneRewordReplay {
	replay := &ChattoneRewordReplay{Answers: map[string]ChattoneRewordRecorded{}}
	for _, row := range rows {
		replay.Answers[row.Key] = row
	}
	return replay
}

func (r *ChattoneRewordReplay) Rewrite(ctx context.Context, p chatrewrite.Prompt) (string, error) {
	checked, err := r.RewriteChecked(ctx, p)
	if err != nil {
		return "", err
	}
	return checked.Text, nil
}

func (r *ChattoneRewordReplay) RewriteChecked(_ context.Context, p chatrewrite.Prompt) (chatrewrite.Checked, error) {
	draft := chattonePromptDraft(p)
	row, ok := r.Answers[chattoneTemplate(draft)]
	if !ok {
		return chatrewrite.Checked{}, chatrewrite.ErrUnavailable
	}
	// Put this call's own placeholders back where the record has {n}.
	tokens := map[string]string{}
	for _, m := range chattoneTokenPattern.FindAllStringSubmatch(draft, -1) {
		tokens["{"+m[1]+"}"] = m[0]
	}
	text := row.Text
	for template, token := range tokens {
		text = strings.ReplaceAll(text, template, token)
	}
	return chatrewrite.Checked{Text: text, MeaningPreserved: row.MeaningPreserved}, nil
}

// QualifyChattoneReword scores the base deployment's named model (default
// gpt-6-luna) on a reword suite, calling the provider through the SchemaFlux
// operation with synthetic messages only. It records the model's answers
// through recorder when one is given. It is the paid half of qualification and
// is only ever run by the owner (see the live command in the lane report).
func QualifyChattoneReword(ctx context.Context, cfg ChattoneQualifyConfig, suite []ChattoneRewordCase, recorder *ChattoneRewordRecorder) (ChattoneRewordRecord, error) {
	live, identity, err := chattoneLiveModelFor(ctx, cfg)
	if err != nil {
		return ChattoneRewordRecord{}, err
	}
	var model chatrewrite.CheckedModel = live
	if recorder != nil {
		recorder.Model = live
		model = recorder
	}
	reworder, err := NewChattoneReworder(model, chattoneAllowAllPolicy{}, nil, cfg.Now)
	if err != nil {
		return ChattoneRewordRecord{}, err
	}
	record := RunChattoneRewordQualification(ctx, reworder, suite, identity, cfg.Now)
	live.mu.Lock()
	record.CostMicros = live.cost
	live.mu.Unlock()
	return record, nil
}
