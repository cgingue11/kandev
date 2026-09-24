package codexappserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/server/adapter/transport/shared"
	agenttypes "github.com/kandev/kandev/internal/agentctl/types"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/common/logger"
)

func TestConversationLifecycleAndMCPOverlay(t *testing.T) {
	server := newProtocolServer(t, func(req map[string]json.RawMessage, write func(any) error) error {
		method := readString(req, "method")
		id := req["id"]
		switch method {
		case "initialize":
			return write(resultFrame(id, map[string]any{"userAgent": "Codex CLI 0.154.0"}))
		case "initialized":
			return nil
		case "model/list":
			return write(resultFrame(id, map[string]any{"data": []any{map[string]any{"model": "gpt-5", "displayName": "GPT-5", "isDefault": true}}}))
		case "thread/start":
			var params struct {
				ApprovalPolicy string         `json:"approvalPolicy"`
				Sandbox        string         `json:"sandbox"`
				Config         map[string]any `json:"config"`
			}
			if err := json.Unmarshal(req["params"], &params); err != nil {
				return err
			}
			if params.ApprovalPolicy != "on-request" || params.Sandbox != "workspace-write" {
				t.Errorf("thread/start policy = %q, sandbox = %q", params.ApprovalPolicy, params.Sandbox)
			}
			servers, _ := params.Config["mcp_servers"].(map[string]any)
			if servers["workspace"] == nil {
				t.Errorf("thread/start config did not include MCP servers: %#v", params.Config)
			}
			return write(resultFrame(id, map[string]any{
				"thread": map[string]any{"id": "thread-1"},
				"model":  "gpt-5",
			}))
		case "turn/start":
			if err := write(map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1"}}); err != nil {
				return err
			}
			if err := write(map[string]any{"jsonrpc": "2.0", "method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "message-1", "delta": "hello"}}); err != nil {
				return err
			}
			if err := write(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "turn": map[string]any{"id": "turn-1", "status": "completed"}}}); err != nil {
				return err
			}
			return write(resultFrame(id, map[string]any{"turn": map[string]any{"id": "turn-1", "status": "completed"}}))
		default:
			return write(errorFrame(id, -32601, "unsupported"))
		}
	})
	defer server.close()

	adapter := NewAdapter(&shared.Config{WorkDir: "/workspace"}, logger.Default())
	defer func() { _ = adapter.Close() }()
	if err := adapter.Connect(server.clientWriter, server.clientReader); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	if err := adapter.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if info := adapter.GetAgentInfo(); info == nil || info.Version != "codex-cli 0.154.0" {
		t.Fatalf("agent info = %#v", info)
	}
	if _, err := adapter.NewSession(ctx, []agenttypes.McpServer{{Name: "workspace", Type: "stdio", Command: "mcp", Args: []string{"serve"}}}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := adapter.Prompt(ctx, "Say hello", nil, 42); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	var text strings.Builder
	var completeCount int
	deadline := time.After(time.Second * 3)
	for completeCount == 0 {
		select {
		case event := <-adapter.Updates():
			switch event.Type {
			case streams.EventTypeMessageChunk:
				text.WriteString(event.Text)
				if event.PromptGeneration != 0 && event.PromptGeneration != 42 {
					t.Errorf("prompt generation = %d, want 42", event.PromptGeneration)
				}
			case streams.EventTypeComplete:
				completeCount++
				if event.PromptGeneration != 42 {
					t.Errorf("completion generation = %d, want 42", event.PromptGeneration)
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for turn completion")
		}
	}
	if text.String() != "hello" {
		t.Fatalf("message text = %q, want hello", text.String())
	}
	// The terminal notification and RPC response describe the same turn.
	select {
	case event := <-adapter.Updates():
		if event.Type == streams.EventTypeComplete {
			t.Fatal("turn completion was emitted twice")
		}
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPromptRejectsConcurrentStartBeforeTurnStarted(t *testing.T) {
	turnStartReceived := make(chan struct{}, 1)
	turnStartHandled := make(chan struct{}, 2)
	releaseTurnStart := make(chan struct{})
	var releaseOnce sync.Once
	var turnStartCount int
	server := newProtocolServer(t, func(req map[string]json.RawMessage, write func(any) error) error {
		id := req["id"]
		switch readString(req, "method") {
		case "thread/start":
			return write(resultFrame(id, map[string]any{"thread": map[string]any{"id": "thread-1"}}))
		case "turn/start":
			turnStartCount++
			if turnStartCount == 1 {
				turnStartReceived <- struct{}{}
				<-releaseTurnStart
			}
			turnStartHandled <- struct{}{}
			return nil
		default:
			return write(errorFrame(id, -32601, "unsupported"))
		}
	})

	adapter := NewAdapter(&shared.Config{}, logger.Default())
	secondAccepted := false
	defer func() {
		releaseOnce.Do(func() { close(releaseTurnStart) })
		_ = adapter.Close()
		server.close()
	}()
	if err := adapter.Connect(server.clientWriter, server.clientReader); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := adapter.NewSession(ctx, nil); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := adapter.Prompt(ctx, "first", nil, 1); err != nil {
		t.Fatalf("first Prompt: %v", err)
	}
	select {
	case <-turnStartReceived:
	case <-ctx.Done():
		t.Fatal("first turn/start did not reach the server")
	}
	secondErr := adapter.Prompt(ctx, "second", nil, 2)
	secondAccepted = secondErr == nil
	adapter.mu.RLock()
	generation := adapter.activeGeneration
	adapter.mu.RUnlock()
	releaseOnce.Do(func() { close(releaseTurnStart) })
	expectedStarts := 1
	if secondAccepted {
		expectedStarts++
	}
	for range expectedStarts {
		select {
		case <-turnStartHandled:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for queued turn/start to drain")
		}
	}
	if secondErr == nil {
		t.Fatal("second Prompt was accepted before turn/started identified the active turn")
	}
	if generation != 1 {
		t.Fatalf("active generation = %d, want the first prompt's generation 1", generation)
	}
}

func TestBindingReplaysBufferedChildActivityAndClearsIt(t *testing.T) {
	adapter := NewAdapter(&shared.Config{}, logger.Default())
	defer func() { _ = adapter.Close() }()

	adapter.emitSubagentActivity(map[string]any{"agentThreadId": "child-1", "kind": "started"})
	adapter.emitCollabToolCall(map[string]any{
		"id": "spawn-1", "tool": "spawnAgent", "prompt": "inspect", "status": "inProgress",
		"receiverThreadIds": []any{"child-1"},
	}, "root", "turn-root", false)
	first := <-adapter.Updates()
	if first.Type != streams.EventTypeToolCall {
		t.Fatalf("first event type = %q, want tool call", first.Type)
	}
	var replay streams.AgentEvent
	select {
	case replay = <-adapter.Updates():
	default:
		t.Fatal("buffered child activity was not replayed when its binding arrived")
	}
	if replay.Type != streams.EventTypeToolUpdate || replay.ToolCallID != "spawn-1" || replay.ToolStatus != childStatusRunning {
		t.Fatalf("replayed child activity = %#v", replay)
	}

	adapter.emitChildStatus("child-1", childStatusCompleted)
	completed := <-adapter.Updates()
	if completed.ToolStatus != childStatusCompleted {
		t.Fatalf("child completion status = %q, want completed", completed.ToolStatus)
	}
	adapter.emitCollabToolCall(map[string]any{
		"id": "spawn-1", "tool": "spawnAgent", "prompt": "inspect", "status": "completed",
		"receiverThreadIds": []any{"child-1"},
	}, "root", "turn-root", true)
	parentComplete := <-adapter.Updates()
	if parentComplete.ToolStatus != childStatusCompleted {
		t.Fatalf("parent completion status = %q, want completed", parentComplete.ToolStatus)
	}
	adapter.mu.RLock()
	active := hasActiveChild(adapter.childStatuses, adapter.earlyChildActivities)
	adapter.mu.RUnlock()
	if active {
		t.Fatal("terminal child remained active after buffered activity was drained")
	}
}

func TestApprovalResolutionMapsToNativeDecision(t *testing.T) {
	server := newProtocolServer(t, func(req map[string]json.RawMessage, write func(any) error) error {
		method := readString(req, "method")
		id := req["id"]
		switch method {
		case "initialize":
			if err := write(resultFrame(id, map[string]any{"userAgent": "Codex"})); err != nil {
				return err
			}
			return write(map[string]any{"jsonrpc": "2.0", "id": 777, "method": "item/commandExecution/requestApproval", "params": map[string]any{
				"threadId": "thread-approval", "turnId": "turn-approval", "itemId": "item-approval", "command": []string{"go", "test", "./..."}, "cwd": "/workspace",
			}})
		case "initialized":
			return nil
		case "model/list":
			return write(resultFrame(id, map[string]any{"data": []any{}}))
		default:
			return write(errorFrame(id, -32601, "unsupported"))
		}
	})
	defer server.close()

	adapter := NewAdapter(&shared.Config{}, logger.Default())
	defer func() { _ = adapter.Close() }()
	if err := adapter.Connect(server.clientWriter, server.clientReader); err != nil {
		t.Fatal(err)
	}
	adapter.SetPermissionHandler(func(_ context.Context, req *agenttypes.PermissionRequest) (*agenttypes.PermissionResponse, error) {
		if req.SessionID != "thread-approval" || req.ToolCallID != "item-approval" || req.ActionType != string(streams.ActionTypeCommand) {
			t.Errorf("permission request = %#v", req)
		}
		return &agenttypes.PermissionResponse{OptionID: "allow-once"}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	if err := adapter.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	select {
	case frame := <-server.responses:
		var result struct {
			Decision string `json:"decision"`
		}
		if err := json.Unmarshal(frame["result"], &result); err != nil {
			t.Fatalf("decode permission response: %v", err)
		}
		if result.Decision != "accept" {
			t.Fatalf("decision = %q, want accept", result.Decision)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for approval response")
	}
}

func TestChildOutputRemainsScopedAfterParentTurnCompletes(t *testing.T) {
	allowChild := make(chan struct{})
	turnResponseSent := make(chan struct{})
	backgroundListResponded := make(chan struct{}, 1)
	server := newProtocolServer(t, func(req map[string]json.RawMessage, write func(any) error) error {
		method := readString(req, "method")
		id := req["id"]
		switch method {
		case "initialize":
			return write(resultFrame(id, map[string]any{"userAgent": "Codex CLI 0.154.0"}))
		case "initialized":
			return nil
		case "model/list":
			return write(resultFrame(id, map[string]any{"data": []any{}}))
		case "thread/start":
			return write(resultFrame(id, map[string]any{"thread": map[string]any{"id": "root"}}))
		case "turn/start":
			if err := write(map[string]any{"jsonrpc": "2.0", "method": "item/started", "params": map[string]any{
				"threadId": "root", "turnId": "root-turn", "item": map[string]any{
					"id": "spawn-1", "type": "collabAgentToolCall", "tool": "spawnAgent", "prompt": "inspect", "status": "inProgress", "receiverThreadIds": []string{"child"}, "agentsStates": map[string]any{},
				},
			}}); err != nil {
				return err
			}
			if err := write(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": "root", "turnId": "root-turn", "turn": map[string]any{"id": "root-turn", "status": "completed"}}}); err != nil {
				return err
			}
			<-allowChild
			if err := write(map[string]any{"jsonrpc": "2.0", "method": "item/agentMessage/delta", "params": map[string]any{"threadId": "child", "turnId": "child-turn", "itemId": "child-message", "delta": "still working"}}); err != nil {
				return err
			}
			if err := write(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": "child", "turnId": "child-turn", "turn": map[string]any{"id": "child-turn", "status": "completed"}}}); err != nil {
				return err
			}
			err := write(resultFrame(id, map[string]any{"turn": map[string]any{"id": "root-turn", "status": "completed"}}))
			close(turnResponseSent)
			return err
		case "thread/backgroundTerminals/list":
			err := write(resultFrame(id, map[string]any{"data": []any{}}))
			backgroundListResponded <- struct{}{}
			return err
		default:
			return write(errorFrame(id, -32601, "unsupported"))
		}
	})
	defer server.close()

	adapter := NewAdapter(&shared.Config{}, logger.Default())
	defer func() { _ = adapter.Close() }()
	if err := adapter.Connect(server.clientWriter, server.clientReader); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := adapter.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.NewSession(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prompt(ctx, "start", nil, 9); err != nil {
		t.Fatal(err)
	}
	var sawRootComplete bool
	var sawChildMessage bool
	deadline := time.After(2 * time.Second)
	for !sawRootComplete {
		select {
		case event := <-adapter.Updates():
			if event.Type == streams.EventTypeComplete && event.SessionID == "root" {
				sawRootComplete = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for root completion")
		}
	}
	close(allowChild)
	for !sawChildMessage {
		select {
		case event := <-adapter.Updates():
			if event.Type == streams.EventTypeMessageChunk && event.ProtocolMessageID == "child-message" {
				sawChildMessage = true
				if event.SessionID != "root" || event.ParentToolCallID != "spawn-1" || event.Text != "still working" {
					t.Fatalf("child event lost root ownership: %#v", event)
				}
			}
			if event.Type == streams.EventTypeComplete && event.SessionID == "root" {
				t.Fatal("child completion emitted a second root completion")
			}
		case <-deadline:
			t.Fatal("timed out waiting for child output")
		}
	}
	select {
	case <-turnResponseSent:
	case <-ctx.Done():
		t.Fatal("timed out waiting for turn/start response")
	}
	select {
	case <-backgroundListResponded:
	case <-ctx.Done():
		t.Fatal("timed out waiting for background-terminal reconciliation")
	}
}

func TestResumeRestoresNativeChildBindingFromThreadHistory(t *testing.T) {
	server := newProtocolServer(t, func(req map[string]json.RawMessage, write func(any) error) error {
		method := readString(req, "method")
		id := req["id"]
		switch method {
		case "initialize":
			return write(resultFrame(id, map[string]any{"userAgent": "Codex CLI 0.154.0"}))
		case "initialized":
			return nil
		case "model/list":
			return write(resultFrame(id, map[string]any{"data": []any{}}))
		case "thread/resume":
			return write(resultFrame(id, map[string]any{"thread": map[string]any{"id": "root"}}))
		case "thread/read":
			return write(resultFrame(id, map[string]any{"thread": map[string]any{"id": "root", "turns": []any{map[string]any{"id": "past-turn", "items": []any{map[string]any{
				"id": "spawn-2", "type": "collabAgentToolCall", "tool": "spawnAgent", "prompt": "review", "status": "inProgress", "receiverThreadIds": []string{"child-2"}, "agentsStates": map[string]any{},
			}}}}}}))
		case "thread/backgroundTerminals/list":
			return write(resultFrame(id, map[string]any{"data": []any{}}))
		default:
			return write(errorFrame(id, -32601, "unsupported"))
		}
	})
	defer server.close()

	adapter := NewAdapter(&shared.Config{}, logger.Default())
	defer func() { _ = adapter.Close() }()
	if err := adapter.Connect(server.clientWriter, server.clientReader); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := adapter.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := adapter.LoadSession(ctx, "root", nil); err != nil {
		t.Fatal(err)
	}
	adapter.handleNotification(ctx, "item/agentMessage/delta", json.RawMessage(`{"threadId":"child-2","turnId":"child-turn","itemId":"child-message","delta":"replayed child"}`))
	select {
	case event := <-adapter.Updates():
		if event.Type != streams.EventTypeSessionStatus && event.Type != "session_models" {
			if event.Type == streams.EventTypeMessageChunk {
				if event.SessionID != "root" || event.ParentToolCallID != "spawn-2" {
					t.Fatalf("resumed child event lost binding: %#v", event)
				}
				return
			}
		}
	default:
	}
	for {
		select {
		case event := <-adapter.Updates():
			if event.Type == streams.EventTypeMessageChunk {
				if event.SessionID != "root" || event.ParentToolCallID != "spawn-2" {
					t.Fatalf("resumed child event lost binding: %#v", event)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for resumed child output")
		}
	}
}

func TestCompletedCommandExecutionPreservesOutputAndExitCode(t *testing.T) {
	adapter := NewAdapter(&shared.Config{}, logger.Default())
	defer func() { _ = adapter.Close() }()

	adapter.emitItem(map[string]any{
		"item": map[string]any{
			"id": "command-1", "type": "commandExecution", "command": "go test ./...",
			"cwd": "/workspace", "status": "failed", "aggregatedOutput": "FAIL package\n", "exitCode": float64(7),
		},
	}, "root", "root", "", "turn-1", true)
	event := <-adapter.Updates()
	if event.Type != streams.EventTypeToolUpdate || event.ToolStatus != "failed" {
		t.Fatalf("completed command event = %#v", event)
	}
	if event.NormalizedPayload == nil || event.NormalizedPayload.ShellExec() == nil || event.NormalizedPayload.ShellExec().Output == nil {
		t.Fatalf("completed command output missing: %#v", event.NormalizedPayload)
	}
	output := event.NormalizedPayload.ShellExec().Output
	if output.Stdout != "FAIL package\n" || output.ExitCode == nil || *output.ExitCode != 7 {
		t.Fatalf("command output = %#v, want aggregate output and exit code 7", output)
	}
}

func TestBackgroundTerminalDisappearanceEmitsCompletion(t *testing.T) {
	listCalls := 0
	server := newProtocolServer(t, func(req map[string]json.RawMessage, write func(any) error) error {
		method := readString(req, "method")
		id := req["id"]
		switch method {
		case "initialize":
			return write(resultFrame(id, map[string]any{"userAgent": "Codex CLI 0.154.0"}))
		case "initialized":
			return nil
		case "model/list":
			return write(resultFrame(id, map[string]any{"data": []any{}}))
		case "thread/start":
			return write(resultFrame(id, map[string]any{"thread": map[string]any{"id": "root"}}))
		case "thread/backgroundTerminals/list":
			listCalls++
			if listCalls == 1 {
				return write(resultFrame(id, map[string]any{"data": []any{map[string]any{"command": "sleep 1", "cwd": "/workspace", "itemId": "cmd-1", "processId": "proc-1"}}}))
			}
			return write(resultFrame(id, map[string]any{"data": []any{}}))
		default:
			return write(errorFrame(id, -32601, "unsupported"))
		}
	})
	defer server.close()

	adapter := NewAdapter(&shared.Config{}, logger.Default())
	defer func() { _ = adapter.Close() }()
	if err := adapter.Connect(server.clientWriter, server.clientReader); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := adapter.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.NewSession(ctx, nil); err != nil {
		t.Fatal(err)
	}
	adapter.refreshBackgroundTerminals(ctx, "root")
	adapter.refreshBackgroundTerminals(ctx, "root")
	var sawStart, sawComplete bool
	for !sawComplete {
		select {
		case event := <-adapter.Updates():
			if event.ToolCallID == "cmd-1" && event.Type == streams.EventTypeToolCall {
				sawStart = true
				if event.NormalizedPayload == nil || !event.NormalizedPayload.IsActiveBackgroundWork() {
					t.Fatalf("background start lacks lifecycle identity: %#v", event)
				}
			}
			if event.Type == streams.EventTypeBackgroundComplete && event.ToolCallID == "cmd-1" {
				sawComplete = true
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for background completion")
		}
	}
	if !sawStart {
		t.Fatal("missing background start event")
	}
}

type protocolServer struct {
	clientWriter io.WriteCloser
	clientReader io.Reader
	responses    chan map[string]json.RawMessage
	close        func()
}

func newProtocolServer(t *testing.T, handle func(map[string]json.RawMessage, func(any) error) error) *protocolServer {
	t.Helper()
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	responses := make(chan map[string]json.RawMessage, 8)
	done := make(chan struct{})
	var closeOnce sync.Once
	write := func(frame any) error {
		data, err := json.Marshal(frame)
		if err != nil {
			return err
		}
		_, err = serverOutput.Write(append(data, '\n'))
		return err
	}
	go func() {
		defer close(done)
		defer close(responses)
		scanner := bufio.NewScanner(serverInput)
		for scanner.Scan() {
			var request map[string]json.RawMessage
			if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
				return
			}
			var method string
			_ = json.Unmarshal(request["method"], &method)
			if method == "" {
				select {
				case responses <- request:
				case <-done:
					return
				}
				continue
			}
			if err := handle(request, write); err != nil {
				t.Errorf("fake app-server: %v", err)
				return
			}
		}
	}()
	return &protocolServer{
		clientWriter: clientInput,
		clientReader: clientOutput,
		responses:    responses,
		close: func() {
			closeOnce.Do(func() {
				_ = clientInput.Close()
				_ = serverInput.Close()
				_ = serverOutput.Close()
				_ = clientOutput.Close()
				<-done
			})
		},
	}
}

func resultFrame(id json.RawMessage, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func errorFrame(id json.RawMessage, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}

func readString(values map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(values[key], &value)
	return value
}
