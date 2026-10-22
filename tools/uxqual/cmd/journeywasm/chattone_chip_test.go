package main

import "testing"

// TestChattonePreviewEdited: the line above the composer's text says "(edited)"
// once the field differs from what the model wrote, and stops saying it when the
// person types the model's words back; Undo and a cleared draft forget the style.
func TestChattonePreviewEdited(t *testing.T) {
	p := chattonePreview{}
	typed := "Please keep these words"
	serial, _ := p.Begin(typed)
	if !p.Complete(serial, typed, typed, "Please retain these words") {
		t.Fatal("complete")
	}
	p.Style = "professional"
	if p.Edited() {
		t.Fatal("a fresh rewrite is marked edited")
	}
	p.Edit("Please retain these words, thanks")
	if !p.Edited() || p.Original != typed {
		t.Fatal("an edit is not noticed", p)
	}
	p.Edit("Please retain these words")
	if p.Edited() {
		t.Fatal("typing the rewrite back is still edited")
	}
	p.Edit("changed again")
	if _, ok := p.Undo(); !ok || p.Edited() || p.Style != "" || p.Output != "" {
		t.Fatal("undo kept the rewrite's state", p)
	}
	serial, _ = p.Begin(typed)
	_ = p.Complete(serial, typed, typed, "Another rewrite")
	p.Style = "concise"
	p.Edit("")
	if p.Original != "" || p.Style != "" || p.Edited() {
		t.Fatal("clearing the draft kept the rewrite", p)
	}
}
