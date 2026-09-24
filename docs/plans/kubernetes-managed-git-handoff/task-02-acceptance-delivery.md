---
id: "02-acceptance-delivery"
title: "Validate private Git and deliver reviewed PR"
status: in_progress
wave: 2
depends_on:
  - "01-configure-handoff"
plan: "plan.md"
requirements:
  - REQ-EXECUTORS-KUBERNETES-TASK-POD-001
acceptance_criteria:
  - AC-EXECUTORS-KUBERNETES-TASK-POD-001.4
  - AC-EXECUTORS-KUBERNETES-TASK-POD-001.9
  - AC-EXECUTORS-KUBERNETES-TASK-POD-001.10
system_design:
  - ../../specs/executors/system-design/kubernetes-task-pod.md
---

# Task 02: Disposable acceptance and PR delivery

## Summary

Validate the implemented handoff with actual private Git operations from the
agent subprocess, then publish the isolated branch and follow CI/review fixups.
Report exact head, results and any remaining acceptance blockers. Do not merge.

## In scope

- Inspect and adapt the historical receipts and harness under
  `/tmp/intel-git-evidence-20260924`; never replay hardcoded deleted resources.
- Reproduce current-base live behavior first if access permits, then repeat the
  same fixture with backend and agentctl built from the fixed head.
- Use an isolated temporary home/database/ports and disposable cluster namespace,
  task, workspace, profiles and claims. Explicitly select managed task Git access
  with a `gh_cli` connection. Never set `KANDEV_E2E_MOCK=true`.
- Use the supplied pinned worker digest, non-root UID 1000, bounded resources,
  dedicated scheduling constraints and disabled service-account token mounting.
  Keep the HTTPS broker route restricted and validate its certificate with a
  temporary trusted CA; no insecure TLS switches.
- Ask the actual agent to run noninteractive `ls-remote` and a shallow fetch of
  private `zeval/intelligence`, first fresh, then after Stop/Resume. Record the
  dynamically observed advertised and fetched commits, not an old expected head.
- Verify same retained Pod/PVC UIDs, foreign `zeval/koi.git` helper denial,
  broker failure/revocation behavior, and presence-only subprocess diagnostics.
  Assert raw `GH_TOKEN`/`GITHUB_TOKEN` absence. Never print helper credentials.
- Tear down only owned resources, processes, credential copies, CA keys and
  temporary data. Preserve sanitized command/result receipts.
- Update public executor/Git documentation only for verified behavior and keep
  this package's status/results accurate. Load commit, PR and PR-fixup skills;
  retain hooks and the repository PR template. Resolve actionable reviews and CI
  failures and report the exact final head. No merge, rollout or deployment.

## Out of scope

Main Kandev service, main executor profiles, previous #3835 worktree, production
workspaces and credentials, scheduled automation and unrelated acceptance gaps.

## Acceptance

1. Real agent private reads/fetches succeed fresh and resumed, retaining expected
   resource identities; foreign-repository redemption fails with verified TLS.
2. Sanitized evidence identifies tested commit, resource continuity and cleanup;
   unavailable live prerequisites are explicitly reported rather than marked passed.
3. PR is open with required checks and review findings dispositioned on the exact
   head; task remains unmerged and undeployed.

## Verification

The disposable harness must be adapted after inspection; record its exact
command, environment contract, instance identifiers and cleanup commands before
running it. Current historical scripts are not an executable acceptance recipe.
Run these documentation gates from the repository root:

```bash
node --test scripts/validate-public-docs.test.mjs
node scripts/validate-public-docs.mjs
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
git diff --check -- docs/plans/kubernetes-managed-git-handoff
git status --short -- docs/plans/kubernetes-managed-git-handoff
```

After creating the PR, use `scripts/pr-await <PR>` for the authorized wait and
`scripts/pr-resolve list <PR>` for findings. Follow the loaded PR-fixup skill for
exact-head checks and actionable remediations. Backend fixups also run the
repository-required full changed-code lint against the actual PR base:
`golangci-lint run ./... --new-from-rev="<base-sha>" --timeout=5m` from `apps/backend`.

## Files likely touched

- `docs/public/executors.md` and `docs/public/git-operations.md`, if clarification is needed.
- This plan and its work orders, with actual final results.
- Only implementation files required by valid CI/review findings.

## Dependencies

Task 01 passing targeted validation.

## Risks

Private credentials and cluster access may be unavailable. Do not substitute
mock-token success or preparation-only success for actual-agent acceptance.
Any blocker must identify the missing prerequisite without exposing a secret.

## Parallelism

`sequential`

## Inputs

- User-supplied acceptance report and sanitized historical evidence.
- [Plan](plan.md), task 01 results and current repository delivery skills.

## Results

Live acceptance completed with real Codex on Ocean in disposable namespace
`git-handoff-o7u9z451`, using the pinned worker digest specified above.

- Built backend and agentctl from base `d60274528129dc17d413357c104c9c427c4e7af9`.
  Fresh and retained-pod resumed agent commands failed while preparation passed.
- Built backend and agentctl with this repair. Actual fresh and resumed agent
  commands both passed private `ls-remote` and shallow fetch. Both observed
  `59d1df9585e70556ca1477a3a91ef12f4b94bd0a` as advertised and fetched HEAD.
- Fixed Pod UID `3bd45feb-b1e4-4946-91ed-bc2d8bb98284` and PVC UID
  `07876735-30e8-4270-b8a2-281c66d90d9b` were unchanged across Stop/Resume.
- Foreign-repository helper lookup returned no credentials. Real Codex environment
  contained broker fields but no raw `GH_TOKEN`/`GITHUB_TOKEN`. A helper call with
  an untrusted CA failed without credentials. Deleting only the disposable
  workspace's GitHub connection also caused the existing lease to fail.
- Archived disposable tasks, deleted the namespace and all owned PVs, stopped
  the owned backend/proxy, and deleted the private home, copied credentials,
  kubeconfig, TLS key and databases. Main instance/profiles were not modified.

Adapted harness commands were run from
`/tmp/kandev-managed-git-handoff-20260924/`: `python3 setup.py`, `cluster.py`,
`launch.py base`, `register.py`, `start.py base`, `control.py base stop`,
`control.py base resume`, `control.py base message`, `switch.py`, `launch.py fixed`,
`start.py fixed`, and the matching fixed stop/resume/message commands. `status.py`
recorded sanitized receipts; `security-probe.py` ran inside the discovered Pod
for untrusted-CA and connection-revocation checks. `cleanup.py` verified teardown.
These scripts used loopback backend port 18347, Tailscale TLS port 18348, an
isolated credential home and explicit managed policy; no mock profile was enabled.
The exact scripts and sanitized JSON receipts remain in that evidence directory.
Private resource state and logs were deleted during cleanup.

Public docs: executor guide updated, no UI screenshots required. Public docs
validator passed for 47 pages; its 62 tests passed. Specification catalog,
specification lint and diff whitespace checks passed.

PR publication and current-head CI/review disposition remain pending.
