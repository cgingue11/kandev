---
id: "01-keep-launch-credentials"
title: "Keep managed credentials on an overlay-free launch"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001
acceptance_criteria:
  - AC-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001.1
  - AC-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001.2
system_design:
  - ../../specs/platform/system-design/managed-credential-launch-environment.md
---

# Task 01: Keep managed credentials on an overlay-free launch

## Summary

When an execution has a runtime snapshot and no `runtime_env` overlay, send
the snapshot to agentctl unchanged instead of composing it with an empty
overlay, which strips the managed credential values the helper entries need.

## In scope

- Add `hasRuntimeEnvOverlay` and the snapshot-only branch in
  `configureAndStartAgent`.
- Add a regression test that captures the environment agentctl receives.

## Out of scope

- Changing `composeExecutionRuntimeEnvironment` or `SetExecutionEnv`.

## Acceptance

- With a snapshot and no overlay, the configured environment carries the
  helper path, broker URL, lease, and the managed helper entry.
- With an overlay, composition behaves as before.

## Verification

```bash
(cd apps/backend && go test ./internal/agent/runtime/lifecycle -run 'TestConfigureAndStartAgentKeepsLaunchManagedGitCredentials' -count=1)
(cd apps/backend && go test ./internal/agent/runtime/lifecycle -count=1)
```

## Files likely touched

- `apps/backend/internal/agent/runtime/lifecycle/manager_launch.go`
- `apps/backend/internal/agent/runtime/lifecycle/manager_launch_credentials_test.go`

## Dependencies

None.

## Risks

- A caller that relied on composition to strip stale credential values from a
  fresh launch would now see them. No such caller exists: only
  `SetExecutionEnv` writes `runtime_env`, and it always supplies the overlay.

## Parallelism

`sequential`

## Inputs

- `REQ-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001` and its system
  design.

## Results

Implemented the snapshot-only branch and the regression test. Verification
passed:

```bash
(cd apps/backend && go test ./internal/agent/runtime/lifecycle -count=1)
```
