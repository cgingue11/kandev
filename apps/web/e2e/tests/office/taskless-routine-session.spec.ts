import { expect, test } from "../../fixtures/office-fixture";

type RoutineRun = {
  id: string;
  linked_task_id?: string;
  status: string;
};

async function routineRuns(
  officeApi: { listRoutineRuns(id: string): Promise<Record<string, unknown>> },
  id: string,
) {
  const result = await officeApi.listRoutineRuns(id);
  return (Array.isArray(result.runs) ? result.runs : []) as RoutineRun[];
}

test.describe("Office taskless routine sessions", () => {
  test("fires a real taskless routine twice without creating task rows", async ({
    officeApi,
    apiClient,
    officeSeed,
  }) => {
    test.setTimeout(120_000);
    const before = await apiClient.listTasks(officeSeed.workspaceId);
    const routine = await officeApi.createRoutine(officeSeed.workspaceId, {
      name: `Taskless E2E ${Date.now()}`,
      description: "Taskless routine session smoke test",
      assignee_agent_profile_id: officeSeed.agentId,
    });
    const routineId = routine.id as string;

    const existing = await officeApi.listRuns(officeSeed.workspaceId);
    const seen = new Set(((existing.runs ?? []) as { id: string }[]).map((run) => run.id));
    const sessions: string[] = [];
    for (let attempt = 1; attempt <= 2; attempt += 1) {
      const response = await officeApi.runRoutine(routineId);
      if (response.status !== 200) {
        throw new Error(
          `manual routine fire returned ${response.status}: ${await response.text()}`,
        );
      }
      const fired = (await response.json()) as { run: RoutineRun };
      expect(fired.run.id).toBeTruthy();
      const routineRunId = fired.run.id;
      await expect
        .poll(() => routineRuns(officeApi, routineId), { timeout: 20_000 })
        .toHaveLength(attempt);
      await expect
        .poll(async () =>
          (await routineRuns(officeApi, routineId)).some((run) => run.id === routineRunId),
        )
        .toBe(true);
      let runId = "";
      let observedRuns: unknown[] = [];
      await expect
        .poll(
          async () => {
            const result = await officeApi.listRuns(officeSeed.workspaceId);
            observedRuns = (result.runs ?? []) as unknown[];
            const run = (
              observedRuns as {
                id: string;
                agent_profile_id?: string;
                reason?: string;
              }[]
            ).find(
              (candidate) =>
                !seen.has(candidate.id) &&
                candidate.agent_profile_id === officeSeed.agentId &&
                candidate.reason?.startsWith("routine_"),
            );
            runId = run?.id ?? "";
            return runId;
          },
          { timeout: 30_000 },
        )
        .not.toBe("")
        .catch((error) => {
          throw new Error(
            `No live office run found for routine fire ${JSON.stringify({ routineId, routineRunId, agentId: officeSeed.agentId, observedRuns })}`,
            { cause: error },
          );
        });
      seen.add(runId);
      const detailPath = `/agents/${officeSeed.agentId}/runs/${runId}`;
      await expect
        .poll(
          async () => {
            const result = await officeApi.rawRequest("GET", detailPath);
            if (!result.ok) {
              throw new Error(`run detail returned ${result.status}: ${await result.text()}`);
            }
            const detail = await result.json();
            return detail.status;
          },
          { timeout: 60_000 },
        )
        .toMatch(/^(finished|failed|cancelled)$/);
      const detail = await (await officeApi.rawRequest("GET", detailPath)).json();
      expect({ status: detail.status, error: detail.error_message ?? "" }).toEqual({
        status: "finished",
        error: "",
      });
      expect(detail.task_id ?? "").toBe("");
      expect(detail.session.session_id).toBeTruthy();
      expect(detail.assembled_prompt).toBeTruthy();
      sessions.push(detail.session.session_id);
    }

    const runs = await routineRuns(officeApi, routineId);
    expect(runs.every((run) => !run.linked_task_id)).toBe(true);
    expect(new Set(runs.map((run) => run.id)).size).toBe(2);

    expect(new Set(sessions).size).toBe(2);
    const after = await apiClient.listTasks(officeSeed.workspaceId);
    expect(after.tasks.map((task) => task.id)).toEqual(before.tasks.map((task) => task.id));
  });
});
