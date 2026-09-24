package codexdbg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/kandev/kandev/pkg/codexappserver"
)

var requestDefaults = map[string]json.RawMessage{
	"item/commandExecution/requestApproval": json.RawMessage(`{"decision":"decline"}`),
	"item/fileChange/requestApproval":       json.RawMessage(`{"decision":"decline"}`),
	"item/tool/requestUserInput":            json.RawMessage(`{"answers":{}}`),
	"mcpServer/elicitation/request":         json.RawMessage(`{"action":"cancel","content":null,"_meta":null}`),
	"item/permissions/requestApproval":      json.RawMessage(`{"permissions":{},"scope":"turn"}`),
	"applyPatchApproval":                    json.RawMessage(`{"decision":{"denied":{"rejection":"Codex debugger declined the request."}}}`),
	"execCommandApproval":                   json.RawMessage(`{"decision":{"denied":{"rejection":"Codex debugger declined the request."}}}`),
}

// RequestPolicy gives app-server requests a safe default and only accepts
// answer-file responses for methods in the pinned protocol.
type RequestPolicy struct {
	answers map[string]json.RawMessage
}

func NewRequestPolicy(answers map[string]json.RawMessage) *RequestPolicy {
	copyAnswers := make(map[string]json.RawMessage, len(answers))
	for method, answer := range answers {
		copyAnswers[method] = append(json.RawMessage(nil), answer...)
	}
	return &RequestPolicy{answers: copyAnswers}
}

// Handle implements codexappserver.RequestHandler.
func (p *RequestPolicy) Handle(ctx context.Context, method string, _ json.RawMessage) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, known := requestDefaults[method]; !known {
		return nil, &codexappserver.RPCError{Code: -32601, Message: "method not found: " + method}
	}
	if answer, ok := p.answers[method]; ok {
		if !json.Valid(answer) {
			return nil, fmt.Errorf("answer for %s is invalid JSON", method)
		}
		return answer, nil
	}
	return requestDefaults[method], nil
}

// LoadAnswerFile reads explicit method-specific JSON responses. Unknown
// methods are rejected so an answer cannot grant an unreviewed request.
func LoadAnswerFile(path string) (map[string]json.RawMessage, error) {
	if path == "" {
		return nil, errors.New("answer-file path is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read answer file: %w", err)
	}
	var answers map[string]json.RawMessage
	if err := json.Unmarshal(content, &answers); err != nil {
		return nil, fmt.Errorf("decode answer file: %w", err)
	}
	for method, answer := range answers {
		if _, known := requestDefaults[method]; !known {
			return nil, fmt.Errorf("answer file names unsupported server request %q", method)
		}
		if !json.Valid(answer) {
			return nil, fmt.Errorf("answer for %s is not valid JSON", method)
		}
	}
	return answers, nil
}
