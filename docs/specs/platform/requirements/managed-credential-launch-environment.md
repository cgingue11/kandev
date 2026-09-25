---
status: active
system: platform
created: 2026-09-25
owners:
  - kandev
---

# Managed credential launch environment requirements

## Overview

A managed task launch composes the agent subprocess environment once and keeps
it as the execution's runtime snapshot. That snapshot carries the managed Git
credential helper path, broker URL, and lease that the generated Git
`credential.helper` entries expand. Platform owns the shared session launch
contract. The [integration system](../../integrations/README.md) owns how the
credentials are issued.

## Terminology

- **Runtime snapshot:** The environment composed for a launch and kept on the
  execution.
- **Per-run overlay:** Environment values delivered after launch through
  `SetExecutionEnv` for the next subprocess start.
- **Managed credential values:** The helper path, broker URL, and lease that
  the Kandev-generated Git credential helper entries reference.

## Requirements

### REQ-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001: Launch keeps managed credential values

**Intent:** A fresh managed task session can fetch and push over HTTPS with the
credentials issued for its launch.

#### Acceptance criteria

- **AC-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001.1:** When an
  execution starts from a runtime snapshot and no per-run overlay exists, the
  agent subprocess shall receive the snapshot unchanged, so every generated
  helper entry expands to a runnable command.
- **AC-PLATFORM-MANAGED-CREDENTIAL-LAUNCH-ENVIRONMENT-001.2:** When a per-run
  overlay exists, the subprocess environment shall be the snapshot composed
  with the overlay, with stale managed credential values and generated host
  helper entries removed before composition, as before this requirement.

## Out of scope

- Which credential source a workspace uses, and lease reissue after rotation.
  See [Git credential lease reissue](git-credential-lease-reissue.md).
- Resume launches, which carry their own credential snapshot. See [agent resume
  runtime recovery](../../agents/requirements/agent-resume-runtime-recovery.md).
