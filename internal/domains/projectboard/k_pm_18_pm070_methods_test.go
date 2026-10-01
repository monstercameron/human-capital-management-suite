package projectboard

import (
	"errors"
	"testing"
)

func TestTodo_PM_070(t *testing.T) {
	for _, method := range []BoardMethod{MethodTaskBoard, MethodKanban, MethodScrum, MethodScrumban, MethodFeature, MethodBug, MethodDiscovery, MethodMilestone} {
		if err := ValidateBoardMethod(method); err != nil {
			t.Fatalf("supported method %q rejected: %v", method, err)
		}
		presentation, err := PresentBoardMethod(method, SurfaceRPC)
		if err != nil || !presentation.Supported || presentation.Effective != method || presentation.Label == "" {
			t.Fatalf("method %q presentation = %+v, %v", method, presentation, err)
		}
	}
}

func TestTodo_PM_070_Conformance(t *testing.T) {
	for _, method := range []BoardMethod{MethodRelease, MethodSupport, MethodProgram} {
		if !errors.Is(ValidateBoardMethod(method), ErrUnsupportedBoardMethod) {
			t.Fatalf("unsupported method %q was accepted: %v", method, ValidateBoardMethod(method))
		}
		for _, surface := range []MethodSurface{SurfaceBrowser, SurfaceRPC, SurfaceHTTP} {
			presentation, err := PresentBoardMethod(method, surface)
			if err != nil || presentation.Supported || presentation.Effective != MethodTaskBoard || presentation.Notice == "" || presentation.Label == string(method) {
				t.Fatalf("unsupported %q on %q = %+v, %v", method, surface, presentation, err)
			}
		}
	}
	if _, err := BoardMethodSpecFor(BoardMethod("NOT_A_METHOD")); !errors.Is(err, ErrUnknownBoardMethod) {
		t.Fatalf("unknown method error = %v", err)
	}
}

func TestTodo_PM_070_Browser(t *testing.T) {
	for _, method := range []BoardMethod{MethodRelease, MethodSupport, MethodProgram} {
		presentation, err := PresentBoardMethod(method, SurfaceBrowser)
		if err != nil {
			t.Fatal(err)
		}
		if presentation.Label != "Task board" || presentation.Effective != MethodTaskBoard || presentation.Supported {
			t.Fatalf("browser served unsupported method as enabled: %+v", presentation)
		}
	}
	for _, spec := range BoardMethodCatalog() {
		if spec.Supported && spec.Label == "" {
			t.Fatalf("supported method has no served label: %+v", spec)
		}
	}
}
