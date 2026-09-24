import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RunError } from "@/app/office/tasks/[id]/types";
import { WebSocketRequestError } from "@/lib/ws/client";
import { RunErrorEntry } from "./run-error-entry";

const RUN_RESUME_ID = "run-error-resume-button";

const { requestMock } = vi.hoisted(() => ({ requestMock: vi.fn() }));

vi.mock("@/components/state-provider", () => ({
  useAppStore: (selector: (state: unknown) => unknown) => selector({}),
}));
vi.mock("@/lib/state/slices/office/selectors", () => ({
  selectOfficeAgentProfiles: () => [],
}));
vi.mock("@/lib/ws/connection", () => ({
  getWebSocketClient: () => ({ request: requestMock }),
}));

afterEach(() => cleanup());

function runError(failureCode: string): RunError {
  return {
    id: "run-1",
    sessionId: "session-1",
    agentProfileId: "agent-1",
    rawPayload: "provider raw details",
    failedAt: "2026-08-20T10:00:00Z",
    failureCode,
    errorStamp: "ordinary-failure-stamp",
    message: "provider failure",
    remediationUrl: "https://opencode.ai/workspace/demo_workspace/go",
  };
}

describe("RunErrorEntry", () => {
  it.each(["provider_auth_required", "model_capacity"])(
    "keeps ordinary failure code %s on the resumable error surface",
    (failureCode) => {
      render(
        <RunErrorEntry taskId="task-1" workspaceId="workspace-1" error={runError(failureCode)} />,
      );

      expect(screen.getByTestId(RUN_RESUME_ID)).toBeTruthy();

      expect(screen.getByTestId("run-error-fresh-button")).toBeTruthy();
      expect(
        screen.getByTestId("run-error-raw-payload").closest("details")?.hasAttribute("open"),
      ).toBe(false);
      expect(screen.getByTestId("remediation-link")).toBeTruthy();
      expect(screen.queryByTestId("task-launch-error-entry")).toBeNull();
    },
  );

  it("retains a manual recovery error and exposes the typed branch action", async () => {
    requestMock.mockRejectedValueOnce(
      new WebSocketRequestError("The saved branch is no longer available.", "CONFLICT", {
        kind: "branch_unrecoverable",
        recovery_action: "resume_new_branch",
        original_branch: "feature/lost",
        base_branch: "main",
      }),
    );

    render(
      <RunErrorEntry
        taskId="task-1"
        workspaceId="workspace-1"
        error={runError("provider_auth_required")}
      />,
    );

    screen.getByTestId(RUN_RESUME_ID).click();

    expect((await screen.findByTestId("run-error-recovery-error")).textContent).toContain(
      "The saved branch is no longer available.",
    );
    expect(screen.getByTestId("run-error-continue-new-branch-button")).toBeTruthy();

    expect(screen.getByTestId("run-error-restore-workspace-button")).toBeTruthy();
  });

  it("renders recovered session history without stale recovery controls", () => {
    render(
      <RunErrorEntry
        taskId="task-1"
        workspaceId="workspace-1"
        error={{ ...runError("provider_auth_required"), isActive: false }}
      />,
    );

    expect(screen.queryByTestId(RUN_RESUME_ID)).toBeNull();
    expect(screen.queryByTestId("run-error-fresh-button")).toBeNull();
  });
});

it("explains a non-retryable recovery refusal without offering a bypass", async () => {
  requestMock.mockRejectedValueOnce(
    new WebSocketRequestError("blocked", "UNAVAILABLE", {
      kind: "session_recovery_unstoppable",
      retryable: false,
    }),
  );
  render(<RunErrorEntry taskId="task-1" error={runError("provider_auth_required")} />);
  fireEvent.click(screen.getByTestId(RUN_RESUME_ID));
  const error = await screen.findByTestId("run-error-recovery-error");
  expect(error.querySelector("p")?.textContent).toContain("Restart the backend");
  expect(screen.queryByTestId(RUN_RESUME_ID)).toBeNull();
});
