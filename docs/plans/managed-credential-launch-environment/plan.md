---
created: 2026-09-25
status: implemented
requirements:
  - REQ-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001
system_design:
  - ../../specs/platform/system-design/managed-credential-launch-environment.md
legacy_specs: []
---

# Implementation Plan: Managed credential launch environment

## Overview

A fresh launch stores its composed environment, including the managed
credential broker values, as the execution's runtime snapshot. When no
`SetExecutionEnv` overlay was delivered, `configureAndStartAgent` still ran
`composeExecutionRuntimeEnvironment`, which strips the helper path, broker URL,
and lease as obsolete while keeping the managed `credential.helper` entry.
`agentctl` then ran Git with a helper that expanded
`${KANDEV_GITHUB_CREDENTIAL_HELPER_PATH}` to an empty command, and an HTTPS
push from the task UI failed with `terminal prompts disabled`. The failure was
observed on a plugin-provider (Forgejo) repository, where the launch lease is
the only credential the task has.

## Scope

### In scope

- Send the runtime snapshot unchanged when no per-run overlay exists.

### Out of scope

- Overlay composition, credential issuance, lease reissue, and resume
  launches.

## Technical approach

Add a `hasRuntimeEnvOverlay` check on the `runtime_env` metadata key in
`configureAndStartAgent`, between the no-snapshot branch and the composition
branch. Nothing else changes.

## Tests

| Acceptance criterion | Evidence |
| --- | --- |
| `AC-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001.1` | `TestConfigureAndStartAgentKeepsLaunchManagedGitCredentials` in `manager_launch_credentials_test.go`: a snapshot with broker values and a managed helper entry reaches agentctl intact. |
| `AC-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001.2` | Existing `configureAndStartAgent` and `composeExecutionRuntimeEnvironment` tests in the lifecycle package keep passing. |

## Work orders

- [x] [Task 01: Keep managed credentials on an overlay-free launch](task-01-keep-launch-credentials.md)

## Verification results

- `cd apps/backend && go test ./internal/agent/runtime/lifecycle -count=1` passes.
- `gofmt -l` and `go vet` are clean for `./internal/agent/runtime/lifecycle`.
