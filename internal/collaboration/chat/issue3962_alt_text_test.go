package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestIssue3962_ImageDescriptionTravelsWithTheAttachment proves the description
// an author wrote is kept on the post's media reference, beside the file name
// and not in place of it.
func TestIssue3962_ImageDescriptionTravelsWithTheAttachment(t *testing.T) {
	directory := &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: MediaFacts{ContentType: "image/png", ByteSize: 4096, Admitted: true}}
	s, f := mediaService(t, directory)
	ref := mediaRef()
	ref.AltText = "Bar chart: headcount by team, Sales is the tallest bar"
	if _, err := s.SendPost(context.Background(), mediaSend(ref)); err != nil {
		t.Fatalf("send with a described image = %v", err)
	}
	got := f.sent.References[0]
	if got.AltText != ref.AltText || got.Display != "comp-bands.png" || got.Decorative {
		t.Fatalf("persisted reference = %+v", got)
	}
}

// TestIssue3962_DecorativeIsAnExplicitChoice proves an author can mark an image
// decorative and that the mark is kept.
func TestIssue3962_DecorativeIsAnExplicitChoice(t *testing.T) {
	directory := &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: MediaFacts{ContentType: "image/png", ByteSize: 4096, Admitted: true}}
	s, f := mediaService(t, directory)
	ref := mediaRef()
	ref.Decorative = true
	if _, err := s.SendPost(context.Background(), mediaSend(ref)); err != nil {
		t.Fatalf("send with a decorative image = %v", err)
	}
	if got := f.sent.References[0]; !got.Decorative || got.AltText != "" {
		t.Fatalf("persisted reference = %+v", got)
	}
}

// TestIssue3962_BadDescriptionsAreRefused walks the ways a description can be
// wrong: both a description and the decorative mark, too long, and on a file
// that is not an image.
func TestIssue3962_BadDescriptionsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Reference)
	}{
		{"both description and decorative", func(r *Reference) { r.AltText, r.Decorative = "A chart", true }},
		{"too long", func(r *Reference) { r.AltText = strings.Repeat("a", MaxImageDescriptionRunes+1) }},
		{"too long in multibyte letters", func(r *Reference) { r.AltText = strings.Repeat("ä", MaxImageDescriptionRunes+1) }},
		{"on a document", func(r *Reference) { r.ContentType, r.AltText, r.Width, r.Height = "application/pdf", "A report", 0, 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := MediaFacts{ContentType: "image/png", ByteSize: 4096, Admitted: true}
			ref := mediaRef()
			if tc.name == "on a document" {
				facts = MediaFacts{ContentType: "application/pdf", ByteSize: 4096, Admitted: true}
			}
			tc.mutate(&ref)
			directory := &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: facts}
			s, f := mediaService(t, directory)
			_, err := s.SendPost(context.Background(), mediaSend(ref))
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("err = %v, want ErrInvalidArgument", err)
			}
			if f.mutations != 0 {
				t.Fatal("a refused description still wrote the post")
			}
		})
	}
}

// TestIssue3962_LimitIsInCharactersNotBytes proves a description of exactly the
// limit in multibyte letters is accepted.
func TestIssue3962_LimitIsInCharactersNotBytes(t *testing.T) {
	directory := &mediaDirectoryStub{tenant: "t1", conversation: "c1", artifact: "artifact-1", facts: MediaFacts{ContentType: "image/png", ByteSize: 4096, Admitted: true}}
	s, _ := mediaService(t, directory)
	ref := mediaRef()
	ref.AltText = strings.Repeat("ä", MaxImageDescriptionRunes)
	if _, err := s.SendPost(context.Background(), mediaSend(ref)); err != nil {
		t.Fatalf("description at the limit = %v", err)
	}
}
