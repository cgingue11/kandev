---
status: active
system: platform
created: 2026-09-25
owners:
  - kandev
---

# GitHub CLI shim re-entry guard requirements

## Overview

A managed task session receives a `gh` shim ahead of the real GitHub CLI on
`PATH`. The shim redeems a broker lease and launches the real CLI with that
token. When the real-CLI lookup resolves back to a shim, every invocation
starts another shim, and the host fills with `gh` processes until it runs out
of memory. Platform owns this runtime-safety contract. The [integration
system](../../integrations/README.md) owns GitHub authentication itself.

## Terminology

- **Shim:** The `agentctl` binary invoked under the name `gh` from a
  Kandev-managed shim directory.
- **Shim directory:** The directory Kandev places ahead of the real CLI on
  `PATH` and names in `KANDEV_GITHUB_CLI_SHIM_DIR`.
- **Real CLI:** A `gh` executable that is not the running `agentctl` binary or
  a link to it.
- **Re-entry marker:** A non-secret environment variable that the shim sets
  for the real CLI it launches.

## Requirements

### REQ-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001: Bounded shim resolution

**Intent:** One shim invocation launches at most one child process, and that
child is never another shim, whatever directories `PATH` contains.

#### Acceptance criteria

- **AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.1:** When the shim resolves
  the real CLI, it shall skip every `PATH` candidate that is the running
  `agentctl` executable or a symbolic or hard link to it, including a candidate
  in a shim directory other than the one named by `KANDEV_GITHUB_CLI_SHIM_DIR`.
- **AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.2:** When the shim launches
  the real CLI, the child environment shall carry the re-entry marker.
- **AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.3:** When the shim starts
  with the re-entry marker already set, it shall exit with a non-zero status
  and an error that names the re-entry, and it shall launch no process.
- **AC-PLATFORM-GITHUB-CLI-SHIM-REENTRY-GUARD-001.4:** When a real CLI exists
  on `PATH`, the shim shall launch it with the same token, isolated
  configuration directory, and shim-free `PATH` as before this guard.

## Out of scope

- Broker lease issuance, reissue, and scope checks. See [Git credential lease
  reissue](git-credential-lease-reissue.md).
- Removing stale shim directories from an inherited `PATH`.
