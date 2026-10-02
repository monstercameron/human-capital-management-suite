package chatrender

import "context"

type Producer interface {
	Produce(context.Context, Rendering) (Rendering, error)
}
type Registration struct {
	Kind     Kind
	Producer Producer
}

// Registry belongs to the composition root; registration never changes globals.
type Registry struct{ registrations []Registration }

func NewRegistry(registrations ...Registration) (*Registry, error) {
	r := &Registry{}
	for _, entry := range registrations {
		if entry.Kind == "" || entry.Producer == nil {
			return nil, ErrInvalid
		}
		for _, old := range r.registrations {
			if old.Kind == entry.Kind {
				return nil, ErrInvalid
			}
		}
		r.registrations = append(r.registrations, entry)
	}
	return r, nil
}
func (r *Registry) Produce(ctx context.Context, in Rendering, kinds []Kind) (Rendering, error) {
	if r == nil {
		return Rendering{}, ErrUnavailable
	}
	in.Kinds = nil
	in.Producers = nil
	// Rewording always precedes translation, independent of registration order.
	ordered := []Kind{}
	for _, k := range []Kind{Mask, Reword, Translate} {
		if hasKind(kinds, k) {
			ordered = append(ordered, k)
		}
	}
	for _, k := range kinds {
		if k != Mask && k != Reword && k != Translate {
			ordered = append(ordered, k)
		}
	}
	for _, k := range ordered {
		var p Producer
		for _, entry := range r.registrations {
			if entry.Kind == k {
				p = entry.Producer
				break
			}
		}
		if p == nil {
			return Rendering{}, ErrUnavailable
		}
		out, err := p.Produce(ctx, in)
		if err != nil {
			return Rendering{}, err
		}
		if out.Tenant != in.Tenant || out.Message != in.Message || out.Revision != in.Revision || !out.Checks.Meaning || !out.Checks.Placeholders || len(out.Checks.Failures) > 0 {
			return Rendering{}, ErrInvalid
		}
		out.Kinds = append(append([]Kind(nil), in.Kinds...), k)
		out.Producers = append(append([]ProducerIdentity(nil), in.Producers...), out.Producer)
		in = out
	}
	return in, nil
}

// FixtureProducer has no network path and only returns explicitly supplied text.
type FixtureProducer struct {
	Text     string
	Identity ProducerIdentity
	Tone     Tone
	Language string
}

func (p FixtureProducer) Produce(_ context.Context, in Rendering) (Rendering, error) {
	if p.Text == "" {
		return Rendering{}, ErrUnavailable
	}
	in.Text = p.Text
	in.Producer = p.Identity
	if in.Producer.Provider == "" {
		in.Producer.Provider = "fixture"
	}
	if in.Producer.Model == "" {
		in.Producer.Model = "deterministic"
	}
	if in.Producer.InstructionDigest == "" {
		in.Producer.InstructionDigest = "fixture:v1"
	}
	if in.Producer.GlossaryVersion == "" {
		in.Producer.GlossaryVersion = "none"
	}
	if p.Tone != "" {
		in.Tone = p.Tone
	}
	if p.Language != "" {
		in.Language = p.Language
	}
	in.Checks = Checks{Meaning: true, Placeholders: true}
	in.Confidence = 1
	return in, nil
}

// RequestsForReaders de-duplicates eager targets. Callers supply only readers
// currently present; all other readers request lazily on their first read.
func RequestsForReaders(original Rendering, readers []Preference) []Rendering {
	var targets []Rendering
	for _, p := range readers {
		r := original
		r.Text = ""
		r.Kinds = nil
		r.Tone = p.Tone
		if r.Tone == "" {
			r.Tone = AsWritten
		}
		r.Language = Language(original.SourceLanguage)
		if r.Tone == Reworded {
			r.Kinds = append(r.Kinds, Reword)
		}
		if WantsTranslation(p, original.SourceLanguage) {
			r.Language = Language(p.ReadingLanguage)
			r.Kinds = append(r.Kinds, Translate)
		}
		if len(r.Kinds) == 0 {
			continue
		}
		duplicate := false
		for _, prior := range targets {
			if prior.Tone == r.Tone && prior.Language == r.Language {
				duplicate = true
			}
		}
		if !duplicate {
			targets = append(targets, r)
		}
	}
	return targets
}
