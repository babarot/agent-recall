package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
)

// withImage is a session whose first request has an image pasted into it.
type withImage struct {
	fakePreview
	png   []byte
	asked int
}

func (f *withImage) SessionDetail(id string) (*db.Detail, error) {
	d, err := f.fakePreview.SessionDetail(id)
	d.First = &db.Message{UUID: "u1", Role: "user", Content: "[Image #1]\nwhy does it look like this?", Timestamp: now}
	return d, err
}

func (f *withImage) GetImage(session, uuid string, index int) (string, []byte, bool, error) {
	f.asked++
	if uuid != "u1" || index != 0 {
		return "", nil, false, nil
	}
	return "image/png", f.png, true, nil
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range m.Pix {
		m.Pix[i] = 0x80
		if i%4 == 3 {
			m.Pix[i] = 0xff // opaque
		}
	}
	m.Set(0, 0, color.White)
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func newImageModel(t *testing.T, on bool) (Model, *withImage) {
	t.Helper()
	cfg := config.Default().TUI
	cfg.Images = on
	src := &withImage{png: testPNG(t, 800, 400)}
	m := New(testSessions(t), src, cfg)
	m.now = func() time.Time { return now }
	return update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40}), src
}

func TestImagesShowInTheSpreadConversation(t *testing.T) {
	m, src := newImageModel(t, true)
	m = press(t, m, "space")
	// Until the image is read, its marker shows.
	if s := screen(m); !strings.Contains(s, "[Image #1]") {
		t.Fatalf("the marker should show while the image is read:\n%s", s)
	}
	k := imageKey{m.current().s.ID, "u1", 0}
	if !m.images.loading[k] {
		t.Fatalf("the image should be read once asked for")
	}
	m = update(t, m, loadImage(src, k))
	s := m.render()
	if strings.Contains(ansi.Strip(s), "[Image #1]") || !strings.ContainsRune(s, kitty.Placeholder) {
		t.Fatalf("the image should replace its marker:\n%s", ansi.Strip(s))
	}
	if !strings.Contains(ansi.Strip(s), "why does it look like this?") {
		t.Fatalf("the text around the image should stay:\n%s", ansi.Strip(s))
	}
	for i, l := range strings.Split(s, "\n") {
		if w := ansi.StringWidth(l); w > m.width {
			t.Fatalf("line %d is %d cells wide, over %d", i, w, m.width)
		}
	}
	// 800x400 pixels is 100 columns by 25 rows, held to 20 rows.
	if img := m.images.held[k]; img.cols != 80 || img.rows != 20 {
		t.Fatalf("placed over %dx%d cells, want 80x20", img.cols, img.rows)
	}
	if src.asked != 1 {
		t.Fatalf("read %d times, want once", src.asked)
	}
}

func TestImagesOff(t *testing.T) {
	m, src := newImageModel(t, false)
	m = press(t, m, "space")
	if s := m.render(); !strings.Contains(ansi.Strip(s), "[Image #1]") || strings.ContainsRune(s, kitty.Placeholder) || src.asked != 0 {
		t.Fatalf("with images off, the marker shows and nothing is read (read %d):\n%s", src.asked, ansi.Strip(s))
	}
}

func TestImageThatCannotBeReadKeepsItsMarker(t *testing.T) {
	m, src := newImageModel(t, true)
	src.png = []byte("not an image")
	m = press(t, m, "space")
	k := imageKey{m.current().s.ID, "u1", 0}
	m = update(t, m, loadImage(src, k))
	if s := screen(m); !strings.Contains(s, "[Image #1]") || m.images.loading[k] || !m.images.failed[k] {
		t.Fatalf("an unreadable image should stay a marker, and not be read again:\n%s", s)
	}
}

func TestImageCells(t *testing.T) {
	for _, c := range []struct{ w, h, max, cols, rows int }{
		{800, 400, 200, 80, 20}, // held to maxImageRows
		{1000, 200, 60, 60, 6},  // held to the width
		{64, 32, 200, 8, 2},     // not blown up past pixelsPerCell
		{100, 2000, 80, 2, 20},  // tall and thin
		{2000, 10, 100, 100, 1}, // a sliver is still a row
	} {
		if cols, rows := imageCells(c.w, c.h, c.max); cols != c.cols || rows != c.rows {
			t.Errorf("imageCells(%d, %d, %d) = %d, %d, want %d, %d", c.w, c.h, c.max, cols, rows, c.cols, c.rows)
		}
	}
}

func TestShrink(t *testing.T) {
	m, _, err := image.Decode(bytes.NewReader(testPNG(t, 2000, 1000)))
	if err != nil {
		t.Fatal(err)
	}
	s := shrink(m, 1000)
	if b := s.Bounds(); b.Dx() != 1000 || b.Dy() != 500 {
		t.Fatalf("shrunk to %v, want 1000x500", b)
	}
	if r, _, _, _ := s.At(500, 250).RGBA(); r>>8 != 0x80 {
		t.Fatalf("a flat image should stay flat, got %#x", r>>8)
	}
}
