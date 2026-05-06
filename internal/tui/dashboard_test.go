package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/github"
	"github.com/devenjarvis/baton/internal/hook"
)

func TestSelectionRect_Inactive(t *testing.T) {
	d := dashboardModel{}
	if _, _, _, _, ok := d.selectionRect(); ok {
		t.Error("selectionRect on zero dashboard should report ok=false")
	}

	// active but no drag: still not a usable rect.
	d.selection = selection{anchorX: 1, anchorY: 1, cursorX: 1, cursorY: 1, active: true}
	if _, _, _, _, ok := d.selectionRect(); ok {
		t.Error("selectionRect with dragSeen=false should report ok=false")
	}
}

func TestSelectionRect_AnchorBeforeCursor(t *testing.T) {
	d := dashboardModel{
		selection: selection{
			anchorX: 2, anchorY: 1,
			cursorX: 7, cursorY: 4,
			active: true, dragSeen: true,
		},
	}
	sx, sy, ex, ey, ok := d.selectionRect()
	if !ok {
		t.Fatal("expected ok=true for drag-seen selection")
	}
	if sx != 2 || sy != 1 || ex != 7 || ey != 4 {
		t.Errorf("anchor-before-cursor: got (%d,%d)-(%d,%d), want (2,1)-(7,4)", sx, sy, ex, ey)
	}
}

func TestSelectionRect_AnchorAfterCursor(t *testing.T) {
	d := dashboardModel{
		selection: selection{
			anchorX: 7, anchorY: 4,
			cursorX: 2, cursorY: 1,
			active: true, dragSeen: true,
		},
	}
	sx, sy, ex, ey, ok := d.selectionRect()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if sx != 2 || sy != 1 || ex != 7 || ey != 4 {
		t.Errorf("anchor-after-cursor: got (%d,%d)-(%d,%d), want (2,1)-(7,4)", sx, sy, ex, ey)
	}
}

func TestSelectionRect_SameRowAnchorAfterCursor(t *testing.T) {
	// Drag right-to-left on the same row should still produce a normalized
	// left-to-right rect.
	d := dashboardModel{
		selection: selection{
			anchorX: 9, anchorY: 3,
			cursorX: 4, cursorY: 3,
			active: true, dragSeen: true,
		},
	}
	sx, sy, ex, ey, ok := d.selectionRect()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if sx != 4 || sy != 3 || ex != 9 || ey != 3 {
		t.Errorf("same-row reverse drag: got (%d,%d)-(%d,%d), want (4,3)-(9,3)", sx, sy, ex, ey)
	}
}

func TestSelectionRect_MultiRowReverseDrag(t *testing.T) {
	// Drag bottom-up: anchor on a later row but cursor X to the right of anchor X.
	// Normalization is by row first, so anchor's row becomes the bottom-right.
	d := dashboardModel{
		selection: selection{
			anchorX: 2, anchorY: 5,
			cursorX: 10, cursorY: 1,
			active: true, dragSeen: true,
		},
	}
	sx, sy, ex, ey, ok := d.selectionRect()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if sx != 10 || sy != 1 || ex != 2 || ey != 5 {
		t.Errorf("reverse multi-row drag: got (%d,%d)-(%d,%d), want (10,1)-(2,5)", sx, sy, ex, ey)
	}
}

func TestClearSelection(t *testing.T) {
	d := dashboardModel{
		selection: selection{
			anchorX: 1, anchorY: 1, cursorX: 5, cursorY: 5,
			active: true, dragSeen: true, agentID: "abc",
		},
	}
	d.clearSelection()
	if d.selection.active || d.selection.dragSeen || d.selection.agentID != "" {
		t.Errorf("clearSelection left residue: %+v", d.selection)
	}
	if _, _, _, _, ok := d.selectionRect(); ok {
		t.Error("after clearSelection, selectionRect should report ok=false")
	}
}

// tickerDashboard builds a minimal dashboardModel for ticker tests.
func tickerDashboard(sidebarW int, sessions ...*agent.Session) dashboardModel {
	d := newDashboardModel()
	d.sidebarWidth = sidebarW
	d.prCache = make(map[string]*prCacheEntry)
	d.closingSessions = make(map[string]bool)
	for _, s := range sessions {
		d.items = append(d.items, listItem{kind: listItemSession, session: s})
	}
	return d
}

// past returns a time in the past so ticker pause/advance checks pass immediately.
func past() time.Time { return time.Now().Add(-time.Hour) }

func TestTickerSlice_Basic(t *testing.T) {
	got := tickerSlice("hello world", 6, 5)
	if got != "world" {
		t.Errorf("got %q, want %q", got, "world")
	}
}

func TestTickerSlice_OffsetAtEnd(t *testing.T) {
	got := tickerSlice("hello", 5, 10)
	if got != "" {
		t.Errorf("offset=len: got %q, want %q", got, "")
	}
}

func TestTickerSlice_OffsetPastEnd(t *testing.T) {
	got := tickerSlice("hello", 10, 5)
	if got != "" {
		t.Errorf("offset>len: got %q, want %q", got, "")
	}
}

func TestTickerSlice_MultibyteRunes(t *testing.T) {
	// "日本語" — 3 runes, each 2 display cells wide; offset=1 skips "日".
	got := tickerSlice("日本語", 1, 10)
	if got != "本語" {
		t.Errorf("multibyte: got %q, want %q", got, "本語")
	}
}

func TestAdvanceTickers_NameFits_NoTickerCreated(t *testing.T) {
	// sidebarW=30 → maxNameLen=20; "short" (5 chars) fits easily.
	sess := &agent.Session{ID: "s1", Name: "short"}
	d := tickerDashboard(30, sess)
	d.advanceTickers(time.Now())
	if _, exists := d.tickers["s1"]; exists {
		t.Error("ticker should not be created for a name that fits")
	}
}

func TestAdvanceTickers_NameFits_ClearsStale(t *testing.T) {
	// Stale ticker entry from a previous long name should be removed.
	sess := &agent.Session{ID: "s1", Name: "short"}
	d := tickerDashboard(30, sess)
	d.tickers["s1"] = &sessionTicker{offset: 5}
	d.advanceTickers(time.Now())
	if _, exists := d.tickers["s1"]; exists {
		t.Error("stale ticker should be removed when name fits")
	}
}

func TestAdvanceTickers_OverflowCreatesTickerWithPause(t *testing.T) {
	// sidebarW=30 → maxNameLen=20; long name (26 chars) overflows.
	longName := "abcdefghijklmnopqrstuvwxyz"
	sess := &agent.Session{ID: "s1", Name: longName}
	d := tickerDashboard(30, sess)
	now := time.Now()
	d.advanceTickers(now)
	tk := d.tickers["s1"]
	if tk == nil {
		t.Fatal("expected ticker to be created for overflowing name")
	}
	if tk.offset != 0 {
		t.Errorf("offset: got %d, want 0", tk.offset)
	}
	if !tk.pauseUntil.After(now) {
		t.Error("ticker should start in paused state")
	}
}

func TestAdvanceTickers_AdvancePastPause_IncrementsOffset(t *testing.T) {
	longName := "abcdefghijklmnopqrstuvwxyz"
	sess := &agent.Session{ID: "s1", Name: longName}
	d := tickerDashboard(30, sess)
	// Pre-seed expired ticker so initial pause is already over.
	d.tickers["s1"] = &sessionTicker{pauseUntil: past(), nextAdvance: past()}
	d.advanceTickers(time.Now())
	tk := d.tickers["s1"]
	if tk.offset != 1 {
		t.Errorf("offset after advance: got %d, want 1", tk.offset)
	}
}

func TestAdvanceTickers_WideCharName_ScrollsNotStuck(t *testing.T) {
	// 12 CJK runes = 24 display cells, overflows maxNameLen=20.
	// len(fullRunes) = 14. Old rune-count check (offset+20 >= 14) would fire at
	// offset=0 (0+20=20 >= 14), preventing the name from ever scrolling.
	wideName := strings.Repeat("日", 12)
	sess := &agent.Session{ID: "s1", Name: wideName}
	d := tickerDashboard(30, sess)
	d.tickers["s1"] = &sessionTicker{pauseUntil: past(), nextAdvance: past()}
	d.advanceTickers(time.Now())
	tk := d.tickers["s1"]
	if tk.atEnd {
		t.Error("wide-char name: atEnd should not fire on first advance")
	}
	if tk.offset != 1 {
		t.Errorf("wide-char name: offset after first advance: got %d, want 1", tk.offset)
	}
}

// TestPreviewMetadataRows verifies the row count used for mouse coordinate
// mapping: 2 baseline (sessionInfo + blank), +1 for a single PR, +N for stack.
func TestPreviewMetadataRows(t *testing.T) {
	makeSession := func(id string) *agent.Session {
		return &agent.Session{ID: id, Name: id}
	}

	tests := []struct {
		name       string
		cacheEntry *prCacheEntry
		want       int
	}{
		{
			name:       "no PR",
			cacheEntry: nil,
			want:       2,
		},
		{
			name:       "single PR, no stack",
			cacheEntry: &prCacheEntry{pr: &github.PRState{Number: 1}},
			want:       3,
		},
		{
			name: "stacked 2-deep",
			cacheEntry: &prCacheEntry{
				pr: &github.PRState{Number: 2},
				stack: []*prCacheEntry{
					{pr: &github.PRState{Number: 1}},
				},
			},
			want: 4,
		},
		{
			name: "stacked 3-deep",
			cacheEntry: &prCacheEntry{
				pr: &github.PRState{Number: 3},
				stack: []*prCacheEntry{
					{pr: &github.PRState{Number: 2}},
					{pr: &github.PRState{Number: 1}},
				},
			},
			want: 5,
		},
		{
			name: "stack with nil entry is skipped",
			cacheEntry: &prCacheEntry{
				pr:    &github.PRState{Number: 2},
				stack: []*prCacheEntry{nil},
			},
			want: 3, // nil stack entry not counted
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sess := makeSession("s1")
			d := newDashboardModel()
			d.prCache = make(map[string]*prCacheEntry)
			d.items = []listItem{{kind: listItemSession, session: sess}}
			d.selected = 0
			if tc.cacheEntry != nil {
				d.prCache["s1"] = tc.cacheEntry
			}
			if got := d.previewMetadataRows(); got != tc.want {
				t.Errorf("previewMetadataRows() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAdvanceTickers_EndReached_SnapsBack(t *testing.T) {
	// sidebarW=30 → maxNameLen=20
	// longName=26 chars → fullName "…" + " ·" = 28 runes
	// end condition: offset+20 >= 28 → offset >= 8
	longName := "12345678901234567890123456"
	sess := &agent.Session{ID: "s1", Name: longName}
	d := tickerDashboard(30, sess)

	// Set offset to 7 (one step before end); advance once → hits 8 → atEnd=true.
	d.tickers["s1"] = &sessionTicker{offset: 7, pauseUntil: past(), nextAdvance: past()}
	d.advanceTickers(time.Now())
	tk := d.tickers["s1"]
	if !tk.atEnd {
		t.Fatal("expected atEnd=true after reaching end")
	}
	if tk.offset != 8 {
		t.Errorf("offset at end: got %d, want 8", tk.offset)
	}

	// Simulate end-pause expiry and advance again → snap back.
	tk.pauseUntil = past()
	tk.nextAdvance = past()
	beforeSnap := time.Now()
	d.advanceTickers(time.Now())
	if tk.offset != 0 {
		t.Errorf("offset after snap: got %d, want 0", tk.offset)
	}
	if tk.atEnd {
		t.Error("atEnd should be false after snap")
	}
	if !tk.pauseUntil.After(beforeSnap) {
		t.Error("snap should set a fresh start pause")
	}
}

// TestSessionFocusPriority_DefaultIsIdle verifies that a session whose agents
// have not received any status-changing events (StatusStarting, the zero value)
// reports priority 3 (idle/other).
func TestSessionFocusPriority_DefaultIsIdle(t *testing.T) {
	sess := &agent.Session{ID: "s1", Name: "s1"}
	sess.SetLifecyclePhase(agent.LifecycleInProgress)
	ag := &agent.Agent{Name: "a1"}

	d := newDashboardModel()
	d.prCache = make(map[string]*prCacheEntry)
	d.items = []listItem{
		{kind: listItemSession, session: sess},
		{kind: listItemAgent, session: sess, agent: ag},
	}

	if got := d.sessionFocusPriority(sess); got != 3 {
		t.Errorf("sessionFocusPriority with StatusStarting agent: got %d, want 3", got)
	}
}

// TestSessionFocusPriority_ActiveBeforeIdle verifies that a session with an
// active agent (priority 2) sorts ahead of one with only idle/default agents
// (priority 3). We drive StatusActive via OnHookEvent(KindSessionStart).
func TestSessionFocusPriority_ActiveBeforeIdle(t *testing.T) {
	sessActive := &agent.Session{ID: "sa", Name: "active"}
	sessActive.SetLifecyclePhase(agent.LifecycleInProgress)
	agActive := &agent.Agent{Name: "ag-active"}
	// Drive to StatusActive.
	agActive.OnHookEvent(hook.Event{Kind: hook.KindSessionStart})

	sessIdle := &agent.Session{ID: "si", Name: "idle"}
	sessIdle.SetLifecyclePhase(agent.LifecycleInProgress)
	agIdle := &agent.Agent{Name: "ag-idle"}

	d := newDashboardModel()
	d.prCache = make(map[string]*prCacheEntry)
	d.items = []listItem{
		{kind: listItemSession, session: sessActive},
		{kind: listItemAgent, session: sessActive, agent: agActive},
		{kind: listItemSession, session: sessIdle},
		{kind: listItemAgent, session: sessIdle, agent: agIdle},
	}

	pa := d.sessionFocusPriority(sessActive)
	pi := d.sessionFocusPriority(sessIdle)
	if pa != 2 {
		t.Errorf("active session priority: got %d, want 2", pa)
	}
	if pi != 3 {
		t.Errorf("idle session priority: got %d, want 3", pi)
	}
}

// TestAllInProgressSessions_SortOrder verifies that after building the result
// slice, sessions with lower priority (more urgent) appear first.
// Session order in d.items: idle first, then active — after sort active should
// be first.
func TestAllInProgressSessions_SortOrder(t *testing.T) {
	sessIdle := &agent.Session{ID: "si", Name: "idle"}
	sessIdle.SetLifecyclePhase(agent.LifecycleInProgress)
	agIdle := &agent.Agent{Name: "ag-idle"}

	sessActive := &agent.Session{ID: "sa", Name: "active"}
	sessActive.SetLifecyclePhase(agent.LifecycleInProgress)
	agActive := &agent.Agent{Name: "ag-active"}
	// Drive to StatusActive.
	agActive.OnHookEvent(hook.Event{Kind: hook.KindSessionStart})

	d := newDashboardModel()
	d.prCache = make(map[string]*prCacheEntry)
	// Idle session is listed first in items; active second — sort should invert.
	d.items = []listItem{
		{kind: listItemSession, session: sessIdle},
		{kind: listItemAgent, session: sessIdle, agent: agIdle},
		{kind: listItemSession, session: sessActive},
		{kind: listItemAgent, session: sessActive, agent: agActive},
	}

	sessions := d.allInProgressSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].session != sessActive {
		t.Errorf("first session after sort should be active (priority 2), got %q", sessions[0].session.Name)
	}
	if sessions[1].session != sessIdle {
		t.Errorf("second session after sort should be idle (priority 3), got %q", sessions[1].session.Name)
	}
}
