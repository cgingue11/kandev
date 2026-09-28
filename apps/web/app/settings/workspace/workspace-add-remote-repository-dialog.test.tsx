import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { TooltipProvider } from "@kandev/ui/tooltip";
import { AddRemoteRepositoryDialog } from "./workspace-add-remote-repository-dialog";
import type { Repository } from "@/lib/types/http";

const { registerRemoteRepositoryAction } = vi.hoisted(() => ({
  registerRemoteRepositoryAction: vi.fn(),
}));

vi.mock("@/app/actions/workspaces", () => ({ registerRemoteRepositoryAction }));
vi.mock("@/hooks/domains/integrations/use-remote-repositories", async () => {
  const support = await import("@/components/task-create-dialog-remote-repo-chip-test-support");
  return {
    useRemoteRepositories: () => support.makeAccessible({ repos: [support.githubSite()] }),
  };
});
vi.mock("@/hooks/domains/github/use-branches-by-url", () => ({
  useBranchesByURL: () => ({
    branches: () => [{ name: "main", type: "remote" }],
    loading: () => false,
    error: () => undefined,
    ensure: () => undefined,
    clear: () => undefined,
  }),
}));

const CONFIRM_LABEL = "Add to workspace";
const registered = {
  id: "repo-1",
  workspace_id: "workspace-1",
  name: "acme/site",
  source_type: "provider",
  local_path: "",
  provider: "github",
  provider_repo_id: "",
  provider_owner: "acme",
  provider_name: "site",
  remote_url: "https://github.com/acme/site.git",
  default_branch: "main",
  worktree_branch_prefix: "feature/",
  pull_before_worktree: true,
  setup_script: "",
  cleanup_script: "",
  dev_script: "",
  copy_files: "",
  created_at: "",
  updated_at: "",
} as unknown as Repository;

function renderDialog() {
  const onOpenChange = vi.fn();
  const onRegistered = vi.fn();
  render(
    <TooltipProvider>
      <AddRemoteRepositoryDialog
        open
        onOpenChange={onOpenChange}
        workspaceId="workspace-1"
        onRegistered={onRegistered}
      />
    </TooltipProvider>,
  );
  return { onOpenChange, onRegistered };
}

function pickAcmeSite() {
  fireEvent.click(screen.getByTestId("remote-repo-chip-trigger"));
  fireEvent.click(screen.getByTestId("remote-repo-option"));
}

afterEach(() => {
  cleanup();
  registerRemoteRepositoryAction.mockReset();
});

describe("AddRemoteRepositoryDialog", () => {
  it("registers the picked repository and reports it back to the page", async () => {
    registerRemoteRepositoryAction.mockResolvedValue(registered);
    const { onOpenChange, onRegistered } = renderDialog();
    const confirm = screen.getByRole("button", { name: CONFIRM_LABEL });
    expect(confirm.hasAttribute("disabled")).toBe(true);

    pickAcmeSite();
    await waitFor(() => expect(confirm.hasAttribute("disabled")).toBe(false));
    fireEvent.click(confirm);

    await waitFor(() => expect(onRegistered).toHaveBeenCalledWith(registered));
    expect(registerRemoteRepositoryAction).toHaveBeenCalledWith("workspace-1", {
      remote_url: "https://github.com/acme/site",
      default_branch: "main",
    });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("keeps the dialog open and shows the failure when registration is rejected", async () => {
    registerRemoteRepositoryAction.mockRejectedValue(new Error("provider unavailable"));
    const { onOpenChange, onRegistered } = renderDialog();

    pickAcmeSite();
    const confirm = screen.getByRole("button", { name: CONFIRM_LABEL });
    await waitFor(() => expect(confirm.hasAttribute("disabled")).toBe(false));
    fireEvent.click(confirm);

    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toContain("provider unavailable"),
    );
    expect(onRegistered).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});
