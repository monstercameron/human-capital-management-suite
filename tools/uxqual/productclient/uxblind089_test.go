package productclient

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_UXBLIND_089_ReadObservations is the RED for the cold-load
// window's missing split: the loader marked the window's edges but the
// three reads inside it (journeys, workers, preferences) were one
// undifferentiated span. Each read now reports its own name, latency and
// error to the service's observer, so the next cold-load run ranks them
// instead of guessing.
func TestTodo_UXBLIND_089_ReadObservations(t *testing.T) {
	var mu sync.Mutex
	var observed []ReadObservation
	observer := func(observation ReadObservation) {
		mu.Lock()
		defer mu.Unlock()
		observed = append(observed, observation)
	}
	journeysErr := errors.New("journeys store down")
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			time.Sleep(10 * time.Millisecond)
			return nil, journeysErr
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			return &journeyv1.GetProductPreferencesResponse{}, nil
		},
		ObserveRead: observer,
	}
	if _, err := Load(context.Background(), service, Session{}, State{Page: productui.PageHome}); err == nil {
		t.Fatal("Load succeeded while the journeys read failed")
	}
	mu.Lock()
	defer mu.Unlock()
	byName := map[string]ReadObservation{}
	for _, observation := range observed {
		if _, dupe := byName[observation.Name]; dupe {
			t.Fatalf("read %q reported %d times, want once", observation.Name, 2)
		}
		byName[observation.Name] = observation
	}
	for _, name := range []string{"journeys", "workers", "preferences"} {
		observation, ok := byName[name]
		if !ok {
			t.Fatalf("read %q was never observed", name)
		}
		if observation.Duration < 0 {
			t.Fatalf("read %q reported a negative latency", name)
		}
	}
	if byName["journeys"].Duration < 10*time.Millisecond {
		t.Fatalf("journeys latency = %s, want at least the read's 10 ms", byName["journeys"].Duration)
	}
	if !errors.Is(byName["journeys"].Err, journeysErr) {
		t.Fatalf("journeys error = %v, want the read's failure", byName["journeys"].Err)
	}
	for _, name := range []string{"workers", "preferences"} {
		if byName[name].Err != nil {
			t.Fatalf("read %q error = %v, want nil", name, byName[name].Err)
		}
	}
}
