package main

import (
	"bytes"
	"testing"
)

// TestClipboardWriteDoesNotSwallowConcurrentCopy pins the fix for a silent
// data-loss bug: clipboardWrite used to re-read clipChangeCount() AFTER
// writing, so a copy the user made in that window had its changeCount recorded
// as ours. clipboardRead only reports a CHANGED count, so that copy was never
// seen again -- it simply never synced, with no error anywhere. Observed live
// on pro 2026-08-05: a page of text sat in the clipboard un-synced while both
// peers reported the previous value.
func TestClipboardWriteDoesNotSwallowConcurrentCopy(t *testing.T) {
	// First read of the process: lastChangeCount is 0, so this always returns
	// the real clipboard. Keep it to put the user back where they were.
	saved, err := clipboardRead()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if saved != nil {
			clipboardWrite(saved)
		}
	}()

	const userCopy = "user-copy-during-write"
	afterWriteHook = func() { externalPasteboardWrite([]byte(userCopy)) }
	defer func() { afterWriteHook = nil }()

	if err := clipboardWrite(&ClipboardContent{Type: 'T', Data: []byte("synced-from-peer")}); err != nil {
		t.Fatal(err)
	}

	got, err := clipboardRead()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("clipboardRead returned nil: the concurrent copy was swallowed and will never sync")
	}
	if !bytes.Equal(got.Data, []byte(userCopy)) {
		t.Errorf("got %q, want %q", got.Data, userCopy)
	}
}
