---
status: active
system: integrations
created: 2026-09-25
owners:
  - kandev
---

# GitHub PR Conflict Indicator Requirements

## Overview

A linked GitHub pull request can have failing checks and merge conflicts at the
same time. A single PR icon color does not reveal both conditions. The GitHub
integration owns the conflict observation and its visible indicator; a shared
glyph presents it in task rows and the task top bar.

## Requirements

### REQ-INTEGRATIONS-GITHUB-PR-CONFLICT-INDICATOR-001: Independent conflict warning

**Intent:** Show a merge-conflict warning alongside the current pull-request
status so users can recognize both conditions at a glance.

#### Acceptance criteria

- **AC-INTEGRATIONS-GITHUB-PR-CONFLICT-INDICATOR-001.1:** When GitHub confirms that an open linked pull request has merge conflicts, its PR status glyph shall show a warning bubble at the glyph's top-right corner while retaining the icon's existing status color and placement.
- **AC-INTEGRATIONS-GITHUB-PR-CONFLICT-INDICATOR-001.2:** When failing CI or requested changes coexist with a confirmed conflict, the existing red status and the conflict bubble shall both remain visible. Draft, queued, pending, and other status treatments shall likewise retain their meaning when conflict information is independently available.
- **AC-INTEGRATIONS-GITHUB-PR-CONFLICT-INDICATOR-001.3:** When several PRs belong to one task, the task-row and top-bar aggregate glyphs shall show the warning if any open PR has a confirmed conflict. Each top-bar PR menu row shall show the warning only for its own conflicted PR. Terminal PRs shall not keep the aggregate warning visible for an open sibling.
- **AC-INTEGRATIONS-GITHUB-PR-CONFLICT-INDICATOR-001.4:** When GitHub confirms that a conflict cleared, the warning shall disappear after the existing PR update reaches each visible surface. Missing or unknown mergeability shall not create a conflict warning.
- **AC-INTEGRATIONS-GITHUB-PR-CONFLICT-INDICATOR-001.5:** The warning shall remain visible on compact task rows before full PR details load, in the desktop sidebar, Home Kanban card and pipeline row, rich task list, phone task switcher, and task top bar where the GitHub PR indicator appears.
- **AC-INTEGRATIONS-GITHUB-PR-CONFLICT-INDICATOR-001.6:** The warning shall not obscure the PR glyph, PR count, top-bar number or dropdown arrow, or existing automation indicators. Each complete PR indicator shall have localized accessible text that identifies its status and conflicts. A compact summary label shall not claim a specific check or review outcome, or merge readiness, unless that summary establishes it. The warning shall not create another action or change existing task-row navigation, top-bar click, menu, hover, or touch-drawer behavior.
- **AC-INTEGRATIONS-GITHUB-PR-CONFLICT-INDICATOR-001.7:** When a conflicted PR has failing CI or requested changes, the single-PR top bar shall show the current status color and conflict bubble on its leading PR glyph, followed only by the PR number. A trailing status icon shall not appear.

## Related requirements

- [PR task status summary](../../ui/requirements/pr-task-status-summary.md) owns the shared disclosure and established icon-color precedence.
- [Sidebar task row presentation](../../ui/requirements/sidebar-task-row-presentation.md) owns task-row placement, including the trailing status option.
- [PR topbar status presentation](../../ui/requirements/pr-topbar-status-presentation.md) owns the single-PR badge layout and interaction.

## Out of scope

- Changing CI, review, draft, queue, or merge-readiness rules.
- Changing GitLab or registered provider indicators.
- Adding a conflict-resolution action or a separate warning button.
- Changing GitHub polling cadence or loading all PR details for a task row.

## Implementation plan

[GitHub PR conflict indicator plan](../../../plans/github-pr-conflict-indicator/plan.md)
