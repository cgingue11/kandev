---
id: "01-simplify-single-pr-badge"
title: "Use the shared left PR glyph"
status: complete
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-UI-PR-TOPBAR-BADGE-001
acceptance_criteria:
  - AC-UI-PR-TOPBAR-BADGE-001.1
  - AC-UI-PR-TOPBAR-BADGE-001.2
  - AC-UI-PR-TOPBAR-BADGE-001.3
  - AC-UI-PR-TOPBAR-BADGE-001.4
  - AC-UI-PR-TOPBAR-BADGE-001.5
system_design:
  - ../../specs/ui/system-design/pr-topbar-status-presentation.md
---

# Task 01: Use the shared left PR glyph

## Summary

Remove the trailing status glyph from the single GitHub PR topbar badge. Use
the shared status-colored and conflict-aware leading glyph, followed by its
number. Preserve accessible status text, detail access, the multi-PR selector,
and the phone PR status drawer. Deliver this with
[conflict indicator Task 02](../github-pr-conflict-indicator/task-02-task-pr-badge.md).

## In scope

- Update the single-PR topbar rendering to use the shared GitHub glyph and
  clean up unused local trailing-icon logic.
- Provide localized status text for the badge's accessible name or description.
- Add focused component and desktop Playwright coverage; run the existing phone
  drawer scenario.

## Out of scope

- Provider state, base color precedence, and PR merge behavior.
- Other provider badges and status icons outside the single GitHub PR topbar.

## Acceptance

1. A single GitHub PR badge has one leading shared PR glyph and its number, no
   trailing status SVG, the existing base color for representative PR states,
   and a separate conflict bubble when confirmed.
2. Keyboard, hover, and click access still reveal localized status and PR
   details; the badge exposes its status in accessible text.
3. A multiple-PR badge retains its count, aggregate color and conflict bubble,
   and dropdown arrow; its menu rows identify their own conflict. The phone PR
   status drawer remains reachable.

## ASCII UI preview

`UI-01: Single GitHub PR badge`, task topbar. This is an excerpt from the
[combined preview](plan.md#ascii-ui-preview) for
`AC-UI-PR-TOPBAR-BADGE-001.1` through `.3`.

```text
Current desktop:   [ red PR icon  #3932  red X ]
Proposed desktop:  [ red PR icon! #3932       ]
                   ! = confirmed conflict; absent otherwise
                   hover/focus -> readable CI and review popover
                   click       -> PR detail panel
```

`UI-02: Multiple GitHub PRs` and `UI-03: Phone PR status` remain as shown in
the combined preview. Spacing and the number are illustrative; the icon order
and missing trailing single-PR glyph are required. Compare the rendered badge
at desktop width and confirm the existing phone chip and drawer at Pixel 5.

## Verification

Run from the repository root after implementation. If this is a fresh
worktree without `apps/node_modules`, run `(cd apps && pnpm install
--frozen-lockfile)` once before the commands below. Follow `/tdd` and `/e2e`:
the new tests must fail before the component change, then pass afterward.

```bash
(cd apps/web && pnpm exec vitest run components/github/pr-status-refresh-routes.test.tsx components/github/pr-task-icon.render.test.tsx)
(cd apps/web && pnpm exec eslint components/github/pr-topbar-button.tsx components/github/pr-status-glyph.tsx)
(cd apps/web && pnpm run typecheck)
(cd apps/web && pnpm run i18n:check)
(cd apps/web && pnpm e2e:run --project chromium tests/pr/pr-topbar-popover.spec.ts -- --grep "single badge shows a conflict bubble")
(cd apps/web && pnpm e2e:run --no-build --project mobile-chrome tests/pr/mobile-pr-ci-chip.spec.ts -- --grep "tapping the chip opens the drawer with PR CI content")
git diff --check
```

The managed E2E runner rebuilds the production web bundle before each run.
Confirm each focused Playwright command discovers at least one test.

## Files likely touched

- `apps/web/components/github/pr-topbar-button.tsx`
- `apps/web/components/github/pr-topbar-button.test.tsx` (new)
- `apps/web/components/github/pr-status-glyph.tsx` (owned by companion work order)
- `apps/web/components/integrations/change-request-status-chrome.tsx`
- `apps/web/e2e/tests/pr/pr-topbar-popover.spec.ts`
- `apps/web/e2e/tests/pr/pr-multi-popover.spec.ts`
- `apps/web/src/locales/*/github.json` if a new accessible label is needed

## Dependencies

Conflict indicator Task 01 supplies conflict state; Task 02 supplies the
shared glyph. This companion task runs in the same frontend implementation
pass as Task 02, not as a second independent edit.

## Risks

- The badge needs readable status for assistive technology after the distinct
  status shape is removed. Keep that text synchronized with the PR state.
- Test selectors must inspect only the topbar badge, not icons inside its
  popover or detail panel.

## Parallelism

`sequential`

## Inputs

- [Requirements](../../specs/ui/requirements/pr-topbar-status-presentation.md)
- [System design](../../specs/ui/system-design/pr-topbar-status-presentation.md)
- `apps/web/components/integrations/change-request-status-chrome.tsx`
- `apps/web/e2e/tests/pr/mobile-pr-ci-chip.spec.ts`

## Results

The single badge displays the shared leading glyph and PR number with no
trailing status glyph. The component test and desktop Playwright scenario
cover localized status text, focus disclosure, conflict warning, and click to
details. The multi-PR Playwright scenario confirms the retained chevron and
per-row conflict status. The existing phone drawer scenario passes. Typecheck,
ESLint, i18n checks, production build, and specification validation pass.
