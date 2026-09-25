---
created: 2026-09-25
status: complete
requirements:
  - REQ-UI-PR-TOPBAR-BADGE-001
system_design:
  - ../../specs/ui/system-design/pr-topbar-status-presentation.md
legacy_specs: []
---

# Implementation plan: Simplify the PR topbar badge

## Overview

Remove the status glyph after the number in the single GitHub PR topbar badge.
Use the shared leading GitHub PR glyph from the
[conflict indicator plan](../github-pr-conflict-indicator/plan.md), including
its independent conflict bubble. Keep detail interactions and the multi-PR
dropdown. This companion work order records top-bar acceptance in the same
implementation pass as that plan's Task 02.

## Scope

### In scope

- Single GitHub PR topbar badge presentation and accessible status text.
- Focused component and desktop browser coverage.
- Confirmation that the existing phone PR status drawer remains available.

### Out of scope

- PR state derivation, provider synchronization, merge actions, or persistence.
- Multi-PR dropdown action and other provider topbar buttons.
- Status glyphs in the composer chip, task rows, and detailed popovers.

## Technical approach

In `apps/web/components/github/pr-topbar-button.tsx`, stop passing
`statusIcon` from `PRSingleButton` to `ChangeRequestTopbarContent`. Supply the
shared GitHub leading glyph from Task 02 of the conflict indicator plan, using
`getPRStatusColor(pr)` for its base color and the independent confirmed
conflict observation for its bubble. Remove unused local trailing-status and
blocker helpers. Keep `PRMultiButton`'s `IconChevronDown`,
`aggregatePRStatusColor`, and menu. Give the single badge localized accessible
status text derived from the same PR snapshot without changing its visible
number or click/hover/focus handlers. Reuse existing locale terms when possible.

Extend `ChangeRequestTopbarContent` with an optional leading glyph slot while
retaining its default icon and optional `statusIcon` for registered providers.
The phone uses `PRStatusChip` and its drawer; this desktop topbar component
does not mount there.

## ASCII UI preview

`UI-01: Single GitHub PR badge`, task topbar, failing checks. Current layout is
confirmed by `PRSingleButton` and `PRStatusIcon`; proposed layout applies to
every PR state. Brackets represent the existing single clickable badge.

```text
Current desktop:   [ red PR icon  #3932  red X ]
Proposed desktop:  [ red PR icon! #3932       ]
                   ! = confirmed conflict; absent otherwise
                   hover/focus -> readable CI and review popover
                   click       -> PR detail panel
```

`UI-02: Multiple GitHub PRs`, task topbar. The trailing arrow remains because
it identifies the dropdown action.

```text
Desktop: [ aggregate-color PR icon!  2 PRs  v ] -> PR selector
          ! appears when any open PR conflicts
```

`UI-03: Phone PR status`, existing phone task view. The desktop PR badge is not
mounted. Its current touch entry point and drawer remain.

```text
Phone chat status: [ CI status chip ] -> PR status drawer
```

The icon/number order, conflict bubble, absence of the single-PR trailing glyph, and retained
multi-PR arrow are requirements (`AC-UI-PR-TOPBAR-BADGE-001.1` and `.4`). The
spacing and example PR number are illustrative. The phone path maps to `.5`.
The work order's desktop Playwright assertion and existing phone scenario are
the targeted rendered checks.

## Tests

- `AC-UI-PR-TOPBAR-BADGE-001.1` and `.2`:
  `apps/web/components/github/pr-status-refresh-routes.test.tsx` checks the
  shared glyph, number, warning bubble, absent trailing glyph, and accessible
  checks/conflict text.
- `AC-UI-PR-TOPBAR-BADGE-001.3`: desktop Playwright checks focus disclosure
  and click navigation.
- `AC-UI-PR-TOPBAR-BADGE-001.4`: component coverage checks the retained
  multi-PR chevron.

## E2E tests

- `AC-UI-PR-TOPBAR-BADGE-001.1` through `.3`: the focused
  `apps/web/e2e/tests/pr/pr-topbar-popover.spec.ts` scenario seeds failing
  checks, changes requested, and a conflict, then checks the badge, keyboard
  disclosure, and PR detail action under `chromium`.
- `AC-UI-PR-TOPBAR-BADGE-001.5`: run the existing drawer scenario in
  `apps/web/e2e/tests/pr/mobile-pr-ci-chip.spec.ts` under `mobile-chrome`.

## Work orders

- [x] [Task 01: Use the shared left PR glyph](task-01-simplify-single-pr-badge.md)

This task is delivered with [conflict indicator Task 02](../github-pr-conflict-indicator/task-02-task-pr-badge.md), after its data contract in Task 01. Keep both work-order results synchronized.

## Verification results

The focused component test checks the leading glyph, absent trailing status
icon, conflict bubble, localized accessible status, and retained multi-PR
chevron. Desktop single- and multi-PR Playwright scenarios confirm focus
disclosure, click-to-detail, and per-row conflict labels. The existing phone
PR drawer Playwright scenario passes. Typecheck, ESLint, i18n checks,
production build, and specification validation pass.

## Risks

- Removing the trailing status shape makes localized accessible status and the
  existing text disclosure necessary for users who cannot distinguish colors.
- Existing component or browser tests may assume a second SVG in the badge;
  update only assertions that represent this topbar presentation.
