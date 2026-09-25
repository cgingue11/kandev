---
created: 2026-09-25
status: implemented
requirements:
  - REQ-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001
system_design:
  - ../../specs/platform/system-design/github-cli-shim-reentry-guard.md
legacy_specs: []
---

# Implementation Plan: GitHub CLI shim re-entry guard

## Overview

`agentctl`'s `gh` shim removed only `$KANDEV_GITHUB_CLI_SHIM_DIR` from `PATH`
before looking up the real `gh`. When that variable was empty or named a
different shim directory (a stale directory left on `PATH` by an earlier
`agentctl`), the lookup returned the shim itself and each invocation launched
another one. On one host this produced about 1,500 `gh pr view --json url`
processes, filled RAM and swap, and took Kandev down.

## Scope

### In scope

- Skip the running `agentctl` binary, and links to it, during the real-CLI
  lookup.
- Mark the child environment and refuse to run when the marker is already set.

### Out of scope

- Cleaning stale shim directories out of an inherited `PATH`.
- Any change to lease redemption or the shim's `gh` configuration isolation.

## Technical approach

Both guards live in `apps/backend/cmd/agentctl`. `main.go` builds the
self-skipping lookup from `os.Executable`. `github_cli_shim.go` checks the
marker before any other work and sets it on the child it launches.

## Tests

| Acceptance criterion | Evidence |
| --- | --- |
| `AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.1` | `TestLookPathSkippingExecutableIgnoresLinksToSelf` in `github_cli_shim_test.go`: a symlinked shim ahead of the real CLI on `PATH` is skipped. |
| `AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.2` | `TestGitHubCLIShimMarksChildEnvironment`: the launched CLI sees the marker. |
| `AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.3` | `TestGitHubCLIShimRefusesToReenterItself`: a marked environment errors before any launch. |
| `AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.4` | Existing `TestGitHubCLIShim*` cases in `github_cli_shim_test.go` keep passing. |

## Work orders

- [x] [Task 01: Guard the gh shim against re-entry](task-01-guard-shim-reentry.md)

## Verification results

- `cd apps/backend && go test ./cmd/agentctl -count=1` passes.
- `gofmt -l` and `go vet` are clean for `./cmd/agentctl`.
