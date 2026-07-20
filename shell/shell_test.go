package shell_test

import (
	"context"
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
	"github.com/vibrantgio/cadence/shell"
	"github.com/vibrantgio/cadence/sidebar"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	shmW, shmH       = 480, 256 // sidebar-header-main canvas
	splitW, splitH   = 480, 128 // split-pane canvas
	vsplitW, vsplitH = 128, 480 // vertical-axis split-pane canvas
	dragCanvasW      = 200
	dragCanvasH      = 100
	tabCanvasW       = 480
	tabCanvasH       = 256
)

var (
	shmSize    = image.Pt(shmW, shmH)
	splitSize  = image.Pt(splitW, splitH)
	vsplitSize = image.Pt(vsplitW, vsplitH)
	dragSize   = image.Pt(dragCanvasW, dragCanvasH)
	vdragSize  = image.Pt(dragCanvasH, dragCanvasW) // 100×200: tall canvas for Y drags
	tabSize    = image.Pt(tabCanvasW, tabCanvasH)
)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

func testIcon() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(16, 16)
		paint.FillShape(gtx.Ops, color.NRGBA{R: 0x3b, G: 0x82, B: 0xf6, A: 0xff}, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// fillRect is a solid-coloured filler used as Main / Left / Right slots
// in goldens; its colour distinguishes regions visually.
func fillRect(c color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

func scene(w layout.Widget, bg color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// TestShellGolden records or diffs the four Measurable goldens.
func TestShellGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}
	leftFill := color.NRGBA{R: 0x22, G: 0x55, B: 0x88, A: 0xff}
	rightFill := color.NRGBA{R: 0x88, G: 0x55, B: 0x22, A: 0xff}
	mainFill := color.NRGBA{R: 0x33, G: 0x99, B: 0x66, A: 0xff}

	shmSidebarProps := sidebar.Props{
		Items: []sidebar.Item{
			{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) {}},
			{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) {}},
		},
		Shaper: shaper,
	}

	shmProps := func() shell.Props {
		links := []navbar.Link{{Label: ""}, {Label: ""}}
		return shell.Props{
			Layout: shell.SidebarHeaderMain,
			Navbar: navbar.Props{Links: links, Shaper: shaper},
			Main:   fillRect(mainFill),
		}
	}

	splitProps := func(axis layout.Axis) shell.Props {
		return shell.Props{
			Layout:    shell.SplitPane,
			Left:      fillRect(leftFill),
			Right:     fillRect(rightFill),
			SplitAxis: axis,
		}
	}

	cases := []struct {
		name         string
		props        shell.Props
		sidebarProps *sidebar.Props
		colors       tokens.ColorTokens
		bg           color.NRGBA
		size         image.Point
		ratio        float32
	}{
		{"light-sidebar-header-main", shmProps(), &shmSidebarProps, tokens.DefaultLight, lightBG, shmSize, 0},
		{"dark-sidebar-header-main", shmProps(), &shmSidebarProps, tokens.DefaultDark, darkBG, shmSize, 0},
		{"light-split-pane-50-50", splitProps(layout.Horizontal), nil, tokens.DefaultLight, lightBG, splitSize, 0.5},
		{"light-split-pane-30-70", splitProps(layout.Horizontal), nil, tokens.DefaultLight, lightBG, splitSize, 0.3},
		{"light-split-pane-vertical-30-70", splitProps(layout.Vertical), nil, tokens.DefaultLight, lightBG, vsplitSize, 0.3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sidebarW layout.Widget
			if tc.sidebarProps != nil {
				sidebarW = sidebar.Render(shaper, *tc.sidebarProps, false, tc.colors, tokens.Spacing, tokens.DefaultTypeScale)
			}
			w := shell.Render(shaper, tc.props, sidebarW, tc.colors, tokens.Spacing, tokens.DefaultTypeScale, tc.ratio)
			renderGolden(t, tc.name, tc.size, scene(w, tc.bg))
		})
	}
}

// ---- Interaction tests ----

func liveWidget(t *testing.T, sh rx.Observable[layout.Widget]) layout.Widget {
	t.Helper()
	var w layout.Widget
	if err := sh.Subscribe(context.Background(), func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}).Wait(); err != nil {
		t.Fatalf("Shell subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Shell did not emit an initial widget")
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

// TestShellSplitPaneDividerDrag verifies that pressing on the divider
// and dragging horizontally emits ratio updates via OnSplitChange.
// With PxPerDp=1 and canvas 200×100 at initial ratio 0.5, the divider
// (6 px wide) sits at x ∈ [97, 103]. A press at (100, 50) followed by
// a drag to (150, 50) shifts the ratio by 50/200 = +0.25, so the
// expected new ratio is 0.75.
func TestShellSplitPaneDividerDrag(t *testing.T) {
	var got []float32
	props := shell.Props{
		Layout:        shell.SplitPane,
		SplitRatio:    rx.Of(float32(0.5)),
		OnSplitChange: func(_ layout.Context, r float32) { got = append(got, r) },
	}
	w := liveWidget(t, shell.Shell(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	// Warm-up frames so the divider's clip area is registered with the
	// router before pointer events are queued.
	driveFrame(w, ops, r, dragSize)
	driveFrame(w, ops, r, dragSize)

	press := f32.Pt(100, 50)
	drag := f32.Pt(150, 50)
	// Press at the divider, Move to drag — the router converts the Move
	// to a pointer.Drag for the press target. Release ends the gesture.
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: press, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Move, Position: drag, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: drag, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, dragSize)

	if len(got) == 0 {
		t.Fatalf("OnSplitChange not invoked; want at least one update")
	}
	last := got[len(got)-1]
	const want = 0.75
	const eps = 0.01
	if last < want-eps || last > want+eps {
		t.Errorf("final ratio = %v; want ~%v", last, want)
	}
}

// TestShellSplitPaneVerticalDividerDrag is the SplitAxis=Vertical
// counterpart of TestShellSplitPaneDividerDrag. With PxPerDp=1 and a
// 100×200 canvas at initial ratio 0.5, the horizontal divider (6 px
// thick) sits at y ∈ [97, 103]. A press at (50, 100) followed by a
// drag to (50, 150) shifts the ratio by 50/200 = +0.25, so the
// expected new ratio is 0.75.
func TestShellSplitPaneVerticalDividerDrag(t *testing.T) {
	var got []float32
	props := shell.Props{
		Layout:        shell.SplitPane,
		SplitAxis:     layout.Vertical,
		SplitRatio:    rx.Of(float32(0.5)),
		OnSplitChange: func(_ layout.Context, r float32) { got = append(got, r) },
	}
	w := liveWidget(t, shell.Shell(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	driveFrame(w, ops, r, vdragSize)
	driveFrame(w, ops, r, vdragSize)

	press := f32.Pt(50, 100)
	drag := f32.Pt(50, 150)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: press, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Move, Position: drag, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: drag, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, vdragSize)

	if len(got) == 0 {
		t.Fatalf("OnSplitChange not invoked; want at least one update")
	}
	last := got[len(got)-1]
	const want = 0.75
	const eps = 0.01
	if last < want-eps || last > want+eps {
		t.Errorf("final ratio = %v; want ~%v", last, want)
	}
}

// TestShellSidebarHeaderMainTabTraversal verifies that Tab focus
// traverses the shell in document order sidebar → navbar → main. Three
// regions each contribute a focusable; the navbar gets an *external*
// handle (a Brand clickable owned by the test) and the main slot gets
// another. The sidebar item is owned by the sidebar package and is
// only externally observable as "neither brand nor main focused". With
// a seed clickable anchoring focus before the shell, the expected
// sequence is:
//
//	Tab #1 → sidebar item       (seed=false, brand=false, main=false)
//	Tab #2 → navbar brand       (brand=true)
//	Tab #3 → navbar link        (brand=false, main=false)
//	Tab #4 → main               (main=true)
//
// Any other ordering of regions would move brand and/or main to
// different positions in the sequence.
func TestShellSidebarHeaderMainTabTraversal(t *testing.T) {
	shaper := defaultShaper(t)
	var mainClick widget.Clickable
	var brandClick widget.Clickable
	var seedClick widget.Clickable

	mainWidget := func(gtx layout.Context) layout.Dimensions {
		return mainClick.Layout(gtx, fillRect(color.NRGBA{R: 0, G: 200, B: 0, A: 255}))
	}
	brandWidget := func(gtx layout.Context) layout.Dimensions {
		return brandClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(40, 20)
			paint.FillShape(gtx.Ops, color.NRGBA{R: 80, G: 80, B: 200, A: 255}, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		})
	}

	props := shell.Props{
		Layout: shell.SidebarHeaderMain,
		Sidebar: sidebar.Sidebar(rx.Of(theme.Default()), sidebar.Props{
			Items: []sidebar.Item{
				{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) {}},
			},
			Collapsed: rx.Of(false),
			Shaper:    shaper,
		}),
		Navbar: navbar.Props{
			Brand: brandWidget,
			Links: []navbar.Link{
				{Label: "", OnClick: func(_ layout.Context) {}},
			},
			Shaper: shaper,
		},
		Main: mainWidget,
	}
	body := shell.Shell(rx.Of(theme.Default()), props)
	bodyW := liveWidget(t, body)

	// Compose: a seed clickable (zero-size visual) then the shell. The
	// seed is a focus anchor whose position in the op-stream is before
	// the shell, so MoveFocus(Forward) from the seed enters the shell
	// at its first focusable.
	composed := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return seedClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(1, 1)}
				})
			}),
			layout.Flexed(1, bodyW),
		)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)

	// Two warm-up frames for stable hit-test data.
	driveFrame(composed, ops, r, tabSize)
	driveFrame(composed, ops, r, tabSize)

	// Drain any synthetic focus events on the seed so the router retains
	// focus when explicitly set, matching the FocusGroup idiom used in
	// the navbar test.
	drainFocus := func() {
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(tabSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		for _, tag := range []any{&seedClick, &brandClick, &mainClick} {
			for {
				if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
					break
				}
			}
		}
	}
	drainFocus()

	// Anchor focus at the seed.
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(tabSize),
		Ops:         ops,
		Source:      r.Source(),
	}
	gtx.Execute(key.FocusCmd{Tag: &seedClick})
	driveFrame(composed, ops, r, tabSize)

	check := func(stage string, wantSeed, wantBrand, wantMain bool) {
		t.Helper()
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(tabSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		gotSeed := gtx.Focused(&seedClick)
		gotBrand := gtx.Focused(&brandClick)
		gotMain := gtx.Focused(&mainClick)
		if gotSeed != wantSeed || gotBrand != wantBrand || gotMain != wantMain {
			t.Errorf("%s: focused(seed)=%v brand=%v main=%v; want seed=%v brand=%v main=%v",
				stage, gotSeed, gotBrand, gotMain, wantSeed, wantBrand, wantMain)
		}
	}

	check("after Focus(seed)", true, false, false)

	// Tab #1 → into the sidebar's first item.
	r.MoveFocus(key.FocusForward)
	driveFrame(composed, ops, r, tabSize)
	check("Tab #1 (→ sidebar item)", false, false, false)

	// Tab #2 → into the navbar's brand (externally observable). If the
	// shell composed navbar before sidebar, this stop would already
	// have been Tab #1.
	r.MoveFocus(key.FocusForward)
	driveFrame(composed, ops, r, tabSize)
	check("Tab #2 (→ navbar brand)", false, true, false)

	// Tab #3 → into the navbar's first link.
	r.MoveFocus(key.FocusForward)
	driveFrame(composed, ops, r, tabSize)
	check("Tab #3 (→ navbar link)", false, false, false)

	// Tab #4 → into main.
	r.MoveFocus(key.FocusForward)
	driveFrame(composed, ops, r, tabSize)
	check("Tab #4 (→ main)", false, false, true)
}

// TestShellCustomSidebarWidget confirms that a caller-supplied
// rx.Observable[layout.Widget] (not sidebar.Sidebar) works as the Sidebar
// slot and that the op-stream order is sidebar → navbar → main, preserving
// Tab focus traversal. Structure mirrors TestShellSidebarHeaderMainTabTraversal;
// the only delta is Props.Sidebar being a plain rx.Of widget instead of
// sidebar.Sidebar.
func TestShellCustomSidebarWidget(t *testing.T) {
	shaper := defaultShaper(t)
	var mainClick widget.Clickable
	var brandClick widget.Clickable
	var seedClick widget.Clickable
	var customSBClick widget.Clickable

	customSBWidget := func(gtx layout.Context) layout.Dimensions {
		return customSBClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(40, 256)
			paint.FillShape(gtx.Ops, color.NRGBA{R: 60, G: 60, B: 60, A: 255}, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		})
	}

	mainWidget := func(gtx layout.Context) layout.Dimensions {
		return mainClick.Layout(gtx, fillRect(color.NRGBA{R: 0, G: 200, B: 0, A: 255}))
	}
	brandWidget := func(gtx layout.Context) layout.Dimensions {
		return brandClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(40, 20)
			paint.FillShape(gtx.Ops, color.NRGBA{R: 80, G: 80, B: 200, A: 255}, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		})
	}

	props := shell.Props{
		Layout:  shell.SidebarHeaderMain,
		Sidebar: rx.Of[layout.Widget](customSBWidget),
		Navbar: navbar.Props{
			Brand: brandWidget,
			Links: []navbar.Link{
				{Label: "", OnClick: func(_ layout.Context) {}},
			},
			Shaper: shaper,
		},
		Main: mainWidget,
	}
	body := shell.Shell(rx.Of(theme.Default()), props)
	bodyW := liveWidget(t, body)

	composed := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return seedClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(1, 1)}
				})
			}),
			layout.Flexed(1, bodyW),
		)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)

	driveFrame(composed, ops, r, tabSize)
	driveFrame(composed, ops, r, tabSize)

	drainFocus := func() {
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(tabSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		for _, tag := range []any{&seedClick, &customSBClick, &brandClick, &mainClick} {
			for {
				if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
					break
				}
			}
		}
	}
	drainFocus()

	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(tabSize),
		Ops:         ops,
		Source:      r.Source(),
	}
	gtx.Execute(key.FocusCmd{Tag: &seedClick})
	driveFrame(composed, ops, r, tabSize)

	check := func(stage string, wantSeed, wantCustomSB, wantBrand, wantMain bool) {
		t.Helper()
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(tabSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		gotSeed := gtx.Focused(&seedClick)
		gotCustomSB := gtx.Focused(&customSBClick)
		gotBrand := gtx.Focused(&brandClick)
		gotMain := gtx.Focused(&mainClick)
		if gotSeed != wantSeed || gotCustomSB != wantCustomSB || gotBrand != wantBrand || gotMain != wantMain {
			t.Errorf("%s: seed=%v customSB=%v brand=%v main=%v; want seed=%v customSB=%v brand=%v main=%v",
				stage, gotSeed, gotCustomSB, gotBrand, gotMain, wantSeed, wantCustomSB, wantBrand, wantMain)
		}
	}

	check("after Focus(seed)", true, false, false, false)

	// Tab #1 → custom sidebar widget (rendered first in Flex op-stream).
	r.MoveFocus(key.FocusForward)
	driveFrame(composed, ops, r, tabSize)
	check("Tab #1 (→ custom sidebar)", false, true, false, false)

	// Tab #2 → navbar brand.
	r.MoveFocus(key.FocusForward)
	driveFrame(composed, ops, r, tabSize)
	check("Tab #2 (→ navbar brand)", false, false, true, false)

	// Tab #3 → navbar link.
	r.MoveFocus(key.FocusForward)
	driveFrame(composed, ops, r, tabSize)
	check("Tab #3 (→ navbar link)", false, false, false, false)

	// Tab #4 → main.
	r.MoveFocus(key.FocusForward)
	driveFrame(composed, ops, r, tabSize)
	check("Tab #4 (→ main)", false, false, false, true)
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
