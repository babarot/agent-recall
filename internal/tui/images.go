package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // decoders for the formats Claude Code stores
	_ "image/jpeg"
	"image/png"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/babarot/claude-recall/internal/db"
)

// Images: with tui.images, the spread Conversation shows the images pasted
// into the user's messages in place of their [Image #N] markers, the k-th
// marker of a message being its k-th stored image, as the web UI has it.
//
// Bubble Tea draws cells and redraws only the ones that change, so an image
// cannot be painted at a position. It is sent to the terminal once instead,
// with the Kitty graphics protocol, and the view holds Unicode placeholders:
// ordinary cells whose color names the image and whose diacritics name the
// row and column of it they show. They scroll, clip and redraw like text.
//
// An image is read, decoded, shrunk and turned into PNG in the background
// the first time a build asks for it; the marker shows as text until then,
// and for an image that cannot be read.

const (
	// maxImageRows is the most rows an image takes.
	maxImageRows = 20
	// maxImagePixels is the widest an image is sent; wider ones are shrunk,
	// which keeps a 2000-pixel screenshot to about 200 KB.
	maxImagePixels = 1000
	// pixelsPerCell is the narrowest a column of an image is drawn, so a
	// small image is not blown up past what it holds.
	pixelsPerCell = 8
	// maxImageIDs is how many images the terminal holds at once: the ID is
	// a 256-color foreground, which no color profile turns into another.
	maxImageIDs = 255
)

var imageMarker = regexp.MustCompile(`\[Image #\d+\]`)

// imageSource reads stored images; *db.DB is one.
type imageSource interface {
	GetImage(sessionID, messageUUID string, index int) (mediaType string, data []byte, ok bool, err error)
}

type imageKey struct {
	session, uuid string
	index         int
}

// termImage is an image the terminal holds under id, w by h pixels, placed
// over cols by rows cells (0 before a placement).
type termImage struct {
	id, w, h   int
	cols, rows int
}

// termImages is what the terminal holds, shared by the Model's copies as
// its maps are, since a build that finds a missing image is a value method.
type termImages struct {
	held    map[imageKey]*termImage
	failed  map[imageKey]bool
	loading map[imageKey]bool
	want    []imageKey       // asked for by a build, to be read
	byID    map[int]imageKey // which image has an ID
	nextID  int
	out     strings.Builder // commands for the terminal, sent after Update
}

func newTermImages() *termImages {
	return &termImages{held: map[imageKey]*termImage{}, failed: map[imageKey]bool{},
		loading: map[imageKey]bool{}, byID: map[int]imageKey{}}
}

// imageLoaded brings an image read in the background, as PNG.
type imageLoaded struct {
	key  imageKey
	png  []byte
	w, h int
	err  error
}

// imageLoadCmd reads the images the last build asked for.
func (m *Model) imageLoadCmd() tea.Cmd {
	src, ok := m.source.(imageSource)
	if !ok || m.images == nil || len(m.images.want) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, k := range m.images.want {
		if m.images.loading[k] {
			continue
		}
		m.images.loading[k] = true
		cmds = append(cmds, func() tea.Msg { return loadImage(src, k) })
	}
	m.images.want = nil
	return tea.Batch(cmds...)
}

func loadImage(src imageSource, k imageKey) tea.Msg {
	_, data, ok, err := src.GetImage(k.session, k.uuid, k.index)
	if err == nil && !ok {
		err = fmt.Errorf("no image %d of %s", k.index, k.uuid)
	}
	if err != nil {
		return imageLoaded{key: k, err: err}
	}
	m, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return imageLoaded{key: k, err: err}
	}
	if m.Bounds().Dx() > maxImagePixels {
		m, format = shrink(m, maxImagePixels), ""
	}
	if format != "png" { // Kitty takes PNG, not JPEG
		var b bytes.Buffer
		if err := png.Encode(&b, m); err != nil {
			return imageLoaded{key: k, err: err}
		}
		data = b.Bytes()
	}
	return imageLoaded{key: k, png: data, w: m.Bounds().Dx(), h: m.Bounds().Dy()}
}

// shrink scales m down to w pixels wide, each pixel the average of the box
// of pixels it covers.
func shrink(m image.Image, w int) image.Image {
	b := m.Bounds()
	src := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), m, b.Min, draw.Src)
	sw, sh := b.Dx(), b.Dy()
	h := max(1, sh*w/sw)
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := y*sh/h, max(y*sh/h+1, (y+1)*sh/h)
		for x := range w {
			x0, x1 := x*sw/w, max(x*sw/w+1, (x+1)*sw/w)
			var sum [4]int
			for yy := y0; yy < y1; yy++ {
				row := src.Pix[yy*src.Stride:]
				for xx := x0; xx < x1; xx++ {
					p := row[xx*4 : xx*4+4]
					sum[0], sum[1], sum[2], sum[3] = sum[0]+int(p[0]), sum[1]+int(p[1]), sum[2]+int(p[2]), sum[3]+int(p[3])
				}
			}
			n := (y1 - y0) * (x1 - x0)
			i := out.PixOffset(x, y)
			for c := range 4 {
				out.Pix[i+c] = uint8(sum[c] / n)
			}
		}
	}
	return out
}

// storeImage sends a loaded image to the terminal under the next free ID,
// dropping the image that had it.
func (m *Model) storeImage(msg imageLoaded) {
	t := m.images
	delete(t.loading, msg.key)
	if msg.err != nil {
		t.failed[msg.key] = true
	} else {
		t.nextID = t.nextID%maxImageIDs + 1
		id := t.nextID
		if old, ok := t.byID[id]; ok {
			delete(t.held, old)
			t.out.WriteString(ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", id), "q=2"))
		}
		t.byID[id] = msg.key
		t.held[msg.key] = &termImage{id: id, w: msg.w, h: msg.h}
		transmit(&t.out, id, msg.png)
	}
	if r := m.current(); r != nil && r.s.ID == msg.key.session {
		m.readFor = "" // the spread conversation, with it
	}
}

// transmit writes the commands that send png under id, in chunks of 4096
// base64 bytes as the protocol asks. q=2 keeps the terminal from replying.
func transmit(b *strings.Builder, id int, png []byte) {
	data := base64.StdEncoding.EncodeToString(png)
	first := true
	for first || len(data) > 0 {
		n := min(4096, len(data))
		chunk := data[:n]
		data = data[n:]
		more := "m=0"
		if len(data) > 0 {
			more = "m=1"
		}
		if first {
			b.WriteString(ansi.KittyGraphics([]byte(chunk), "a=t", "f=100", "q=2", fmt.Sprintf("i=%d", id), more))
		} else {
			b.WriteString(ansi.KittyGraphics([]byte(chunk), more))
		}
		first = false
	}
}

// imageOut takes the commands for the terminal gathered since the last
// Update.
func (m *Model) imageOut() tea.Cmd {
	if m.images == nil || m.images.out.Len() == 0 {
		return nil
	}
	s := m.images.out.String()
	m.images.out.Reset()
	return tea.Raw(s)
}

// imagesQuit deletes the images sent, then quits.
func (m Model) imagesQuit() tea.Cmd {
	if m.images == nil || len(m.images.byID) == 0 {
		return tea.Quit
	}
	var b strings.Builder
	for id := range m.images.byID {
		b.WriteString(ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", id), "q=2"))
	}
	return tea.Sequence(tea.Raw(b.String()), tea.Quit)
}

// image returns the rows of placeholders that show image k at most w
// cells wide, placing it at that size when it is not yet, or false while
// it is not held: asked for, unless it failed.
func (m Model) image(k imageKey, w int) ([]string, bool) {
	t := m.images
	img, ok := t.held[k]
	if !ok {
		if !t.failed[k] && !t.loading[k] {
			t.want = append(t.want, k)
		}
		return nil, false
	}
	cols, rows := imageCells(img.w, img.h, w)
	if cols != img.cols || rows != img.rows {
		// p=1 replaces the image's one placement rather than adding one.
		t.out.WriteString(ansi.KittyGraphics(nil, "a=p", "U=1", fmt.Sprintf("i=%d", img.id), "p=1",
			fmt.Sprintf("c=%d", cols), fmt.Sprintf("r=%d", rows), "q=2"))
		img.cols, img.rows = cols, rows
	}
	return placeholders(img.id, cols, rows), true
}

// imageCells sizes a w by h pixel image in cells, at most maxCols wide and
// maxImageRows tall, taking a cell to be twice as tall as it is wide.
func imageCells(w, h, maxCols int) (cols, rows int) {
	cols = max(1, min(maxCols, (w+pixelsPerCell-1)/pixelsPerCell))
	rows = max(1, (cols*h+w)/(2*w))
	if rows > maxImageRows {
		rows = maxImageRows
		cols = max(1, min(cols, (rows*2*w+h/2)/h))
	}
	return cols, rows
}

// placeholders are the lines of cells that show image id over cols by rows
// cells. Every cell names its row and column, so a cell redrawn alone is
// still the right part of the image.
func placeholders(id, cols, rows int) []string {
	out := make([]string, rows)
	for r := range rows {
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[38;5;%dm", id)
		for c := range cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		b.WriteString("\x1b[39m")
		out[r] = b.String()
	}
	return out
}

// bodyLine is a line of a message's body: text, or a row of an image.
type bodyLine struct {
	s     string
	image bool
}

// messageBody wraps a message's text to w cells, with its images in place
// of their markers when tui.images is on and they are held.
func (m Model) messageBody(msg db.Message, w int) []bodyLine {
	var out []bodyLine
	text := func(s string) {
		for _, l := range wrapText(strings.TrimSpace(s), w) {
			out = append(out, bodyLine{s: l})
		}
	}
	r := m.current()
	if !m.cfg.Images || m.images == nil || r == nil || msg.UUID == "" || msg.Role != "user" {
		text(msg.Content)
		return out
	}
	last := 0
	for i, at := range imageMarker.FindAllStringIndex(msg.Content, -1) {
		lines, ok := m.image(imageKey{r.s.ID, msg.UUID, i}, w)
		if !ok {
			continue // the marker stays in the text
		}
		if s := msg.Content[last:at[0]]; strings.TrimSpace(s) != "" {
			text(s)
		}
		for _, l := range lines {
			out = append(out, bodyLine{s: l, image: true})
		}
		last = at[1]
	}
	if s := msg.Content[last:]; strings.TrimSpace(s) != "" || len(out) == 0 {
		text(s)
	}
	return out
}
