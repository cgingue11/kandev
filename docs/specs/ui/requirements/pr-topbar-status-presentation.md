---
status: active
system: ui
created: 2026-09-25
owners:
  - kandev
---

# PR topbar status presentation requirements

## Overview

The task topbar shows a GitHub pull request as a colored PR icon, its number,
and a second status icon. The second icon repeats the color signal and can look
like a close action. UI owns this compact presentation. The shared GitHub PR
glyph can also show a conflict bubble on the leading icon. The GitHub
integration owns PR state, conflict observation, and status derivation.

## Requirements

### REQ-UI-PR-TOPBAR-BADGE-001: Single PR badge

**Intent:** A user can identify a linked PR and its status without mistaking a
status symbol for an action.

#### Acceptance criteria

- **AC-UI-PR-TOPBAR-BADGE-001.1:** When a task has exactly one linked GitHub PR,
  its topbar badge shall show the shared leading PR glyph and PR number, with
  no trailing status icon. This applies to open, draft, merged, closed, and
  blocked PRs.
- **AC-UI-PR-TOPBAR-BADGE-001.2:** The leading PR glyph shall use the GitHub
  task indicator's current base status-color precedence as PR data changes and
  shall show a separate conflict bubble when a conflict is confirmed. Removing
  the trailing icon shall not change the PR's state or base color. Draft and CI
  status can coexist with the independent conflict bubble.
- **AC-UI-PR-TOPBAR-BADGE-001.3:** The single-PR badge shall remain one
  keyboard-accessible control. Activation shall open the PR detail panel, and
  pointer hover or keyboard focus shall continue to expose readable status
  details. Its accessible name or description shall identify the PR and convey
  status in localized text so color is not the only accessible cue.
- **AC-UI-PR-TOPBAR-BADGE-001.4:** When a task has multiple linked GitHub PRs,
  the topbar badge shall retain its PR count, aggregate glyph status color,
  conflict bubble when applicable, and dropdown arrow. The dropdown shall
  remain usable and identify each PR's own conflict state.
- **AC-UI-PR-TOPBAR-BADGE-001.5:** On phones, users shall continue to read PR
  status and open details through the existing PR status chip and drawer. This
  topbar change shall not remove that touch path.

## Out of scope

- Changing GitHub PR state, synchronization, merge rules, or status derivation.
- Changing GitLab, Azure DevOps, or registered provider topbar controls.
- Removing status icons from the chat PR chip, task rows, or detailed status
  views.

## Related requirements

- [GitHub PR conflict indicator](../../integrations/requirements/github-pr-conflict-indicator.md) owns the confirmed conflict signal and shared badge behavior.

## Implementation plan

[PR topbar badge simplification](../../../plans/pr-topbar-badge-simplification/plan.md)
