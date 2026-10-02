package chatsearch

import "strings"

// Sentence is which sentence of a transcript a voice result opens at
// (CHATSEARCH-002): the first one that holds every searched word, or failing
// that the first that holds any of them. It counts from one; zero means the
// words are in no single sentence, or there were no words, and the result
// opens at the start of the recording.
func Sentence(sentences []string, query string) int {
	words := []string{}
	for _, word := range strings.Fields(query) {
		if word != "*" {
			words = append(words, strings.ToLower(word))
		}
	}
	if len(words) == 0 {
		return 0
	}
	some := 0
	for i, sentence := range sentences {
		text := strings.ToLower(sentence)
		held := 0
		for _, word := range words {
			if strings.Contains(text, word) {
				held++
			}
		}
		if held == len(words) {
			return i + 1
		}
		if held > 0 && some == 0 {
			some = i + 1
		}
	}
	return some
}

// SameTarget reports that two results open the same thing. The sentence a
// voice result seeks to follows the words that were searched, so a recheck
// made without those words compares everything but the sentence.
func SameTarget(a, b Target) bool {
	a.Sentence, b.Sentence = 0, 0
	return a == b
}
