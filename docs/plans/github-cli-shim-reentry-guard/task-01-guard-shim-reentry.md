---
id: "01-guard-shim-reentry"
title: "Guard the gh shim against re-entry"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001
acceptance_criteria:
  - AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.1
  - AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.2
  - AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.3
  - AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.4
system_design:
  - ../../specs/platform/system-design/github-cli-shim-reentry-guard.md
---

# Task 01: Guard the gh shim against re-entry

## Summary

Stop the `gh` shim from launching another copy of itself when the real-CLI
lookup resolves to a shim, so a stale shim directory on `PATH` fails one
command instead of exhausting the host.

## In scope

- Add `lookPathSkippingExecutable`, which rejects any `PATH` candidate that is
  the same file as the running `agentctl`, and use it from `main.go`.
- Add the `KANDEV_GITHUB_CLI_SHIM_ACTIVE` marker: refuse to run when it is set,
  set it on the child environment otherwise.

## Out of scope

- Removing stale shim directories from `PATH`.
- Lease redemption, reissue, or `gh` configuration isolation.

## Acceptance

- A symlink to `agentctl` ahead of the real `gh` on `PATH` is skipped and the
  real `gh` is launched.
- A shim started with the marker set returns an error naming the re-entry and
  launches nothing.
- Existing shim behavior for a valid real CLI is unchanged.

## Verification

```bash
(cd apps/backend && go test ./cmd/agentctl -run 'TestGitHubCLIShim|TestLookPathSkippingExecutable' -count=1)
(cd apps/backend && go test ./cmd/agentctl -count=1)
```

## Files likely touched

- `apps/backend/cmd/agentctl/github_cli_shim.go`
- `apps/backend/cmd/agentctl/github_cli_shim_test.go`
- `apps/backend/cmd/agentctl/main.go`

## Dependencies

None.

## Risks

- A real `gh` that is a hard link to `agentctl` cannot exist, so the identity
  check cannot reject a legitimate CLI.

## Parallelism

`sequential`

## Inputs

- `REQ-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001` and its system design.

## Results

Implemented both guards. The self-skipping lookup reuses `lookPathIn`'s search
order through a shared `lookPathMatching` helper. Verification passed:

```bash
(cd apps/backend && go test ./cmd/agentctl -count=1)
```
