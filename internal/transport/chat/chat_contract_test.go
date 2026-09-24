package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestTodo_CHAT_008 pins the canonical lifecycle RPC shapes used by clients.
func TestTodo_CHAT_008(t *testing.T) {
	service := chatv1.File_hcmnext_chat_v1_chat_service_proto.Services().ByName("ConversationService")
	if service == nil {
		t.Fatal("ConversationService is absent from the generated descriptor")
	}
	for _, want := range []struct {
		name, input, output string
		serverStreaming     bool
	}{
		{"CreateConversation", "CreateConversationRequest", "CreateConversationResponse", false},
		{"UpdateConversation", "UpdateConversationRequest", "UpdateConversationResponse", false},
		{"SendPost", "SendPostRequest", "SendPostResponse", false},
		{"WatchConversation", "WatchConversationRequest", "WatchConversationResponse", true},
	} {
		method := service.Methods().ByName(protoreflect.Name(want.name))
		if method == nil {
			t.Errorf("canonical RPC %s is missing", want.name)
			continue
		}
		if got := string(method.Input().Name()); got != want.input {
			t.Errorf("%s input = %s, want %s", want.name, got, want.input)
		}
		if got := string(method.Output().Name()); got != want.output {
			t.Errorf("%s output = %s, want %s", want.name, got, want.output)
		}
		if method.IsStreamingServer() != want.serverStreaming || method.IsStreamingClient() {
			t.Errorf("%s streaming = client:%v server:%v", want.name, method.IsStreamingClient(), method.IsStreamingServer())
		}
	}
	if got := chatv1.ConversationService_CreateConversation_FullMethodName; got != "/hcmnext.chat.v1.ConversationService/CreateConversation" {
		t.Errorf("create full method = %q", got)
	}
	if got := chatv1.ConversationService_UpdateConversation_FullMethodName; got != "/hcmnext.chat.v1.ConversationService/UpdateConversation" {
		t.Errorf("update full method = %q", got)
	}
	if got := chatv1.ConversationService_SendPost_FullMethodName; got != "/hcmnext.chat.v1.ConversationService/SendPost" {
		t.Errorf("send full method = %q", got)
	}
	if got := chatv1.ConversationService_WatchConversation_FullMethodName; got != "/hcmnext.chat.v1.ConversationService/WatchConversation" {
		t.Errorf("watch full method = %q", got)
	}
}

// TestTodo_CHAT_008_Golden records the compatibility surface and shared typed errors.
func TestTodo_CHAT_008_Golden(t *testing.T) {
	var got strings.Builder
	service := chatv1.File_hcmnext_chat_v1_chat_service_proto.Services().ByName("ConversationService")
	for _, name := range []string{"CreateConversation", "UpdateConversation", "SendPost", "WatchConversation"} {
		method := service.Methods().ByName(protoreflect.Name(name))
		if method == nil {
			t.Fatalf("canonical RPC %s is missing", name)
		}
		stream := "unary"
		if method.IsStreamingServer() {
			stream = "server_stream"
		}
		fmt.Fprintf(&got, "rpc %s(%s) returns (%s) %s\n", name, method.Input().FullName(), method.Output().FullName(), stream)
	}
	for _, name := range []string{"ERROR_CODE_INVALID_ARGUMENT", "ERROR_CODE_UNAUTHENTICATED", "ERROR_CODE_PERMISSION_DENIED", "ERROR_CODE_NOT_FOUND", "ERROR_CODE_ALREADY_EXISTS", "ERROR_CODE_ABORTED", "ERROR_CODE_FAILED_PRECONDITION", "ERROR_CODE_RESOURCE_EXHAUSTED", "ERROR_CODE_DEADLINE_EXCEEDED", "ERROR_CODE_UNAVAILABLE"} {
		value := commonv1.ErrorCode_value[name]
		got.WriteString(fmt.Sprintf("error %s=%d\n", name, value))
	}
	detail := (&commonv1.ErrorDetail{}).ProtoReflect().Descriptor()
	for _, fieldName := range []string{"code", "reason_ref", "field_violations", "retryable", "retry_after_seconds"} {
		field := detail.Fields().ByName(protoreflect.Name(fieldName))
		if field == nil {
			t.Fatalf("typed error field %s missing", fieldName)
		}
		got.WriteString(fmt.Sprintf("detail %s=%d\n", fieldName, field.Number()))
	}
	want, err := os.ReadFile(filepath.Join("testdata", "chat_rpc.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Fatalf("chat RPC compatibility surface changed; got:\n%s", got.String())
	}
}
