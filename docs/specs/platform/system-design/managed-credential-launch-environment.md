---
status: current
system: platform
requirements:
  - REQ-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001
created: 2026-09-25
owners:
  - kandev
---

# Managed credential launch environment system design

## Purpose and boundaries

This design covers how `configureAndStartAgent` in
`apps/backend/internal/agent/runtime/lifecycle` chooses the environment for
the agent subprocess. It does not change how the executor issues managed
credentials, how `SetExecutionEnv` stores an overlay, or how
`composeExecutionRuntimeEnvironment` merges one.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001` | [Control flow](#control-flow), [Failure and recovery](#failure-and-recovery) |

## Components and responsibilities

- `AgentExecution.RuntimeEnvironment` returns the launch-composed snapshot.
- `Manager.SetExecutionEnv` stores a per-run overlay under the `runtime_env`
  execution metadata key.
- `hasRuntimeEnvOverlay` reports whether that key is present.
- `composeExecutionRuntimeEnvironment` in `profile_env.go` removes obsolete
  managed credential values and generated host GitHub helper entries from a
  base environment, then merges the overlay.

## Control flow

`configureAndStartAgent` picks one of three environment sources:

1. No snapshot: build from the overlay and the agent profile environment.
2. Snapshot and no overlay: send the snapshot unchanged.
3. Snapshot and overlay: compose the snapshot with the overlay.

Before this design, case 2 fell through to case 3 with an empty overlay.
Composition strips `githubauth.CredentialHelperPathEnv`,
`githubauth.CredentialBrokerURLEnv`, and `githubauth.CredentialLeaseEnv`
through `removeObsoleteManagedCredentialEnvironment`, but keeps the generated
`credential.helper` entry that expands
`KANDEV_GITHUB_CREDENTIAL_HELPER_PATH`. Git then ran an empty helper command,
and an HTTPS push from the task failed with `terminal prompts disabled`.

## Failure and recovery

Case 2 sends the launch snapshot as-is, so no composition error can occur on
that path. Cases 1 and 3 keep their existing error handling and execution
error reporting.

## Security

No change to where credential values live. The snapshot stays in memory on the
execution, and the lease remains an opaque bearer value that is never logged.

## Implementation plan

- [Managed credential launch environment](../../../plans/managed-credential-launch-environment/plan.md)
