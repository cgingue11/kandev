import type { ReactNode } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EXECUTOR_TYPE_MAP } from "@/app/settings/executors/new/[type]/executor-types";
import { OnboardingDialog } from "./onboarding-dialog";

const apiMocks = vi.hoisted(() => ({
  listAvailableAgents: vi.fn(),
  listWorkflowTemplates: vi.fn(),
}));

const actionMocks = vi.hoisted(() => ({
  listAgentsAction: vi.fn(),
  updateAgentProfileAction: vi.fn(),
}));

const staleSave = vi.hoisted(() => new Error("stale settings save"));
const CHANGED_PROFILE_MODEL = "changed-model";

const coordinatorState = vi.hoisted(() => ({
  snapshot: {
    reloadRequired: false,
    source: null as "boot_id_changed" | "settings_interlock_rejected" | null,
    ownerCount: 0,
  },
  listeners: new Set<() => void>(),
}));

vi.mock("@/lib/api", () => apiMocks);
vi.mock("@/app/actions/agents", () => actionMocks);
vi.mock("@/lib/api/client", () => ({
  isHandledApiError: (error: unknown) => error === staleSave,
}));
vi.mock("@/lib/platform/backend-reload-coordinator", () => ({
  backendReloadCoordinator: {
    getSnapshot: () => coordinatorState.snapshot,
    subscribe: (listener: () => void) => {
      coordinatorState.listeners.add(listener);
      return () => coordinatorState.listeners.delete(listener);
    },
  },
}));
vi.mock("@kandev/ui/dialog", () => {
  const Content = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  return {
    Dialog: ({ open, children }: { open: boolean; children: ReactNode }) =>
      open ? <div data-testid="onboarding-dialog">{children}</div> : null,
    DialogContent: Content,
    DialogHeader: Content,
    DialogTitle: Content,
    DialogFooter: Content,
    DialogDescription: Content,
  };
});
vi.mock("@/components/onboarding/step-agents", () => ({
  StepAgents: ({
    agentSettings,
    onUpdateSetting,
  }: {
    agentSettings: Record<string, { formData: { model: string } }>;
    onUpdateSetting: (agentName: string, formPatch: { model: string }) => void;
  }) => (
    <>
      <button
        type="button"
        disabled={!agentSettings["test-agent"]}
        onClick={() => onUpdateSetting("test-agent", { model: CHANGED_PROFILE_MODEL })}
      >
        Make agent dirty
      </button>
      <output data-testid="test-agent-model">{agentSettings["test-agent"]?.formData.model}</output>
    </>
  ),
}));

const availableAgent = {
  name: "test-agent",
  display_name: "Test Agent",
  model_config: {
    default_model: "default-model",
    current_mode_id: "default-mode",
    status: "ok",
    available_models: [],
  },
  permission_settings: {},
};

const savedAgent = {
  name: "test-agent",
  profiles: [
    {
      id: "profile-1",
      name: "Default",
      model: "default-model",
      mode: "default-mode",
      allowIndexing: false,
      autoApprove: false,
      cliPassthrough: false,
      cliFlags: [],
      commandPrefix: "",
    },
  ],
};

function signalReloadRequired() {
  coordinatorState.snapshot = {
    reloadRequired: true,
    source: "settings_interlock_rejected",
    ownerCount: 0,
  };
  coordinatorState.listeners.forEach((listener) => listener());
}

beforeEach(() => {
  vi.clearAllMocks();
  coordinatorState.snapshot = { reloadRequired: false, source: null, ownerCount: 0 };
  coordinatorState.listeners.clear();
  apiMocks.listAvailableAgents.mockResolvedValue({ agents: [availableAgent], tools: [] });
  apiMocks.listWorkflowTemplates.mockResolvedValue({ templates: [] });
  actionMocks.listAgentsAction.mockResolvedValue({ agents: [savedAgent] });
  actionMocks.updateAgentProfileAction.mockResolvedValue({});
});

afterEach(cleanup);

async function openExecutorStep(onComplete = vi.fn()) {
  render(<OnboardingDialog open onComplete={onComplete} />);
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  await screen.findByText("Executors");
  return onComplete;
}

describe("OnboardingDialog executor discovery", () => {
  it("shows the supported Settings executors as non-selectable information cards", async () => {
    await openExecutorStep();

    const cards = screen.getAllByTestId(/^onboarding-executor-card-/);
    const cardIds = cards.map((card) => card.getAttribute("data-executor-id"));
    const settingsExecutorIds = Object.keys(EXECUTOR_TYPE_MAP).filter(
      (executorId) => executorId !== "remote_docker",
    );

    expect(cardIds[0]).toBe("worktree");
    expect([...cardIds].sort()).toEqual(settingsExecutorIds.sort());
    expect(screen.queryByTestId("onboarding-executor-card-remote_docker")).toBeNull();
    expect(screen.queryByTestId("onboarding-executor-card-mock_remote")).toBeNull();

    for (const card of cards) {
      expect(card.tagName).toBe("DIV");
      expect(card.getAttribute("role")).toBeNull();
      expect(card.querySelector("button, a")).toBeNull();
    }
  });

  it("explains executor boundaries, setup needs, profiles, and the executor guide", async () => {
    await openExecutorStep();

    const worktree = screen.getByTestId("onboarding-executor-card-worktree");
    const local = screen.getByTestId("onboarding-executor-card-local");
    const docker = screen.getByTestId("onboarding-executor-card-local_docker");

    expect(worktree.textContent).toContain("Recommended for existing Git repositories");
    expect(worktree.textContent).toContain("separate Git checkout");
    expect(worktree.textContent).toContain("host account");
    expect(local.textContent).toContain("selected folder");
    expect(local.textContent).toContain("Kandev host");
    expect(docker.textContent).toContain("Docker daemon");
    expect(docker.textContent).toContain("mounts");
    expect(docker.textContent).not.toMatch(/full isolation/i);
    expect(screen.getByText(/An executor chooses where work runs\./).textContent).toContain(
      "Settings > Executors",
    );
    expect(screen.getByRole("link", { name: "View executor guide" }).getAttribute("href")).toBe(
      "https://kandev.ai/docs/executors",
    );
  });

  it("keeps Back, Next, and Skip as the only wizard actions", async () => {
    const onComplete = await openExecutorStep();

    expect(screen.getByRole("button", { name: "Back" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Next" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Skip" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Worktree" })).toBeNull();
    expect(onComplete).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByText("Agentic Workflows");
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    await screen.findByText("Executors");
    expect(onComplete).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Skip" }));
    expect(onComplete).toHaveBeenCalledOnce();
  });
});

describe("OnboardingDialog backend restart recovery", () => {
  it("keeps dirty profile edits across a hidden phone state and saves only when proceeding", async () => {
    const onComplete = vi.fn();
    const page = render(<OnboardingDialog open onComplete={onComplete} />);
    const dirtyButton = (await screen.findByRole("button", {
      name: "Make agent dirty",
    })) as HTMLButtonElement;
    await waitFor(() => expect(dirtyButton.disabled).toBe(false));
    fireEvent.click(dirtyButton);
    expect(screen.getByTestId("test-agent-model").textContent).toBe(CHANGED_PROFILE_MODEL);
    expect(actionMocks.updateAgentProfileAction).not.toHaveBeenCalled();

    page.rerender(<OnboardingDialog open={false} onComplete={onComplete} />);
    expect(screen.queryByTestId("test-agent-model")).toBeNull();
    expect(actionMocks.updateAgentProfileAction).not.toHaveBeenCalled();

    page.rerender(<OnboardingDialog open onComplete={onComplete} />);
    await waitFor(() => expect(actionMocks.listAgentsAction).toHaveBeenCalledTimes(2));
    await waitFor(() => {
      expect(screen.getByTestId("test-agent-model").textContent).toBe(CHANGED_PROFILE_MODEL);
    });
    expect(actionMocks.updateAgentProfileAction).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => {
      expect(actionMocks.updateAgentProfileAction).toHaveBeenCalledWith(
        "profile-1",
        expect.objectContaining({ model: CHANGED_PROFILE_MODEL }),
      );
    });
    expect(onComplete).not.toHaveBeenCalled();
  });

  it("keeps the wizard on the agent step after a handled stale save", async () => {
    actionMocks.updateAgentProfileAction.mockRejectedValue(staleSave);
    const onComplete = vi.fn();

    render(<OnboardingDialog open onComplete={onComplete} />);
    const dirtyButton = (await screen.findByRole("button", {
      name: "Make agent dirty",
    })) as HTMLButtonElement;
    await waitFor(() => expect(dirtyButton.disabled).toBe(false));
    fireEvent.click(dirtyButton);
    fireEvent.click(screen.getByRole("button", { name: "Next" }));

    await waitFor(() => expect(actionMocks.updateAgentProfileAction).toHaveBeenCalledOnce());
    expect(screen.getByText("AI Agents")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Next" })).toBeTruthy();
    expect(onComplete).not.toHaveBeenCalled();
  });

  it("blocks get started and yields the modal when the final save is stale", async () => {
    let saveCount = 0;
    actionMocks.updateAgentProfileAction.mockImplementation(async () => {
      saveCount += 1;
      if (saveCount === 2) {
        signalReloadRequired();
        throw staleSave;
      }
      return {};
    });
    const onComplete = vi.fn();

    render(<OnboardingDialog open onComplete={onComplete} />);
    const dirtyButton = (await screen.findByRole("button", {
      name: "Make agent dirty",
    })) as HTMLButtonElement;
    await waitFor(() => expect(dirtyButton.disabled).toBe(false));
    fireEvent.click(dirtyButton);
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(screen.getByText("Executors")).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(screen.getByText("Agentic Workflows")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(screen.getByText("Command Panel")).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Get Started" }));

    await waitFor(() => expect(actionMocks.updateAgentProfileAction).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByTestId("onboarding-dialog")).toBeNull());
    expect(onComplete).not.toHaveBeenCalled();
  });
});
