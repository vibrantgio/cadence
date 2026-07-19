package tabs_test

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
	"github.com/vibrantgio/cadence/tabs"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 240, 128
)

var canvasSize = image.Pt(canvasW, canvasH)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

// contentRect returns a layout.Widget that fills its constraints with a
// fixed colour. A per-tab distinct colour is used so swapping the
// selected index produces a visible diff in the content panel of each
// golden.
func contentRect(c color.NRGBA) layout.Widget {
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

// threeTabs returns a deterministic three-tab fixture. Labels are empty
// to avoid GPU font rasterisation differences across platforms; the
// per-tab content colours are unrelated to the theme so the same
// fixture is reused for the light and dark goldens.
func threeTabs() []tabs.Tab {
	return []tabs.Tab{
		{Label: "", Content: contentRect(color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xff})},
		{Label: "", Content: contentRect(color.NRGBA{R: 0x40, G: 0xc0, B: 0x60, A: 0xff})},
		{Label: "", Content: contentRect(color.NRGBA{R: 0x40, G: 0x70, B: 0xff, A: 0xff})},
	}
}

func singleTab() []tabs.Tab {
	return []tabs.Tab{
		{Label: "", Content: contentRect(color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xff})},
	}
}

// TestTabsGolden records or diffs the three Measurable goldens.
func TestTabsGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	cases := []struct {
		name     string
		tabs     []tabs.Tab
		selected int
		colors   tokens.ColorTokens
		bg       color.NRGBA
	}{
		{"light-three-tabs-first-selected", threeTabs(), 0, tokens.DefaultLight, lightBG},
		{"dark-three-tabs-second-selected", threeTabs(), 1, tokens.DefaultDark, darkBG},
		{"light-single-tab", singleTab(), 0, tokens.DefaultLight, lightBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := tabs.Props{Tabs: tc.tabs, Shaper: shaper}
			w := tabs.Render(shaper, props, tc.selected, tc.colors, tokens.Spacing, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestTabsSelectionUnderlineIsVisible guards the visual contract that
// the selected tab adds Primary-coloured pixels to the strip relative
// to an unselected (out-of-range index) baseline.
func TestTabsSelectionUnderlineIsVisible(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	render := func(selected int) *image.RGBA {
		props := tabs.Props{Tabs: threeTabs(), Shaper: shaper}
		w := tabs.Render(shaper, props, selected, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypeScale)
		return capture(t, canvasSize, scene(w, bg))
	}

	none := render(-1)
	first := render(0)
	if none == nil || first == nil {
		return
	}
	if n := pixelDiff(none, first); n == 0 {
		t.Errorf("selected and unselected render identically; expected Primary underline pixels")
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
		t.Fatalf("Tabs subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Tabs did not emit an initial widget")
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

// TestTabsArrowAndHomeEndWrapAndFocus drives the WAI-ARIA tab pattern
// end-to-end. Three empty-label tabs render with cellW = 2×S3 = 24 px
// and stripH = 40 px (PxPerDp = 1), so tab 0 occupies x∈[0,24] y∈[0,40]
// — a pointer click at (12, 20) lands squarely inside tab 0 and gives
// it focus.
//
// Focus-follows-selection is verified using the "Enter trick": each
// arrow / Home / End press is followed by a Press+Release of NameReturn
// on the now-focused tab's widget.Clickable. The OnSelect callback fires
// twice — once for the navigation key (target index) and again for
// Enter (same target). If focus had not moved with selection, Enter
// would re-fire the previous tab's index instead, and the sequence
// would diverge from the expected list below.
func TestTabsArrowAndHomeEndWrapAndFocus(t *testing.T) {
	var calls []int
	props := tabs.Props{
		Tabs:     threeTabs(),
		Selected: rx.Of(0),
		OnSelect: func(_ layout.Context, idx int) { calls = append(calls, idx) },
		Shaper:   defaultShaper(t),
	}
	w := liveWidget(t, tabs.Tabs(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	// Two warm-up frames so the router has stable hit-test data for the
	// tab cell clip areas before pointer events are queued.
	driveFrame(w, ops, r, canvasSize)
	driveFrame(w, ops, r, canvasSize)

	// Click tab 0 → OnSelect(0) and focus moves to tab 0.
	hit := f32.Pt(12, 20)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: hit, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: hit, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, canvasSize)

	// pressKey sends a navigation key, drives a frame so the FocusCmd is
	// applied, then sends Enter Press+Release on the now-focused tab and
	// drives another frame so widget.Clickable.Clicked observes the
	// matched key pair. Two OnSelect calls per step: navigation + Enter.
	pressKey := func(name key.Name) {
		r.Queue(key.Event{Name: name, State: key.Press})
		driveFrame(w, ops, r, canvasSize)
		r.Queue(
			key.Event{Name: key.NameReturn, State: key.Press},
			key.Event{Name: key.NameReturn, State: key.Release},
		)
		driveFrame(w, ops, r, canvasSize)
	}

	pressKey(key.NameRightArrow) // → tab 1, Enter on tab 1
	pressKey(key.NameRightArrow) // → tab 2, Enter on tab 2
	pressKey(key.NameRightArrow) // wrap → tab 0, Enter on tab 0
	pressKey(key.NameLeftArrow)  // wrap → tab 2, Enter on tab 2
	pressKey(key.NameHome)       // → tab 0, Enter on tab 0
	pressKey(key.NameEnd)        // → tab 2, Enter on tab 2

	want := []int{
		0,    // initial click
		1, 1, // Right + Enter
		2, 2, // Right + Enter
		0, 0, // Right (wrap) + Enter
		2, 2, // Left (wrap) + Enter
		0, 0, // Home + Enter
		2, 2, // End + Enter
	}
	if !equalInts(calls, want) {
		t.Fatalf("OnSelect call sequence:\n got  %v\n want %v", calls, want)
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
