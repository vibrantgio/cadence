package popover_test

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
	"gioui.org/gpu/headless"
	gioinput "gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/popover"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 320, 240
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius. Anti-aliased rounded corners vary slightly
	// between GPU contexts, breaking determinism.
	sharpRadius = tokens.RadiusScale{}
)

// fixedRect is a sharp-edged solid widget with explicit width and height.
// Used for both Anchor and Content stand-ins so their hit rects are
// predictable and the goldens stay deterministic.
func fixedRect(c color.NRGBA, widthDp, heightDp float32) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(gtx.Dp(unit.Dp(widthDp)), gtx.Dp(unit.Dp(heightDp)))
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// scene renders w over a flat background sized to the constraints.
func scene(w layout.Widget, bg color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// ---- Golden tests ----

// TestPopoverGolden records or diffs the four Measurable goldens — one
// per Placement, alternating light and dark theme. The anchor is a small
// solid rectangle and the content is a larger solid rectangle; the tail
// triangle is the only diagonal-edged shape in each frame.
func TestPopoverGolden(t *testing.T) {
	anchor := fixedRect(color.NRGBA{R: 80, G: 160, B: 220, A: 255}, 60, 28)
	content := fixedRect(color.NRGBA{R: 120, G: 120, B: 120, A: 255}, 80, 36)

	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	cases := []struct {
		name      string
		placement popover.Placement
		colors    tokens.ColorTokens
		bg        color.NRGBA
	}{
		{"top-light", popover.Top, tokens.DefaultLight, lightBG},
		{"bottom-light", popover.Bottom, tokens.DefaultLight, lightBG},
		{"left-dark", popover.Left, tokens.DefaultDark, darkBG},
		{"right-dark", popover.Right, tokens.DefaultDark, darkBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := popover.Props{
				Anchor:    anchor,
				Content:   content,
				Placement: tc.placement,
			}
			w := popover.Render(props, true, tc.colors, tokens.Spacing, sharpRadius)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestPopoverOpenAndClosedDiffer confirms that flipping the open flag
// changes the rendered output. Catches regressions where the open branch
// silently no-ops.
func TestPopoverOpenAndClosedDiffer(t *testing.T) {
	anchor := fixedRect(color.NRGBA{R: 80, G: 160, B: 220, A: 255}, 60, 28)
	content := fixedRect(color.NRGBA{R: 120, G: 120, B: 120, A: 255}, 80, 36)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	props := popover.Props{Anchor: anchor, Content: content, Placement: popover.Top}

	open := popover.Render(props, true, tokens.DefaultLight, tokens.Spacing, sharpRadius)
	closed := popover.Render(props, false, tokens.DefaultLight, tokens.Spacing, sharpRadius)

	imgOpen := capture(t, canvasSize, scene(open, bg))
	imgClosed := capture(t, canvasSize, scene(closed, bg))
	if imgOpen == nil || imgClosed == nil {
		return
	}
	if n := pixelDiff(imgOpen, imgClosed); n == 0 {
		t.Error("open and closed popover render identically; expected the surface + tail to appear when open")
	}
}

// ---- Interaction tests ----

// livePopover subscribes to the Popover observable, drains the trampoline
// scheduler with Wait(), and returns the latest emitted layout.Widget.
// State referenced by the widget closure remains valid for the test's
// lifetime because it is captured by the rx.Defer scope.
func livePopover(t *testing.T, props popover.Props) layout.Widget {
	t.Helper()
	obs := popover.Popover(rx.Of(theme.Default()), props)
	var w layout.Widget
	if err := obs.Subscribe(func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}, rx.NewScheduler()).Wait(); err != nil {
		t.Fatalf("Popover subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Popover did not emit an initial widget")
	}
	return w
}

// driveFrame lays out w against ops + router, returns the rendered dims.
// ops is reset before layout; events queued on the router before the call
// are delivered during w's layout pass and r.Frame.
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

// TestOutsideClickInvokesOnDismiss verifies the Measurable interaction —
// a pointer.Press outside the popover's anchor and surface bounds invokes
// OnDismiss. A press inside the surface (canvas centre, where the anchor
// lives, then near the anchor) must NOT invoke OnDismiss.
func TestOutsideClickInvokesOnDismiss(t *testing.T) {
	var dismissed int
	anchor := fixedRect(color.NRGBA{R: 80, G: 160, B: 220, A: 255}, 60, 28)
	content := fixedRect(color.NRGBA{R: 120, G: 120, B: 120, A: 255}, 80, 36)

	w := livePopover(t, popover.Props{
		Open:      rx.Of(true),
		Anchor:    anchor,
		Content:   content,
		Placement: popover.Top,
		OnDismiss: func(_ layout.Context) { dismissed++ },
	})

	r := new(gioinput.Router)
	ops := new(op.Ops)
	driveFrame(w, ops, r, canvasSize)
	driveFrame(w, ops, r, canvasSize)

	// (1) Press at the canvas corner — guaranteed outside both the anchor
	// (centred ~30 dp around canvas centre) and the surface (above it for
	// Placement=Top). OnDismiss must fire.
	corner := f32.Pt(4, 4)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: corner, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Release, Position: corner, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
	)
	driveFrame(w, ops, r, canvasSize)
	if dismissed == 0 {
		t.Fatalf("outside click did not invoke OnDismiss; dismissed = %d", dismissed)
	}
	outsideHits := dismissed

	// (2) Press at the canvas centre — guaranteed inside the anchor — must
	// not bleed through to the outside-absorber and dismiss.
	centre := f32.Pt(canvasW/2, canvasH/2)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: centre, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Release, Position: centre, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
	)
	driveFrame(w, ops, r, canvasSize)
	if dismissed != outsideHits {
		t.Errorf("anchor click bled through to outside-absorber; OnDismiss went from %d to %d", outsideHits, dismissed)
	}
}

// TestArbitrationDismissesPriorPopover verifies the Specific contract that
// opening a second popover dismisses the first via the prism/coordination
// arbitration channel. We subscribe two live popovers (B after A), then
// drive A's widget for a frame: A observes that arbitration top is no
// longer A and invokes its OnDismiss.
func TestArbitrationDismissesPriorPopover(t *testing.T) {
	var aDismissed int
	anchor := fixedRect(color.NRGBA{R: 80, G: 160, B: 220, A: 255}, 60, 28)
	content := fixedRect(color.NRGBA{R: 120, G: 120, B: 120, A: 255}, 80, 36)

	aWidget := livePopover(t, popover.Props{
		Open:      rx.Of(true),
		Anchor:    anchor,
		Content:   content,
		Placement: popover.Top,
		OnDismiss: func(_ layout.Context) { aDismissed++ },
	})
	// Subscribing B sets arbitration top to B; A's next frame should
	// observe the change and fire OnDismiss.
	_ = livePopover(t, popover.Props{
		Open:      rx.Of(true),
		Anchor:    anchor,
		Content:   content,
		Placement: popover.Bottom,
		OnDismiss: func(_ layout.Context) {},
	})

	r := new(gioinput.Router)
	ops := new(op.Ops)
	driveFrame(aWidget, ops, r, canvasSize)

	if aDismissed == 0 {
		t.Fatalf("opening a second popover did not dismiss the first; aDismissed = %d", aDismissed)
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
