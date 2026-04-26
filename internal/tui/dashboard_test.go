package tui

import "testing"

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
