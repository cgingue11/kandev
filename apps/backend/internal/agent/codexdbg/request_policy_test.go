package codexdbg

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kandev/kandev/pkg/codexappserver"
)

func TestRequestPolicyDeclinesByDefaultAndRejectsUnknownMethods(t *testing.T) {
	policy := NewRequestPolicy(nil)
	response, err := policy.Handle(context.Background(), "item/commandExecution/requestApproval", nil)
	if err != nil {
		t.Fatalf("known request: %v", err)
	}
	var approval struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(response.(json.RawMessage), &approval); err != nil {
		t.Fatal(err)
	}
	if approval.Decision != "decline" {
		t.Fatalf("default decision = %q, want decline", approval.Decision)
	}

	policy = NewRequestPolicy(map[string]json.RawMessage{
		"unknown/request": json.RawMessage(`{"decision":"accept"}`),
	})
	_, err = policy.Handle(context.Background(), "unknown/request", nil)
	var rpcErr *codexappserver.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32601 {
		t.Fatalf("unknown request error = %v, want method-not-found", err)
	}
}

func TestAnswerFileValidatesKnownRequestMethods(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{"item/commandExecution/requestApproval":{"decision":"accept"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	answers, err := LoadAnswerFile(path)
	if err != nil {
		t.Fatalf("load answer file: %v", err)
	}
	policy := NewRequestPolicy(answers)
	response, err := policy.Handle(context.Background(), "item/commandExecution/requestApproval", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(response.(json.RawMessage)) != `{"decision":"accept"}` {
		t.Fatalf("explicit answer = %s", response)
	}

	if err := os.WriteFile(path, []byte(`{"unrecognized/request":{"decision":"accept"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnswerFile(path); err == nil {
		t.Fatal("answer file accepted an unknown request method")
	}
}
