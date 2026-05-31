package navbar_test

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
	"gioui.org/widget"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/navbar"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 480, 64
)

var canvasSize = image.Pt(canvasW, canvasH)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

// scene renders w into a canvas-sized constraint over a flat background.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// TestNavbarGolden records or diffs the three Measurable goldens.
// Labels are empty to avoid GPU font rasterisation differences across
// platforms; each link cell is given (S3, S2) padding so the Active
// link's Primary underline is a visible rectangle even with a zero-
// width label. light-active-second-link therefore differs from
// light-default by ~48 Blue pixels in the second link's cell.
func TestNavbarGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	defaultLinks := []navbar.Link{{Label: ""}, {Label: ""}}
	activeSecond := []navbar.Link{{Label: ""}, {Label: "", Active: true}}

	cases := []struct {
		name   string
		links  []navbar.Link
		colors tokens.ColorTokens
		bg     color.NRGBA
	}{
		{"light-default", defaultLinks, tokens.DefaultLight, lightBG},
		{"dark-default", defaultLinks, tokens.DefaultDark, darkBG},
		{"light-active-second-link", activeSecond, tokens.DefaultLight, lightBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := navbar.Props{Links: tc.links, Shaper: shaper}
			w := navbar.Render(shaper, props, tc.colors, tokens.Spacing, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestNavbarActiveVsDefaultDiffer guards the visual contract that an
// Active link adds Primary-coloured pixels in the link row.
func TestNavbarActiveVsDefaultDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	render := func(links []navbar.Link) *image.RGBA {
		props := navbar.Props{Links: links, Shaper: shaper}
		w := navbar.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypeScale)
		return capture(t, canvasSize, scene(w, bg))
	}

	def := render([]navbar.Link{{Label: ""}, {Label: ""}})
	act := render([]navbar.Link{{Label: ""}, {Label: "", Active: true}})
	if def == nil || act == nil {
		return
	}
	if n := pixelDiff(def, act); n == 0 {
		t.Errorf("active and default render identically; expected Primary underline pixels")
	}
}

// ---- Interaction tests ----

// fillRect is a sharp-edged solid widget with a fixed size.
func fillRect(c color.NRGBA, w, h int) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(w, h)
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// liveWidget subscribes to nb, drains the trampoline scheduler, and
// returns the latest emitted layout.Widget. State referenced by the
// widget closure remains valid for the test's lifetime because it is
// captured by the rx.Defer scope.
func liveWidget(t *testing.T, nb rx.Observable[layout.Widget]) layout.Widget {
	t.Helper()
	var w layout.Widget
	if err := nb.Subscribe(func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}, rx.NewScheduler()).Wait(); err != nil {
		t.Fatalf("Navbar subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Navbar did not emit an initial widget")
	}
	return w
}

// driveFrame lays out w against ops + router and returns the dims.
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

// TestNavbarTabTraversal verifies Tab cycles focus through
// brand → links → actions in document order, and Shift+Tab reverses.
// Brand and action contribute focus stops via outer-test Clickables;
// the two link stops are owned by the navbar.
func TestNavbarTabTraversal(t *testing.T) {
	var brandClick, actionClick widget.Clickable
	brand := func(gtx layout.Context) layout.Dimensions {
		return brandClick.Layout(gtx, fillRect(color.NRGBA{R: 80, G: 80, B: 200, A: 255}, 40, 20))
	}
	action := func(gtx layout.Context) layout.Dimensions {
		return actionClick.Layout(gtx, fillRect(color.NRGBA{R: 200, G: 80, B: 80, A: 255}, 40, 20))
	}

	props := navbar.Props{
		Brand: brand,
		Links: []navbar.Link{
			{Label: "", OnClick: func(_ layout.Context) {}},
			{Label: "", OnClick: func(_ layout.Context) {}},
		},
		Actions: []layout.Widget{action},
		Shaper:  defaultShaper(t),
	}
	w := liveWidget(t, navbar.Navbar(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)

	// Frame 0: register tags.
	driveFrame(w, ops, r, canvasSize)

	// Drain any synthetic focus events for the externally-owned tags so
	// the router retains focus when set, matching the FocusGroup idiom.
	drainFocus := func() {
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(canvasSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		for _, tag := range []any{&brandClick, &actionClick} {
			for {
				if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
					break
				}
			}
		}
	}
	drainFocus()

	// Focus the brand explicitly.
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(canvasSize),
		Ops:         ops,
		Source:      r.Source(),
	}
	gtx.Execute(key.FocusCmd{Tag: &brandClick})
	driveFrame(w, ops, r, canvasSize)

	check := func(stage string, wantBrand, wantAction bool) {
		t.Helper()
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(canvasSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		gotBrand := gtx.Focused(&brandClick)
		gotAction := gtx.Focused(&actionClick)
		if gotBrand != wantBrand || gotAction != wantAction {
			t.Errorf("%s: focused(brand)=%v action=%v; want brand=%v action=%v",
				stage, gotBrand, gotAction, wantBrand, wantAction)
		}
	}

	check("after Focus(brand)", true, false)

	// Tab → expected stop is link 0 (neither brand nor action).
	r.MoveFocus(key.FocusForward)
	driveFrame(w, ops, r, canvasSize)
	check("Tab #1 (→ link 0)", false, false)

	// Tab → expected stop is link 1.
	r.MoveFocus(key.FocusForward)
	driveFrame(w, ops, r, canvasSize)
	check("Tab #2 (→ link 1)", false, false)

	// Tab → expected stop is action.
	r.MoveFocus(key.FocusForward)
	driveFrame(w, ops, r, canvasSize)
	check("Tab #3 (→ action)", false, true)

	// Now reverse the traversal. Shift+Tab from action: back to link 1.
	r.MoveFocus(key.FocusBackward)
	driveFrame(w, ops, r, canvasSize)
	check("Shift+Tab #1 (→ link 1)", false, false)

	// Shift+Tab → link 0.
	r.MoveFocus(key.FocusBackward)
	driveFrame(w, ops, r, canvasSize)
	check("Shift+Tab #2 (→ link 0)", false, false)

	// Shift+Tab → brand.
	r.MoveFocus(key.FocusBackward)
	driveFrame(w, ops, r, canvasSize)
	check("Shift+Tab #3 (→ brand)", true, false)
}

// TestNavbarLinkClickFiresOnClick verifies clicking a link invokes its
// OnClick callback. With PxPerDp=1, canvas 480×64, no brand, no
// actions, two empty-label links: each link cell is 24×18, the link
// row is 56 wide and centred at canvas-mid. Link 0 occupies x in
// [212, 236] and y in [23, 41]; a press/release at (224, 32) lands
// squarely inside.
func TestNavbarLinkClickFiresOnClick(t *testing.T) {
	var fired0, fired1 int
	props := navbar.Props{
		Links: []navbar.Link{
			{Label: "", OnClick: func(_ layout.Context) { fired0++ }},
			{Label: "", OnClick: func(_ layout.Context) { fired1++ }},
		},
		Shaper: defaultShaper(t),
	}
	w := liveWidget(t, navbar.Navbar(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)

	// Two warm-up frames so the router has stable hit-test data for the
	// link clip areas before pointer events are queued.
	driveFrame(w, ops, r, canvasSize)
	driveFrame(w, ops, r, canvasSize)

	hit := f32.Pt(224, 32)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: hit, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: hit, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, canvasSize)

	if fired0 != 1 {
		t.Errorf("link 0 OnClick call count = %d, want 1", fired0)
	}
	if fired1 != 0 {
		t.Errorf("link 1 OnClick spuriously fired %d time(s)", fired1)
	}
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
