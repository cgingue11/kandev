import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RunError } from "@/app/office/tasks/[id]/types";
import { WebSocketRequestError } from "@/lib/ws/client";
import { RunErrorEntry } from "./run-error-entry";

const RECOVERY_ERROR_ID = "run-error-recovery-error";
const { requestMock } = vi.hoisted(() => ({ requestMock: vi.fn() }));
const RUN_ERROR_RESUME_TEST_ID = "run-error-resume-button";

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
  it("renders managed npm policy failures on the runtime recovery surface", () => {
    render(
      <RunErrorEntry
        taskId="task-1"
        workspaceId="workspace-1"
        error={{
          ...runError("managed_runtime_npm_policy"),
          failureDetails:
            "npm error notarget No matching version found. Minimum release age policy applies.",
        }}
      />,
    );

    const recovery = screen.getByTestId("run-error-managed-runtime-npm-recovery");
    expect(recovery.textContent).toContain("npm blocked this runtime version");
    expect(recovery.textContent).toContain(
      "Check npm's min-release-age or before setting. Wait until this version is eligible or select an older version, then retry.",
    );
    expect(screen.getByTestId("run-error-managed-runtime-retry-button")).toBeTruthy();
    expect(screen.queryByTestId(RUN_ERROR_RESUME_TEST_ID)).toBeNull();
  });

  it.each(["provider_auth_required", "model_capacity"])(
    "keeps ordinary failure code %s on the resumable error surface",
    (failureCode) => {
      render(
        <RunErrorEntry taskId="task-1" workspaceId="workspace-1" error={runError(failureCode)} />,
      );

      expect(screen.getByTestId(RUN_ERROR_RESUME_TEST_ID)).toBeTruthy();

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

    screen.getByTestId(RUN_ERROR_RESUME_TEST_ID).click();

    expect((await screen.findByTestId(RECOVERY_ERROR_ID)).textContent).toContain(
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

    expect(screen.queryByTestId(RUN_ERROR_RESUME_TEST_ID)).toBeNull();
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
  fireEvent.click(screen.getByTestId(RUN_ERROR_RESUME_TEST_ID));
  const error = await screen.findByTestId(RECOVERY_ERROR_ID);
  expect(error.querySelector("p")?.textContent).toContain("Restart the backend");
  expect(screen.queryByTestId(RUN_ERROR_RESUME_TEST_ID)).toBeNull();
});

it("redacts credential-bearing branch failures in the status line", async () => {
  requestMock.mockRejectedValueOnce(
    new WebSocketRequestError("Branch failed: token=run-secret-fixture", "CONFLICT", {
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
  fireEvent.click(screen.getByTestId(RUN_ERROR_RESUME_TEST_ID));
  await screen.findByTestId(RECOVERY_ERROR_ID);
  expect(document.body.textContent).not.toContain("run-secret-fixture");
});

it("keeps an explanation when branch error sanitization removes all content", async () => {
  requestMock.mockRejectedValueOnce(
    new WebSocketRequestError("\u001b[31m\u001b[0m", "CONFLICT", {
      kind: "branch_unrecoverable",
      recovery_action: "resume_new_branch",
      original_branch: "feature/lost",
      base_branch: "main",
    }),
  );
  render(<RunErrorEntry taskId="task-1" error={runError("provider_auth_required")} />);
  fireEvent.click(screen.getByTestId(RUN_ERROR_RESUME_TEST_ID));
  const error = await screen.findByTestId(RECOVERY_ERROR_ID);
  expect(error.querySelector("p")?.textContent).toBe("Failed to resume session");
});
