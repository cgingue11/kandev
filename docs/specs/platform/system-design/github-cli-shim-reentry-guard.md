---
status: current
system: platform
requirements:
  - REQ-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001
created: 2026-09-25
owners:
  - kandev
---

# GitHub CLI shim re-entry guard system design

## Purpose and boundaries

The shim lives in `apps/backend/cmd/agentctl` (`github_cli_shim.go` and
`main.go`). This design adds two guards to it. It changes nothing about lease
redemption, the isolated `GH_CONFIG_DIR`, or how the [integration
system](../../integrations/system-design/github-authentication-03.md) installs
the shim directory on `PATH`.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001` | [Control flow](#control-flow), [Failure and recovery](#failure-and-recovery) |

## Components and responsibilities

- `runGitHubUtilityCommand` in `main.go` detects a shim invocation from
  `os.Args[0]`, resolves the running binary with `os.Executable`, and passes a
  self-skipping lookup to the shim.
- `lookPathSkippingExecutable(self)` returns a `githubCLILookPath` that rejects
  any candidate for which `os.SameFile` matches `self`. It wraps the existing
  `lookPathIn` search order, including the Windows extension list, and falls
  back to `lookPathIn` when `self` cannot be stat-ed.
- `runGitHubCLIShim` refuses to run when `KANDEV_GITHUB_CLI_SHIM_ACTIVE` is set
  in its own environment, and sets it to `1` in the child environment it hands
  to the real CLI.

## Control flow

1. Refuse immediately when `KANDEV_GITHUB_CLI_SHIM_ACTIVE` is already set.
2. Redeem the broker lease as before.
3. Strip the directory named by `KANDEV_GITHUB_CLI_SHIM_DIR` from `PATH`, then
   search the remaining `PATH` for `gh` with the self-skipping lookup.
4. Launch the result with `GH_TOKEN`, `GH_CONFIG_DIR`, the shim-free `PATH`,
   and the marker.

The lookup guard is the primary defense. It compares file identity, so a shim
reached through a symlink or a stale directory left by an earlier `agentctl`
is skipped without any environment cooperation. The marker is the backstop for
a shim that is a distinct copy of the binary rather than a link.

## Failure and recovery

- No real CLI on `PATH`: the existing `find real gh CLI` error, unchanged.
- Re-entry detected: the shim returns `gh shim re-entered itself: the gh found
  on PATH is a Kandev shim, not the GitHub CLI` and exits through
  `githubUtilityExitCode`. The command fails once instead of recursing.

## Security

The marker is a non-secret flag. It carries no token, lease, or path, and it
is set only in the child environment of the real CLI.

## Observability

The re-entry error reaches the caller's stderr. No new logs or metrics.

## Implementation plan

- [GitHub CLI shim re-entry guard](../../../plans/github-cli-shim-reentry-guard/plan.md)
