package accordion_test

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gpu/headless"
	gioinput "gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/accordion"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW = 240
	canvasH = 240
)

var canvasSize = image.Pt(canvasW, canvasH)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

// bodyRect returns a layout.Widget that fills its constraints with a
// fixed colour. Distinct per-section colours give each open body a
// visibly different fill in goldens regardless of theme.
func bodyRect(c color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
}

func scene(w layout.Widget, bg color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// threeSections returns a deterministic three-section fixture. Titles
// are empty so the goldens do not depend on GPU font rasterisation;
// the per-section body colours are unrelated to the theme so the same
// fixture is reused for the light and dark goldens.
func threeSections() []accordion.Section {
	return []accordion.Section{
		{Title: "", Body: bodyRect(color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xff})},
		{Title: "", Body: bodyRect(color.NRGBA{R: 0x40, G: 0xc0, B: 0x60, A: 0xff})},
		{Title: "", Body: bodyRect(color.NRGBA{R: 0x40, G: 0x70, B: 0xff, A: 0xff})},
	}
}

// TestAccordionGolden records or diffs the three Measurable goldens.
func TestAccordionGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	cases := []struct {
		name   string
		open   map[int]bool
		colors tokens.ColorTokens
		bg     color.NRGBA
	}{
		{"light-three-sections-first-open", map[int]bool{0: true}, tokens.DefaultLight, lightBG},
		{"dark-three-sections-all-closed", map[int]bool{}, tokens.DefaultDark, darkBG},
		// SingleOpen is a behavioural property exercised by the
		// interaction test; the visual golden simply pins the
		// "one section open in the middle" appearance the mode produces.
		{"light-single-open-mode", map[int]bool{1: true}, tokens.DefaultLight, lightBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := accordion.Props{Sections: threeSections(), Shaper: shaper}
			w := accordion.Render(shaper, props, tc.open, tc.colors, tokens.Spacing, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestAccordionChevronRotatesBetweenStates guards the visual contract
// that the chevron differs between open and closed states. A
// closed-vs-open render of the same section must produce a non-zero
// pixel diff in the header strip.
func TestAccordionChevronRotatesBetweenStates(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	render := func(open map[int]bool) *image.RGBA {
		// Use a Body of identical Surface colour so any pixel diff
		// between open and closed renders must originate in the
		// chevron, not in the body area.
		sections := []accordion.Section{{Title: "", Body: bodyRect(tokens.DefaultLight.Surface)}}
		props := accordion.Props{Sections: sections, Shaper: shaper}
		// Crop to the header strip so the body area never participates
		// in the diff regardless of how the renderer pads.
		w := accordion.Render(shaper, props, open, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypeScale)
		return capture(t, image.Pt(48, 48), scene(w, bg))
	}

	closed := render(map[int]bool{})
	open := render(map[int]bool{0: true})
	if closed == nil || open == nil {
		return
	}
	if n := pixelDiff(closed, open); n == 0 {
		t.Errorf("open and closed headers render identically; expected chevron pixels to rotate")
	}
}

// ---- Interaction tests ----

func liveWidget(t *testing.T, obs rx.Observable[layout.Widget]) layout.Widget {
	t.Helper()
	var w layout.Widget
	if err := obs.Subscribe(func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}, rx.NewScheduler()).Wait(); err != nil {
		t.Fatalf("Accordion subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Accordion did not emit an initial widget")
	}
	return w
}

func driveFrame(w layout.Widget, ops *op.Ops, r *gioinput.Router, size image.Point) layout.Dimensions {
	ops.Reset()
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(size),
		Ops:         ops,
		Source:      r.Source(),
	}
	dims := w(gtx)
	r.Frame(ops)
	return dims
}

// TestAccordionArrowSpaceEnter drives Arrow-Up/Down focus traversal
// and proves Enter and Space both activate the focused header. With
// PxPerDp=1, an accordion with three sections renders each header as
// 240×48 px. Headers stack from y=0 with no open bodies, so header i
// occupies y∈[48i, 48(i+1)]. A pointer click at (96, 24) lands on
// header 0 and gives it focus — the seed for subsequent arrow traversal.
func TestAccordionArrowSpaceEnter(t *testing.T) {
	var calls []int
	props := accordion.Props{
		Sections: []accordion.Section{
			{Title: ""},
			{Title: ""},
			{Title: ""},
		},
		Open:     rx.Of(map[int]bool{}),
		OnToggle: func(_ layout.Context, idx int) { calls = append(calls, idx) },
		Shaper:   defaultShaper(t),
	}
	w := liveWidget(t, accordion.Accordion(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	// Two warm-up frames so the router has stable hit-test data for
	// each header's clip area before pointer events are queued.
	driveFrame(w, ops, r, canvasSize)
	driveFrame(w, ops, r, canvasSize)

	// Click header 0 → OnToggle(0) and focus moves to header 0.
	hit := f32.Pt(96, 24)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: hit, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: hit, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, canvasSize)

	// Arrow-Down to header 1, then Enter (Press+Release) → OnToggle(1).
	r.Queue(key.Event{Name: key.NameDownArrow, State: key.Press})
	driveFrame(w, ops, r, canvasSize)
	r.Queue(
		key.Event{Name: key.NameReturn, State: key.Press},
		key.Event{Name: key.NameReturn, State: key.Release},
	)
	driveFrame(w, ops, r, canvasSize)

	// Arrow-Down to header 2, then Space (Press+Release) → OnToggle(2).
	r.Queue(key.Event{Name: key.NameDownArrow, State: key.Press})
	driveFrame(w, ops, r, canvasSize)
	r.Queue(
		key.Event{Name: key.NameSpace, State: key.Press},
		key.Event{Name: key.NameSpace, State: key.Release},
	)
	driveFrame(w, ops, r, canvasSize)

	// Two Arrow-Ups → focus moves back to header 0; Enter → OnToggle(0).
	r.Queue(key.Event{Name: key.NameUpArrow, State: key.Press})
	driveFrame(w, ops, r, canvasSize)
	r.Queue(key.Event{Name: key.NameUpArrow, State: key.Press})
	driveFrame(w, ops, r, canvasSize)
	r.Queue(
		key.Event{Name: key.NameReturn, State: key.Press},
		key.Event{Name: key.NameReturn, State: key.Release},
	)
	driveFrame(w, ops, r, canvasSize)

	want := []int{0, 1, 2, 0}
	if !equalInts(calls, want) {
		t.Fatalf("OnToggle call sequence:\n got  %v\n want %v", calls, want)
	}
}

// TestAccordionSingleOpenCollapsesPrior verifies the SingleOpen
// invariant: when activating a closed section while another section
// is open in the captured snapshot, the accordion first dispatches
// OnToggle for the open peer, then OnToggle for the activated index.
//
// Initial Open={0:true}. With section 0 open the layout is:
//
//	header 0  y∈[0, 48]
//	body   0  y∈[48, 144]
//	header 1  y∈[144, 192]
//	header 2  y∈[192, 240]
//
// A pointer click at (96, 168) lands on header 1 — closed in the
// snapshot — so the SingleOpen path closes header 0 first (OnToggle(0))
// then opens header 1 (OnToggle(1)).
func TestAccordionSingleOpenCollapsesPrior(t *testing.T) {
	var calls []int
	props := accordion.Props{
		Sections: []accordion.Section{
			{Title: "", Body: bodyRect(color.NRGBA{A: 0xff})},
			{Title: "", Body: bodyRect(color.NRGBA{A: 0xff})},
			{Title: "", Body: bodyRect(color.NRGBA{A: 0xff})},
		},
		Open:       rx.Of(map[int]bool{0: true}),
		OnToggle:   func(_ layout.Context, idx int) { calls = append(calls, idx) },
		SingleOpen: true,
		Shaper:     defaultShaper(t),
	}
	w := liveWidget(t, accordion.Accordion(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	driveFrame(w, ops, r, canvasSize)
	driveFrame(w, ops, r, canvasSize)

	hit := f32.Pt(96, 168)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: hit, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: hit, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, canvasSize)

	want := []int{0, 1}
	if !equalInts(calls, want) {
		t.Fatalf("SingleOpen call sequence:\n got  %v\n want %v", calls, want)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- golden harness (inlined; prism/internal/golden is not importable
// from outside the prism module tree) ----

func capture(t *testing.T, size image.Point, draw layout.Widget) *image.RGBA {
	t.Helper()
	w, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skipf("headless rendering not supported: %v", err)
		return nil
	}
	defer w.Release()

	var ops op.Ops
	gtx := layout.Context{
		Constraints: layout.Exact(size),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Ops:         &ops,
	}
	draw(gtx)
	if err := w.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := w.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
}

func renderGolden(t *testing.T, name string, size image.Point, draw layout.Widget) {
	t.Helper()
	img := capture(t, size, draw)
	if img == nil {
		return
	}
	path := filepath.Join("testdata", "golden", name+".png")

	if *goldenUpdate {
		if err := saveImage(path, img); err != nil {
			t.Fatalf("save %s: %v", path, err)
		}
		return
	}

	stored, err := loadImage(path)
	if os.IsNotExist(err) {
		t.Fatalf("%s not found; run go test -golden.update to create", path)
		return
	}
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
		return
	}
	if n := pixelDiff(stored, img); n > 0 {
		actualPath := strings.TrimSuffix(path, ".png") + ".actual.png"
		_ = saveImage(actualPath, img)
		t.Fatalf("%q: %d pixel(s) differ (actual saved to %s)", name, n, actualPath)
	}
}

func pixelDiff(a, b *image.RGBA) int {
	if a.Bounds() != b.Bounds() {
		return -1
	}
	bounds := a.Bounds()
	n := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			off := (y-bounds.Min.Y)*a.Stride + (x-bounds.Min.X)*4
			if a.Pix[off] != b.Pix[off] ||
				a.Pix[off+1] != b.Pix[off+1] ||
				a.Pix[off+2] != b.Pix[off+2] ||
				a.Pix[off+3] != b.Pix[off+3] {
				n++
			}
		}
	}
	return n
}

func saveImage(path string, img *image.RGBA) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	nrgba := &image.NRGBA{Pix: img.Pix, Stride: img.Stride, Rect: img.Rect}
	return png.Encode(f, nrgba)
}

func loadImage(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	decoded, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	switch v := decoded.(type) {
	case *image.RGBA:
		return v, nil
	case *image.NRGBA:
		return &image.RGBA{Pix: v.Pix, Stride: v.Stride, Rect: v.Rect}, nil
	default:
		bounds := decoded.Bounds()
		rgba := image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				rgba.Set(x, y, decoded.At(x, y))
			}
		}
		return rgba, nil
	}
}
