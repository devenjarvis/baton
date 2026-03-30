# In-Place Agent Interaction Design

**Date:** 2026-03-29
**Status:** Approved

## Problem

Currently, interacting with an agent requires pressing `enter` to enter a fullscreen focus view, losing sight of the agent list sidebar. Users need to type longer prompts/responses to agents frequently, and want to keep the sidebar status context visible while doing so.

## Goal

Allow the user to type into an agent's terminal directly from the dashboard — keeping the sidebar visible — then return to sidebar navigation naturally after submitting input.

## Approach

Add a `panelFocus` concept to the dashboard: the left sidebar and right preview panel each hold focus independently. Right arrow shifts keyboard input to the preview terminal; the sidebar remains visible and unchanged.

## State

Add a `panelFocus` field to `dashboardModel`:

```go
type panelFocus int

const (
    focusList     panelFocus = iota // default: j/k navigate the list
    focusTerminal                    // right panel is interactive
)
```

## Key Bindings

### `focusList` (default)

| Key | Action |
|-----|--------|
| `j` / `k` / `↑` / `↓` | Navigate agent list |
| `→` | Enter `focusTerminal` for selected agent |
| `n` | New agent (prompt overlay) |
| `d` | Diff selected agent |
| `x` | Kill selected agent |
| `m` | Merge selected agent |
| `q` / `ctrl+c` | Quit |

### `focusTerminal`

| Key | Action |
|-----|--------|
| All keys | Forwarded to agent via `agent.SendKey()` |
| `enter` | Forward to agent, then return to `focusList` |
| `esc` | Return to `focusList`, not forwarded |

`←` and `→` are forwarded normally for cursor movement within the agent terminal — they do not switch panels.

## Visual Feedback

- **Preview panel border**: Colored border (using `ColorSecondary`) when `focusTerminal`; no border when `focusList` (current behavior).
- **Statusbar hints** swap on panel focus:
  - `focusList`: `j/k navigate  →  interact  n new  d diff  x kill  m merge  q quit`
  - `focusTerminal`: `enter send  esc back`

## Code Changes

### Delete
- `internal/tui/focus.go` — entire file removed

### Modify

**`internal/tui/keymap.go`**
- Add `panelFocus` type and `focusList` / `focusTerminal` constants

**`internal/tui/dashboard.go`**
- Add `panelFocus` field to `dashboardModel`
- Handle `→` in `focusList` to enter `focusTerminal`
- In `focusTerminal`: forward keys via `agent.SendKey()`; handle `enter` (forward + return to `focusList`) and `esc` (return only)
- Render colored border on preview panel when `focusTerminal`

**`internal/tui/statusbar.go`**
- Add `focusTerminalHints`
- Remove `focusHints` (no longer needed)

**`internal/tui/app.go`**
- Remove `ViewFocus` constant
- Remove `focusModel` field from `App`
- Remove `updateFocus()` method
- Remove `focusExitMsg` type
- Remove focus resize logic in `WindowSizeMsg` handler
- Remove focus case in `View()`
- Pass `dashboard.panelFocus` state to statusbar hint selection in `View()`

## What Is Not Changing

- Dashboard layout (sidebar width, preview width) — unchanged
- Agent list navigation in `focusList` — unchanged
- All overlay views (prompt, merge, diff) — unchanged
- Fullscreen ViewFocus mode is removed entirely; `enter` on the dashboard is a no-op
