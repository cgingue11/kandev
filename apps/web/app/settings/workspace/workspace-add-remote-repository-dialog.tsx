"use client";

import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@kandev/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kandev/ui/dialog";
import { Spinner } from "@kandev/ui/spinner";
import { RemoteRepoChip } from "@/components/task-create-dialog-remote-repo-chip";
import type { TaskRemoteRepoRow } from "@/components/task-create-dialog-types";
import { useRemoteRepositories } from "@/hooks/domains/integrations/use-remote-repositories";
import { useBranchesByURL } from "@/hooks/domains/github/use-branches-by-url";
import { registerRemoteRepositoryAction } from "@/app/actions/workspaces";
import type { Repository } from "@/lib/types/http";
import { remoteRepositoryRegistrationPayload } from "./workspace-remote-repository-registration";

const EMPTY_ROW: TaskRemoteRepoRow = { key: "remote", url: "", branch: "", source: "paste" };

function RegistrationStatus({ error, submitting }: { error: string | null; submitting: boolean }) {
  const { t } = useTranslation();
  return (
    <>
      <p className="text-xs text-muted-foreground">{t("workspaces:remoteRepositoryBranchHint")}</p>
      {error ? (
        <p role="alert" className="text-sm text-destructive">
          {t("workspaces:couldNotAddRemoteRepositoryWithReason", { message: error })}
        </p>
      ) : null}
      {submitting ? (
        <p role="status" className="flex items-center gap-2 text-xs text-muted-foreground">
          <Spinner className="size-3" aria-hidden="true" />
          {t("workspaces:addingRepository")}
        </p>
      ) : null}
    </>
  );
}

type AddRemoteRepositoryDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workspaceId: string;
  /** Called with the saved repository; the page decides how to list it. */
  onRegistered: (repository: Repository) => void;
};

/**
 * Registers a provider-hosted repository from the workspace Repositories page.
 * It reuses the New Task remote picker, so every provider that lists there
 * (built-in hosts and plugin providers alike) is available here too.
 */
export function AddRemoteRepositoryDialog({
  open,
  onOpenChange,
  workspaceId,
  onRegistered,
}: AddRemoteRepositoryDialogProps) {
  const { t } = useTranslation();
  const [row, setRow] = useState<TaskRemoteRepoRow>(EMPTY_ROW);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const accessibleRepos = useRemoteRepositories(workspaceId);
  const branches = useBranchesByURL(workspaceId);

  useEffect(() => {
    if (!open) return;
    setRow(EMPTY_ROW);
    setError(null);
    setSubmitting(false);
  }, [open]);

  useEffect(() => {
    if (row.url) branches.ensure(row.url);
  }, [branches, row.url]);

  const handleURLChange = useCallback<Parameters<typeof RemoteRepoChip>[0]["onURLChange"]>(
    (url, source, metadata) => {
      setError(null);
      setRow({
        key: "remote",
        url,
        source,
        branch: metadata?.defaultBranch ?? "",
        remoteUrl: metadata?.remoteUrl,
        provider: metadata?.provider,
        providerHost: metadata?.providerHost,
        providerScope: metadata?.providerScope,
        providerRepoId: metadata?.providerRepoId,
        providerOwner: metadata?.providerOwner,
        providerName: metadata?.providerName,
        fullName: metadata?.fullName,
      });
    },
    [],
  );

  const handleConfirm = async () => {
    if (!row.url.trim() || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const repository = await registerRemoteRepositoryAction(
        workspaceId,
        remoteRepositoryRegistrationPayload(row),
      );
      onRegistered(repository);
      onOpenChange(false);
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : t("workspaces:couldNotAddRemoteRepository"),
      );
    } finally {
      setSubmitting(false);
    }
  };

  const canSave = Boolean(row.url.trim()) && !submitting;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("workspaces:addRemoteRepository")}</DialogTitle>
          <DialogDescription>{t("workspaces:addRemoteRepositoryDescription")}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <RemoteRepoChip
            workspaceId={workspaceId}
            row={row}
            branches={branches.branches(row.url)}
            branchesLoading={branches.loading(row.url)}
            accessibleRepos={accessibleRepos}
            onURLChange={handleURLChange}
            onBranchChange={(branch) => setRow((current) => ({ ...current, branch }))}
            onRemove={() => setRow(EMPTY_ROW)}
          />
          <RegistrationStatus error={error} submitting={submitting} />
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t("common:cancel")}
          </Button>
          <Button type="button" onClick={handleConfirm} disabled={!canSave}>
            {t("workspaces:addRepositoryToWorkspace")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
