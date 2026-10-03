package fixture

import (
	"testing"

	"github.com/babarot/claude-recall/internal/db"
)

func TestArchive(t *testing.T) {
	d, err := db.Open(Archive(t), db.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s, err := d.ListSessions(db.ListOptions{})
	if err != nil || len(s) != 2 {
		t.Fatalf("got %d sessions, %v", len(s), err)
	}
}
