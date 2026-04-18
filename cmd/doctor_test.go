package cmd

import (
	"testing"
)

// TestCheckHookPipeline exercises the doctor self-check: it builds the baton
// binary, invokes checkHookPipeline against it, and asserts the socket round
// trip succeeds. Uses buildBaton from hook_test.go.
func TestCheckHookPipeline(t *testing.T) {
	bin := buildBaton(t)
	if err := checkHookPipeline(bin); err != nil {
		t.Fatalf("checkHookPipeline: %v", err)
	}
}

// TestCheckHookPipelineBadBinary verifies the check surfaces an actionable
// error when the baton binary path is wrong.
func TestCheckHookPipelineBadBinary(t *testing.T) {
	err := checkHookPipeline("/nonexistent/baton-binary")
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}
