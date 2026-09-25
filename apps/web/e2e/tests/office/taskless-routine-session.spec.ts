import { expect, test } from "../../fixtures/office-fixture";

type RoutineRun = {
  id: string;
  linked_task_id?: string;
  status: string;
};

type AgentRun = { id: string; routine_id?: string };

async function routineRuns(
  officeApi: { listRoutineRuns(id: string): Promise<Record<string, unknown>> },
  id: string,
) {
  const result = await officeApi.listRoutineRuns(id);
  return (Array.isArray(result.runs) ? result.runs : []) as RoutineRun[];
}

async function listAgentRuns(
  officeApi: { rawRequest: (method: string, path: string) => Promise<Response> },
  agentId: string,
): Promise<AgentRun[]> {
  const allRuns: AgentRun[] = [];
  let cursor = "";
  let cursorId = "";
  for (;;) {
    const query = new URLSearchParams({ limit: "100" });
    if (cursor) {
      query.set("cursor", cursor);
      query.set("cursor_id", cursorId);
    }
    const response = await officeApi.rawRequest(
      "GET",
      `/agents/${agentId}/runs?${query.toString()}`,
    );
    if (!response.ok) {
      throw new Error(`Listing runs for agent ${agentId} failed with HTTP ${response.status}`);
    }
    const result = (await response.json()) as {
      runs?: AgentRun[];
      next_cursor?: string;
      next_id?: string;
    };
    const page = result.runs ?? [];
    allRuns.push(...page);
    if (!result.next_cursor || !result.next_id || page.length === 0) return allRuns;
    if (result.next_cursor === cursor && result.next_id === cursorId) {
      throw new Error(`Listing runs for agent ${agentId} returned a repeated page cursor`);
    }
    cursor = result.next_cursor;
    cursorId = result.next_id;
  }
}

async function findAgentRunForRoutine(
  officeApi: { rawRequest: (method: string, path: string) => Promise<Response> },
  agentId: string,
  routineId: string,
  seenRunIds: Set<string>,
): Promise<string> {
  const runs = await listAgentRuns(officeApi, agentId);
  return runs.find((run) => !seenRunIds.has(run.id) && run.routine_id === routineId)?.id ?? "";
}

test.describe("Office taskless routine sessions", () => {
  test("fires a real taskless routine twice without creating task rows", async ({
    officeApi,
    apiClient,
    officeSeed,
  }) => {
    test.setTimeout(360_000);
    const before = await apiClient.listTasks(officeSeed.workspaceId);
    const routine = await officeApi.createRoutine(officeSeed.workspaceId, {
      name: `Taskless E2E ${Date.now()}`,
      description: "Taskless routine session smoke test",
      assignee_agent_profile_id: officeSeed.agentId,
    });
    const routineId = routine.id as string;

    const existing = await listAgentRuns(officeApi, officeSeed.agentId);
    const seen = new Set(existing.map((run) => run.id));
    const sessions: string[] = [];
    for (let attempt = 1; attempt <= 2; attempt += 1) {
      const response = await officeApi.runRoutine(routineId);
      expect(response.status).toBe(200);
      await expect
        .poll(() => routineRuns(officeApi, routineId), { timeout: 20_000 })
        .toHaveLength(attempt);
      let runId = "";
      await expect
        .poll(
          async () => {
            runId = await findAgentRunForRoutine(officeApi, officeSeed.agentId, routineId, seen);
            return runId;
          },
          { timeout: 90_000, message: `Waiting for routine ${routineId} to create an agent run` },
        )
        .not.toBe("");
      seen.add(runId);
      const detailPath = `/agents/${officeSeed.agentId}/runs/${runId}`;
      await expect
        .poll(
          async () => {
            const result = await officeApi.rawRequest("GET", detailPath);
            expect(result.ok).toBe(true);
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
