package db_test

import (
	"testing"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/fixture"
)

func TestSessionMessages(t *testing.T) {
	d, err := db.Open(fixture.Archive(t), db.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	msgs, err := d.SessionMessages(fixture.AppSession)
	if err != nil || len(msgs) != 4 {
		t.Fatalf("got %d messages, %v", len(msgs), err)
	}
	if msgs[0].Role != "user" || msgs[0].Content != "How do I configure the terraform module for staging?" || msgs[3].Role != "assistant" {
		t.Fatalf("got %+v", msgs)
	}
	// It is what the preview takes its ends from.
	p, err := d.SessionPreview(fixture.AppSession, 1, 1)
	if err != nil || p.Head[0] != msgs[0] || p.Tail[0] != msgs[3] || p.Skipped != 2 {
		t.Fatalf("preview %+v, %v", p, err)
	}
}
