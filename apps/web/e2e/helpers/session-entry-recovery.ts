import type { Page } from "@playwright/test";
import type { SeedData } from "../fixtures/test-base";
import type { ApiClient } from "./api-client";
import { injectLatency } from "./causal-waits";

type WireFrame = {
  id?: unknown;
  type?: unknown;
  action?: unknown;
  payload?: unknown;
};

type DelayRule = {
  remaining: number;
  delayMs: number;
  reason: string;
};

export type SessionEntryRecoveryProxy = {
  delayNextResponses: (action: string, count: number, delayMs: number, reason: string) => void;
  dropNextResponses: (action: string, count: number) => void;
  failResponses: (action: string, message: string) => void;
  allowResponses: (action: string) => void;
  holdResponses: (action: string) => void;
  releaseHeldResponses: (action: string) => void;
  pendingRequestCount: (action: string) => number;
  requestCount: (action: string) => number;
  delayedResponseCount: (action: string) => number;
  droppedResponseCount: (action: string) => number;
  failedResponseCount: (action: string) => number;
  heldResponseCount: (action: string) => number;
};

export async function createSettledHistoryTask(
  apiClient: ApiClient,
  seedData: SeedData,
  title: string,
) {
  const task = await apiClient.createTask(seedData.workspaceId, title, {
    workflow_id: seedData.workflowId,
    workflow_step_id: seedData.startStepId,
    agent_profile_id: seedData.agentProfileId,
    repository_ids: [seedData.repositoryId],
  });
  const { session_id: sessionId } = await apiClient.seedTaskSession(task.id, {
    state: "WAITING_FOR_INPUT",
    agentProfileId: seedData.agentProfileId,
    repositoryId: seedData.repositoryId,
  });
  await apiClient.seedSessionMessage(sessionId, {
    type: "message",
    authorType: "user",
    content: "Earlier user message",
  });
  await apiClient.seedSessionMessage(sessionId, {
    type: "message",
    authorType: "agent",
    content: "This is a simple mock response for e2e testing.",
  });
  return task;
}

function parseFrame(value: string): WireFrame | null {
  try {
    const parsed = JSON.parse(value) as unknown;
    return typeof parsed === "object" && parsed !== null ? (parsed as WireFrame) : null;
  } catch {
    return null;
  }
}

function isResponseFrame(frame: WireFrame | null): boolean {
  return frame?.type === "response" || frame?.type === "error";
}

function responseAction(
  frame: WireFrame | null,
  requestActions: Map<string, string>,
): string | undefined {
  if (typeof frame?.action === "string") return frame.action;
  if (typeof frame?.id === "string") return requestActions.get(frame.id);
  return undefined;
}

function takeResponseAction(
  frame: WireFrame | null,
  requestActions: Map<string, string>,
): string | undefined {
  const action = responseAction(frame, requestActions);
  if (typeof frame?.id === "string") requestActions.delete(frame.id);
  return action;
}

function consumeDropRule(
  action: string | undefined,
  dropRules: Map<string, number>,
  droppedCounts: Map<string, number>,
): boolean {
  if (!action) return false;
  const remaining = dropRules.get(action) ?? 0;
  if (remaining < 1) return false;
  dropRules.set(action, remaining - 1);
  droppedCounts.set(action, (droppedCounts.get(action) ?? 0) + 1);
  return true;
}

function consumeDelayRule(
  action: string | undefined,
  message: string,
  rules: Map<string, DelayRule>,
  delayedCounts: Map<string, number>,
  send: (message: string) => void,
): boolean {
  if (!action) return false;
  const rule = rules.get(action);
  if (!rule || rule.remaining < 1) return false;
  rule.remaining -= 1;
  delayedCounts.set(action, (delayedCounts.get(action) ?? 0) + 1);
  void (async () => {
    await injectLatency(rule.delayMs, rule.reason);
    send(message);
  })();
  return true;
}

/**
 * Fail, delay, or drop selected gateway responses while forwarding other frames.
 * Rules correlate replies by request id, so the test never relies on
 * action-only or payload timing and does not inspect message contents.
 */
export async function routeSessionEntryRecovery(page: Page): Promise<SessionEntryRecoveryProxy> {
  const requestActions = new Map<string, string>();
  const requestCounts = new Map<string, number>();
  const delayedCounts = new Map<string, number>();
  const droppedCounts = new Map<string, number>();
  const failedCounts = new Map<string, number>();
  const heldCounts = new Map<string, number>();
  const rules = new Map<string, DelayRule>();
  const dropRules = new Map<string, number>();
  const failureMessages = new Map<string, string>();
  const heldActions = new Set<string>();

  await page.routeWebSocket(/\/ws$/, (ws) => {
    const server = ws.connectToServer();

    ws.onMessage((message) => {
      if (typeof message === "string") {
        for (const part of message.split("\n")) {
          const frame = parseFrame(part.trim());
          if (
            frame?.type === "request" &&
            typeof frame.id === "string" &&
            typeof frame.action === "string"
          ) {
            requestActions.set(frame.id, frame.action);
            requestCounts.set(frame.action, (requestCounts.get(frame.action) ?? 0) + 1);
          }
        }
      }
      server.send(message);
    });

    server.onMessage((message) => {
      if (typeof message !== "string") {
        ws.send(message);
        return;
      }

      for (const part of message.split("\n")) {
        const trimmed = part.trim();
        if (!trimmed) continue;
        const frame = parseFrame(trimmed);
        const action = takeResponseAction(frame, requestActions);
        if (isResponseFrame(frame)) {
          if (action && heldActions.has(action)) {
            heldCounts.set(action, (heldCounts.get(action) ?? 0) + 1);
            continue;
          }
          if (frame && action && failureMessages.has(action)) {
            failedCounts.set(action, (failedCounts.get(action) ?? 0) + 1);
            ws.send(
              JSON.stringify({
                ...frame,
                type: "error",
                payload: {
                  code: "INTERNAL_ERROR",
                  message: failureMessages.get(action),
                },
              }),
            );
            continue;
          }
          if (consumeDropRule(action, dropRules, droppedCounts)) continue;
          if (consumeDelayRule(action, trimmed, rules, delayedCounts, ws.send.bind(ws))) continue;
        }

        ws.send(trimmed);
      }
    });
  });

  return {
    delayNextResponses: (action, count, delayMs, reason) => {
      if (count < 1) throw new Error("delayNextResponses requires a positive response count");
      if (delayMs < 0) throw new Error("delayNextResponses requires a non-negative delay");
      rules.set(action, { remaining: count, delayMs, reason });
    },
    dropNextResponses: (action, count) => {
      if (count < 1) throw new Error("dropNextResponses requires a positive response count");
      dropRules.set(action, count);
    },
    failResponses: (action, message) => {
      if (!message) throw new Error("failResponses requires an error message");
      failureMessages.set(action, message);
    },
    allowResponses: (action) => {
      failureMessages.delete(action);
    },
    holdResponses: (action) => {
      heldActions.add(action);
    },
    releaseHeldResponses: (action) => {
      heldActions.delete(action);
    },
    pendingRequestCount: (action) =>
      [...requestActions.values()].filter((requestAction) => requestAction === action).length,
    requestCount: (action) => requestCounts.get(action) ?? 0,
    delayedResponseCount: (action) => delayedCounts.get(action) ?? 0,
    droppedResponseCount: (action) => droppedCounts.get(action) ?? 0,
    failedResponseCount: (action) => failedCounts.get(action) ?? 0,
    heldResponseCount: (action) => heldCounts.get(action) ?? 0,
  };
}
