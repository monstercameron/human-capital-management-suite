package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/industrypack"
)

func TestIndustryPackLifecycleRequiresComposedDependencies(t *testing.T) {
	if _, err := NewIndustryPackLifecycle(nil, nil, nil); !errors.Is(err, ErrIndustryPackLifecycleUnavailable) {
		t.Fatalf("nil dependencies error=%v", err)
	}
	var lifecycle *IndustryPackLifecycle
	if _, _, err := lifecycle.Publish(context.Background(), "tenant-a", industrypack.PublicationCheck{}); !errors.Is(err, ErrIndustryPackLifecycleUnavailable) {
		t.Fatalf("nil lifecycle publish error=%v", err)
	}
	if _, _, _, err := lifecycle.Activate(context.Background(), industrypack.IndustryComposition{}, industrypack.ActivationRequest{}); !errors.Is(err, ErrIndustryPackLifecycleUnavailable) {
		t.Fatalf("nil lifecycle activate error=%v", err)
	}
}
