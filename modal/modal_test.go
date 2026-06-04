package modal_test

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
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/modal"
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

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

// fillRect is a sharp-edged solid widget used as a Body or Action stand-in.
// Text and rounded paths are avoided in goldens because GPU font and AA
// rasterisation are non-deterministic across platforms.
func fillRect(c color.NRGBA, heightDp float32) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(heightDp))
		size := image.Pt(gtx.Constraints.Max.X, h)
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// fixedRect is a sharp-edged solid widget with explicit width and height.
// Used for footer action stand-ins so their hit rect is predictable.
func fixedRect(c color.NRGBA, widthDp, heightDp float32) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(gtx.Dp(unit.Dp(widthDp)), gtx.Dp(unit.Dp(heightDp)))
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// scene renders w into a canvas-sized constraint over a flat background.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// ---- Golden tests ----

// TestModalGolden records or diffs the four Measurable goldens. Title is
// left empty to avoid font rasterisation variance across GPUs; the cross,
// scrim, surface, and action rectangles are all deterministic clip shapes.
func TestModalGolden(t *testing.T) {
	shaper := defaultShaper(t)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 40)
	action1 := fixedRect(color.NRGBA{R: 80, G: 160, B: 220, A: 255}, 60, 28)
	action2 := fixedRect(color.NRGBA{R: 220, G: 100, B: 100, A: 255}, 60, 28)

	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	cases := []struct {
		name    string
		open    bool
		actions []layout.Widget
		colors  tokens.ColorTokens
		bg      color.NRGBA
	}{
		{"light-open", true, nil, tokens.DefaultLight, lightBG},
		{"dark-open", true, nil, tokens.DefaultDark, darkBG},
		{"light-closed", false, nil, tokens.DefaultLight, lightBG},
		{"light-with-actions", true, []layout.Widget{action1, action2}, tokens.DefaultLight, lightBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := modal.Props{
				Title:   "",
				Body:    body,
				Actions: tc.actions,
				Shaper:  shaper,
			}
			w := modal.Render(shaper, props, tc.open, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestModalOpenAndClosedDiffer confirms that flipping the open flag
// changes the rendered output. Catches regressions where the open
// branch silently no-ops.
func TestModalOpenAndClosedDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 40)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}

	open := modal.Render(shaper, modal.Props{Body: body, Shaper: shaper}, true, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
	closed := modal.Render(shaper, modal.Props{Body: body, Shaper: shaper}, false, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)

	imgOpen := capture(t, canvasSize, scene(open, bg))
	imgClosed := capture(t, canvasSize, scene(closed, bg))
	if imgOpen == nil || imgClosed == nil {
		return
	}
	if n := pixelDiff(imgOpen, imgClosed); n == 0 {
		t.Error("open and closed modal render identically; expected scrim + surface in open")
	}
}

// ---- Interaction tests ----

// liveModal subscribes to the Modal observable, drains the trampoline
// scheduler with Wait(), and returns the latest emitted layout.Widget.
// State referenced by the widget closure remains valid for the test's
// lifetime because it is captured by the rx.Defer scope.
func liveModal(t *testing.T, props modal.Props) layout.Widget {
	t.Helper()
	if props.Shaper == nil {
		props.Shaper = defaultShaper(t)
	}
	obs := modal.Modal(rx.Of(theme.Default()), props)
	var w layout.Widget
	if err := obs.Subscribe(func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}, rx.NewScheduler()).Wait(); err != nil {
		t.Fatalf("Modal subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Modal did not emit an initial widget")
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

// TestEscapeInvokesOnClose verifies Measurable (a) — pressing Escape while
// the modal holds focus invokes the OnClose callback.
func TestEscapeInvokesOnClose(t *testing.T) {
	var closed int
	w := liveModal(t, modal.Props{
		Open:    rx.Of(true),
		Body:    fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 40),
		OnClose: func(_ layout.Context) { closed++ },
	})

	r := new(gioinput.Router)
	ops := new(op.Ops)

	// Frame 1: register tags and request initial focus.
	driveFrame(w, ops, r, canvasSize)
	// Frame 2: focus has been applied; the close button now holds focus.
	driveFrame(w, ops, r, canvasSize)

	r.Queue(key.Event{Name: key.NameEscape, State: key.Press})
	driveFrame(w, ops, r, canvasSize)

	if closed != 1 {
		t.Errorf("OnClose call count after Escape = %d, want 1", closed)
	}
}

// TestCloseButtonActivatesOnClose verifies the close affordance — now a
// prism/button keyed to &st.closeClick — invokes OnClose when activated by
// keyboard while focused. The button drains its own Clicked() and routes
// through Props.OnClick; the modal no longer checks Clicked itself, so this
// is the only guard against the close button silently doing nothing.
func TestCloseButtonActivatesOnClose(t *testing.T) {
	var closed int
	w := liveModal(t, modal.Props{
		Open:    rx.Of(true),
		Body:    fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 40),
		OnClose: func(_ layout.Context) { closed++ },
	})

	r := new(gioinput.Router)
	ops := new(op.Ops)

	// Frame 1 registers tags + requests initial focus; frame 2 applies it,
	// leaving the close button focused (same setup as the Escape test).
	driveFrame(w, ops, r, canvasSize)
	driveFrame(w, ops, r, canvasSize)

	// widget.Clickable registers a click on Return/Space release after a
	// matching press while focused — queue both in one frame.
	r.Queue(
		key.Event{Name: key.NameReturn, State: key.Press},
		key.Event{Name: key.NameReturn, State: key.Release},
	)
	driveFrame(w, ops, r, canvasSize)

	if closed != 1 {
		t.Errorf("OnClose call count after close-button activation = %d, want 1", closed)
	}
}

// TestBackdropClickInvokesOnClose verifies Measurable (c) — pressing inside
// the scrim region but outside the modal surface invokes OnClose. A press
// inside the surface must NOT invoke OnClose.
func TestBackdropClickInvokesOnClose(t *testing.T) {
	var closed int
	w := liveModal(t, modal.Props{
		Open:    rx.Of(true),
		Body:    fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 40),
		OnClose: func(_ layout.Context) { closed++ },
	})

	r := new(gioinput.Router)
	ops := new(op.Ops)

	driveFrame(w, ops, r, canvasSize)
	driveFrame(w, ops, r, canvasSize)

	// (1) Press near the top-left corner — guaranteed scrim, never surface.
	corner := f32.Pt(4, 4)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: corner, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Release, Position: corner, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
	)
	driveFrame(w, ops, r, canvasSize)
	if closed == 0 {
		t.Fatalf("scrim click did not invoke OnClose; closed = %d", closed)
	}
	scrimClicks := closed

	// (2) Press at the canvas centre — guaranteed inside the surface — must
	// not invoke OnClose.
	centre := f32.Pt(canvasW/2, canvasH/2)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: centre, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Release, Position: centre, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
	)
	driveFrame(w, ops, r, canvasSize)
	if closed != scrimClicks {
		t.Errorf("surface click bled through to scrim; OnClose went from %d to %d", scrimClicks, closed)
	}
}

// TestTabTrapsFocusWithinModal verifies Measurable (b) — Tab cycles among
// modal focus tags and does not advance focus to a background-registered
// focusable, no matter how many times Tab is pressed.
func TestTabTrapsFocusWithinModal(t *testing.T) {
	// Two footer actions plus the implicit close button → three modal tags.
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 40)
	action1 := fixedRect(color.NRGBA{R: 80, G: 160, B: 220, A: 255}, 60, 28)
	action2 := fixedRect(color.NRGBA{R: 220, G: 100, B: 100, A: 255}, 60, 28)

	// A background focusable that the test will assert focus never reaches.
	backgroundTag := new(int)

	mw := liveModal(t, modal.Props{
		Open:    rx.Of(true),
		Body:    body,
		Actions: []layout.Widget{action1, action2},
		OnClose: func(_ layout.Context) {},
	})

	// Compose the modal over a background that also registers a focusable
	// tag. This is the harder version of the test: it proves Tab cannot
	// escape the modal even when the router has another focus target to
	// advance to.
	composed := func(gtx layout.Context) layout.Dimensions {
		// Background focusable: a 1×1 region with a FocusFilter.
		bgClip := clip.Rect{Max: image.Pt(1, 1)}.Push(gtx.Ops)
		event.Op(gtx.Ops, backgroundTag)
		// Drain the synthetic focus events so the router retains focus
		// when set, matching FocusGroup.Update's idiom.
		for {
			if _, ok := gtx.Event(key.FocusFilter{Target: backgroundTag}); !ok {
				break
			}
		}
		bgClip.Pop()
		return mw(gtx)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)

	driveFrame(composed, ops, r, canvasSize)
	driveFrame(composed, ops, r, canvasSize) // initial focus is applied.

	// At this point the modal's close button holds focus. Press Tab N+1
	// times — far more than the number of focus stops in the modal — and
	// assert focus is still NOT on the background tag.
	for i := 0; i < 12; i++ {
		r.Queue(key.Event{Name: key.NameTab, State: key.Press})
		driveFrame(composed, ops, r, canvasSize)
	}

	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(canvasSize),
		Ops:         ops,
		Source:      r.Source(),
	}
	if gtx.Focused(backgroundTag) {
		t.Fatal("Tab cycle escaped the modal: background tag has focus")
	}
}

// TestShiftTabTrapsFocusWithinModal mirrors TestTabTrapsFocusWithinModal
// for the reverse direction. With Shift+Tab the router would otherwise
// MoveFocus(FocusBackward), so this is a distinct code path on Gio's
// side.
func TestShiftTabTrapsFocusWithinModal(t *testing.T) {
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 40)
	action1 := fixedRect(color.NRGBA{R: 80, G: 160, B: 220, A: 255}, 60, 28)
	action2 := fixedRect(color.NRGBA{R: 220, G: 100, B: 100, A: 255}, 60, 28)

	backgroundTag := new(int)

	mw := liveModal(t, modal.Props{
		Open:    rx.Of(true),
		Body:    body,
		Actions: []layout.Widget{action1, action2},
		OnClose: func(_ layout.Context) {},
	})

	composed := func(gtx layout.Context) layout.Dimensions {
		bgClip := clip.Rect{Max: image.Pt(1, 1)}.Push(gtx.Ops)
		event.Op(gtx.Ops, backgroundTag)
		for {
			if _, ok := gtx.Event(key.FocusFilter{Target: backgroundTag}); !ok {
				break
			}
		}
		bgClip.Pop()
		return mw(gtx)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)

	driveFrame(composed, ops, r, canvasSize)
	driveFrame(composed, ops, r, canvasSize)

	for i := 0; i < 12; i++ {
		r.Queue(key.Event{Name: key.NameTab, Modifiers: key.ModShift, State: key.Press})
		driveFrame(composed, ops, r, canvasSize)
	}

	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(canvasSize),
		Ops:         ops,
		Source:      r.Source(),
	}
	if gtx.Focused(backgroundTag) {
		t.Fatal("Shift+Tab cycle escaped the modal: background tag has focus")
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
