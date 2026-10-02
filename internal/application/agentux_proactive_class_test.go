package application

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestAgentUXProactive_MixedDocumentClasses_Security(t *testing.T) {
	for _, pair := range [][2]dlp.DataClass{{dlp.ClassPublic, dlp.ClassInternal}, {dlp.ClassInternal, dlp.ClassPublic}, {dlp.ClassInternal, dlp.ClassInternal}} {
		class, err := announcementCombinedSourceClass(pair[0], pair[1])
		if err != nil || class != dlp.ClassInternal {
			t.Fatalf("source classification weakened for %v: %s %v", pair, class, err)
		}
	}
	if _, err := announcementCombinedSourceClass(dlp.ClassInternal, dlp.ClassCompensation); !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatalf("incompatible classes accepted: %v", err)
	}
	if _, err := announcementCombinedSourceClass("", "unknown"); !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatalf("unknown class accepted: %v", err)
	}
}
