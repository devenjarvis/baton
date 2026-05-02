package tui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/github"
)

// TestPrPollInterval_BurstOverridesBaseline verifies the burst window shortens
// the poll interval to 2s regardless of the adaptive baseline.
func TestPrPollInterval_BurstOverridesBaseline(t *testing.T) {
	a := NewApp()
	ps := &prSessionState{burstUntil: time.Now().Add(30 * time.Second)}
	a.prPollStates["s1"] = ps
	if got := a.prPollInterval("s1", ps); got != 2*time.Second {
		t.Fatalf("burst interval = %v, want 2s", got)
	}
}

func TestPrPollInterval_ExpiredBurstFallsBackToBaseline(t *testing.T) {
	a := NewApp()
	ps := &prSessionState{burstUntil: time.Now().Add(-5 * time.Second)}
	a.prPollStates["s1"] = ps
	if got := a.prPollInterval("s1", ps); got != 30*time.Second {
		t.Fatalf("expired burst should use 30s baseline, got %v", got)
	}
}

// TestBranchRenamedEventArmsBurst verifies that feeding an EventBranchRenamed
// via agentEventMsg sets burstUntil in the future and resets SHA/poll state
// so the next tick re-queries immediately.
func TestBranchRenamedEventArmsBurst(t *testing.T) {
	a := NewApp()
	// Seed prior state so we can verify the handler resets it.
	a.prPollStates["sess-1"] = &prSessionState{
		lastPoll:      time.Now(),
		lastSHACheck:  time.Now(),
		lastRemoteSHA: "oldsha",
	}

	model, _ := a.Update(agentEventMsg{
		event: agent.Event{
			Type:      agent.EventBranchRenamed,
			SessionID: "sess-1",
			Branch:    "baton/new-name",
		},
	})
	got := model.(App).prPollStates["sess-1"]
	if got == nil {
		t.Fatal("prPollStates missing after event")
	}
	if !got.burstUntil.After(time.Now().Add(50 * time.Second)) {
		t.Errorf("burstUntil should be ~60s in the future, got %v", got.burstUntil)
	}
	if !got.lastPoll.IsZero() {
		t.Errorf("lastPoll should be reset, got %v", got.lastPoll)
	}
	if got.lastRemoteSHA != "" {
		t.Errorf("lastRemoteSHA should be cleared, got %q", got.lastRemoteSHA)
	}
}

// TestPrPollMsg_ErrorPreservesCache verifies that a fetch error does not
// clobber a previously-cached PR entry.
func TestPrPollMsg_ErrorPreservesCache(t *testing.T) {
	a := NewApp()
	a.prPollsInFlight = 1
	a.prPollStates["sess-1"] = &prSessionState{inFlight: true}
	prev := &prCacheEntry{}
	a.prCache["sess-1"] = prev

	model, _ := a.Update(prPollMsg{sessionID: "sess-1", err: errors.New("boom")})
	got := model.(App)
	if got.prCache["sess-1"] != prev {
		t.Errorf("cache entry was clobbered on error")
	}
	if got.prPollStates["sess-1"].inFlight {
		t.Errorf("inFlight should be cleared after poll result")
	}
	if got.prPollsInFlight != 0 {
		t.Errorf("prPollsInFlight = %d, want 0", got.prPollsInFlight)
	}
}

// TestPrPollMsg_NilGracePeriod verifies the 2-consecutive-nil grace period:
// the first nil preserves the cache entry, the second nil evicts it.
func TestPrPollMsg_NilGracePeriod(t *testing.T) {
	a := NewApp()
	a.prPollsInFlight = 1
	a.prPollStates["sess-1"] = &prSessionState{inFlight: true, lastCheckState: "success"}
	a.prCache["sess-1"] = &prCacheEntry{}

	// First nil: cache should be preserved.
	model, _ := a.Update(prPollMsg{sessionID: "sess-1"})
	got := model.(App)
	if _, ok := got.prCache["sess-1"]; !ok {
		t.Errorf("cache entry should be preserved on first nil poll (grace period)")
	}
	if got.prPollStates["sess-1"].consecutiveNilPolls != 1 {
		t.Errorf("consecutiveNilPolls = %d, want 1", got.prPollStates["sess-1"].consecutiveNilPolls)
	}

	// Second nil: cache should be cleared.
	got.prPollsInFlight = 1
	got.prPollStates["sess-1"].inFlight = true
	model2, _ := got.Update(prPollMsg{sessionID: "sess-1"})
	got2 := model2.(App)
	if _, ok := got2.prCache["sess-1"]; ok {
		t.Errorf("cache entry should be cleared after second consecutive nil poll")
	}
	if got2.prPollStates["sess-1"].lastCheckState != "" {
		t.Errorf("lastCheckState should reset, got %q", got2.prPollStates["sess-1"].lastCheckState)
	}
	if got2.prPollStates["sess-1"].consecutiveNilPolls != 0 {
		t.Errorf("consecutiveNilPolls should reset to 0 after eviction, got %d", got2.prPollStates["sess-1"].consecutiveNilPolls)
	}
}

// TestPrPollMsg_NilThenSuccessResetsCounter verifies that a successful poll
// after a nil resets the consecutiveNilPolls counter so the grace period
// starts fresh on the next nil.
func TestPrPollMsg_NilThenSuccessResetsCounter(t *testing.T) {
	a := NewApp()
	a.prPollsInFlight = 1
	a.prPollStates["sess-1"] = &prSessionState{inFlight: true}
	a.prCache["sess-1"] = &prCacheEntry{}

	// First nil: increments counter.
	model, _ := a.Update(prPollMsg{sessionID: "sess-1"})
	got := model.(App)
	if got.prPollStates["sess-1"].consecutiveNilPolls != 1 {
		t.Fatalf("consecutiveNilPolls after first nil = %d, want 1", got.prPollStates["sess-1"].consecutiveNilPolls)
	}

	// Successful poll: counter resets.
	got.prPollsInFlight = 1
	got.prPollStates["sess-1"].inFlight = true
	model2, _ := got.Update(prPollMsg{sessionID: "sess-1", pr: &github.PRState{Number: 1}})
	got2 := model2.(App)
	if got2.prPollStates["sess-1"].consecutiveNilPolls != 0 {
		t.Errorf("consecutiveNilPolls should reset to 0 after success, got %d", got2.prPollStates["sess-1"].consecutiveNilPolls)
	}
}

// TestPrPollMsg_NilWithNoPriorCacheIsNoop verifies that a successful empty
// lookup for a session that never had a PR doesn't create spurious state.
func TestPrPollMsg_NilWithNoPriorCacheIsNoop(t *testing.T) {
	a := NewApp()
	a.prPollsInFlight = 1
	a.prPollStates["sess-1"] = &prSessionState{inFlight: true}

	model, _ := a.Update(prPollMsg{sessionID: "sess-1"})
	got := model.(App)
	if _, ok := got.prCache["sess-1"]; ok {
		t.Errorf("no cache entry should exist")
	}
	if got.prPollStates["sess-1"].inFlight {
		t.Errorf("inFlight should be cleared")
	}
}

// Ensure the test file participates in the package even when the above tests
// are filtered out via -run.
var _ = tea.Batch
