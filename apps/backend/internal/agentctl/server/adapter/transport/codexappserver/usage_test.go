package codexappserver

import (
	"encoding/json"
	"testing"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	protocol "github.com/kandev/kandev/pkg/codexappserver"
)

func TestNormalizeRawResponseUsagePreservesDisjointLedgerComponents(t *testing.T) {
	usage, observation, ok := normalizeRawResponseUsage(protocol.TokenUsageBreakdown{
		InputTokens:           1_000,
		CachedInputTokens:     600,
		CacheWriteInputTokens: 0,
		OutputTokens:          200,
		ReasoningOutputTokens: 80,
		TotalTokens:           1_200,
	}, "thread-1", "turn-1", "response-1", false, "gpt-5")
	if !ok {
		t.Fatal("normalizeRawResponseUsage rejected valid response usage")
	}
	if usage.InputTokens != 400 || usage.CachedReadTokens != 600 || usage.OutputTokens != 200 || usage.TotalTokens != 1_200 {
		t.Fatalf("normalized usage = %#v, want 400 uncached + 600 cached + 200 output = 1200", usage)
	}
	if usage.ThoughtTokens != 0 {
		t.Fatalf("ThoughtTokens = %d, native reasoning is an output subset", usage.ThoughtTokens)
	}
	if observation.ReasoningOutputTokens == nil || *observation.ReasoningOutputTokens != 80 {
		t.Fatalf("reasoning detail = %#v, want 80", observation.ReasoningOutputTokens)
	}
	if observation.PriceSuppressed {
		t.Fatal("zero native cache-write count should not suppress pricing")
	}
}

func TestNormalizeRawResponseUsageSuppressesUnknownCacheWritePricing(t *testing.T) {
	usage, observation, ok := normalizeRawResponseUsage(protocol.TokenUsageBreakdown{
		InputTokens:           100,
		CachedInputTokens:     25,
		CacheWriteInputTokens: 10,
		OutputTokens:          20,
		ReasoningOutputTokens: 5,
		TotalTokens:           120,
	}, "thread-1", "turn-1", "response-1", false, "gpt-5")
	if !ok || usage == nil {
		t.Fatal("normalizeRawResponseUsage rejected valid cache-write usage")
	}
	if !observation.PriceSuppressed || observation.ReportedCacheWriteTokens == nil || *observation.ReportedCacheWriteTokens != 10 {
		t.Fatalf("cache-write detail = %#v, pricing suppression = %t", observation.ReportedCacheWriteTokens, observation.PriceSuppressed)
	}
}

func TestNormalizeRawResponseUsageRejectsImpossibleSubsetAndNegativeCounts(t *testing.T) {
	cases := []protocol.TokenUsageBreakdown{
		{InputTokens: 10, CachedInputTokens: 11, OutputTokens: 2},
		{InputTokens: 10, CachedInputTokens: 2, OutputTokens: 2, ReasoningOutputTokens: 3},
		{InputTokens: -1, OutputTokens: 2},
	}
	for _, raw := range cases {
		if _, _, ok := normalizeRawResponseUsage(raw, "t", "u", "r", false, "m"); ok {
			t.Errorf("normalizeRawResponseUsage accepted invalid usage %#v", raw)
		}
	}
}

func TestRawResponsesRemainDistinctAndFallbackDoesNotDoubleCount(t *testing.T) {
	a := NewAdapter(nil, nil)
	a.threadID = "thread-1"
	a.modelID = "gpt-5"
	a.activeGeneration = 7
	params := func(responseID string) map[string]any {
		return map[string]any{
			"threadId": "thread-1", "turnId": "native-turn", "responseId": responseID,
			"usage": map[string]any{
				"totalTokens": 12, "inputTokens": 10, "cachedInputTokens": 4,
				"cacheWriteInputTokens": 0, "outputTokens": 2, "reasoningOutputTokens": 1,
			},
		}
	}
	a.handleRawResponseCompleted(params("response-a"))
	a.handleRawResponseCompleted(params("response-b"))
	a.finalizeProviderTurn("thread-1", "native-turn")
	var responseIDs []string
	for len(responseIDs) < 2 {
		event := <-a.Updates()
		if event.Type != streams.EventTypeUsageObservation {
			continue
		}
		responseIDs = append(responseIDs, event.UsageObservation.ProviderResponseID)
	}
	if responseIDs[0] == responseIDs[1] {
		t.Fatalf("response observations share an ID: %v", responseIDs)
	}
	select {
	case event := <-a.Updates():
		if event.Type == streams.EventTypeUsageObservation {
			t.Fatalf("turn finalization added fallback after exact response events: %s", event.UsageObservation.Source)
		}
	default:
	}
	_ = a.Close()
}

func TestTokenUsageReplayBaselineDoesNotEmitHistoricalDelta(t *testing.T) {
	a := NewAdapter(nil, nil)
	a.threadID = "thread-1"
	a.handleTokenUsageNotification(map[string]any{
		"threadId": "thread-1", "turnId": "prior-turn",
		"tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 100, "cachedInputTokens": 20, "outputTokens": 30, "totalTokens": 130}},
	})
	a.beginProviderTurn("thread-1", "new-turn")
	a.latestTokenTotals["thread-1"] = protocol.TokenUsageBreakdown{InputTokens: 150, CachedInputTokens: 30, OutputTokens: 45, TotalTokens: 195}
	a.finalizeProviderTurn("thread-1", "new-turn")
	event := <-a.Updates()
	if event.Type != streams.EventTypeUsageObservation || event.UsageObservation.Source != "turn_fallback" {
		t.Fatalf("fallback event = %#v", event)
	}
	if event.Usage.InputTokens != 40 || event.Usage.CachedReadTokens != 10 || event.Usage.OutputTokens != 15 {
		t.Fatalf("baseline delta = %#v, want uncached 40 / cached 10 / output 15", event.Usage)
	}
	_ = a.Close()
}

func TestRawResponseUsageNotificationDecodesOptionalUsage(t *testing.T) {
	var got protocol.RawResponseCompleted
	raw := json.RawMessage(`{"threadId":"thread","turnId":"turn","responseId":"response","usage":null,"usageMetadata":null}`)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ResponseID != "response" || got.Usage != nil {
		t.Fatalf("decoded raw response = %#v", got)
	}
}
