package main

import (
	"errors"
	"fmt"
	"testing"
)

// TestProgress_TriggerAbort_FirstReasonWins checks that once an abort is
// triggered, later calls don't overwrite the original reason -- the first
// confirmed corruption reported should be the one surfaced to the user.
func TestProgress_TriggerAbort_FirstReasonWins(t *testing.T) {
	prog := &Progress{}

	if aborted, _ := prog.aborted(); aborted {
		t.Fatal("aborted() = true before triggerAbort was ever called")
	}

	first := errors.New("first corruption")
	second := errors.New("second corruption")
	prog.triggerAbort(first)
	prog.triggerAbort(second)

	aborted, err := prog.aborted()
	if !aborted {
		t.Fatal("aborted() = false after triggerAbort was called")
	}
	if err != first {
		t.Errorf("aborted() reason = %v, want the first error reported (%v)", err, first)
	}
}

// TestHandleFileSyncError_ChecksumMismatchTriggersAbort verifies that
// confirmed corruption -- a checksum mismatch after copy -- is what makes
// cpgo stop, per the safety requirement that a detected corruption halts
// processing immediately rather than just being logged and skipped.
func TestHandleFileSyncError_ChecksumMismatchTriggersAbort(t *testing.T) {
	prog := &Progress{}

	// Real callers wrap this with fmt.Errorf("...: %w", ...); errors.Is
	// must still see through that wrapping, so wrap it here too.
	wrapped := fmt.Errorf("copy failed after 3 attempts: %w", ErrChecksumMismatch)
	handleFileSyncError(prog, "some/file.bin", wrapped)

	aborted, _ := prog.aborted()
	if !aborted {
		t.Error("handleFileSyncError did not trigger an abort for a checksum mismatch")
	}
	if prog.Failed.Load() != 1 {
		t.Errorf("Failed = %d, want 1", prog.Failed.Load())
	}
}

// TestHandleFileSyncError_OtherErrorsDoNotAbort checks the other side: an
// ordinary failure (e.g. a permission error) for one file should not stop
// the sync of every other file, only a confirmed checksum mismatch should.
func TestHandleFileSyncError_OtherErrorsDoNotAbort(t *testing.T) {
	prog := &Progress{}

	handleFileSyncError(prog, "some/file.bin", errors.New("permission denied"))

	if aborted, _ := prog.aborted(); aborted {
		t.Error("handleFileSyncError triggered an abort for a non-corruption error")
	}
	if prog.Failed.Load() != 1 {
		t.Errorf("Failed = %d, want 1", prog.Failed.Load())
	}
}
