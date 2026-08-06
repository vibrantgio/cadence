package sidebar_test

import (
	"context"
	"flag"
	"fmt"
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
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/sidebar"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	expandedW  = 192
	collapsedW = 48
	canvasH    = 256
)

var (
	expandedSize  = image.Pt(expandedW, canvasH)
	collapsedSize = image.Pt(collapsedW, canvasH)
)

// defaultShaper returns the shaper every golden here draws with: the default
// typography's faces pinned, system fonts off, so the stored images are the
// same on every machine. A golden test pins its faces with
// DeterministicShaper; application code takes the fallback Shaper. See
// AGENTS.md.
func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return tokens.DefaultTypography.DeterministicShaper()
}

// testIcon returns a 16×16 filled square in a fixed mid-Blue colour.
// Using a deterministic shape avoids GPU font rasterisation differences
// across platforms.
func testIcon() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(16, 16)
		paint.FillShape(gtx.Ops, color.NRGBA{R: 0x3b, G: 0x82, B: 0xf6, A: 0xff}, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

func scene(w layout.Widget, bg color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// TestSidebarGolden records or diffs the three Measurable goldens.
// Labels are empty so the goldens do not depend on GPU font
// rasterisation; the icon is a fixed colour square so it renders
// identically across themes.
func TestSidebarGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	items := func(activeIdx int) []sidebar.Item {
		out := make([]sidebar.Item, 3)
		for i := range out {
			out[i] = sidebar.Item{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) {}}
			if i == activeIdx {
				out[i].Active = true
			}
		}
		return out
	}

	cases := []struct {
		name      string
		collapsed bool
		colors    tokens.ColorTokens
		bg        color.NRGBA
		size      image.Point
		activeIdx int
	}{
		{"light-expanded", false, tokens.DefaultLight, lightBG, expandedSize, -1},
		{"light-collapsed", true, tokens.DefaultLight, lightBG, collapsedSize, -1},
		{"dark-expanded-active-second", false, tokens.DefaultDark, darkBG, expandedSize, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := sidebar.Props{Items: items(tc.activeIdx), Shaper: shaper}
			w := sidebar.Render(shaper, props, tc.collapsed, tc.colors, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
			renderGolden(t, tc.name, tc.size, scene(w, tc.bg))
		})
	}
}

// TestSidebarActiveTintIsVisible guards the visual contract that the
// Active item adds Primary-tinted pixels on both light and dark
// schemes. A tint that drops below the alpha threshold and becomes a
// no-op would silently break the active-item indicator.
func TestSidebarActiveTintIsVisible(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	render := func(activeIdx int, colors tokens.ColorTokens) *image.RGBA {
		items := []sidebar.Item{
			{Icon: testIcon(), OnClick: func(_ layout.Context) {}},
			{Icon: testIcon(), OnClick: func(_ layout.Context) {}},
		}
		if activeIdx >= 0 {
			items[activeIdx].Active = true
		}
		props := sidebar.Props{Items: items, Shaper: shaper}
		w := sidebar.Render(shaper, props, false, colors, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
		return capture(t, expandedSize, scene(w, bg))
	}

	for _, c := range []struct {
		name   string
		colors tokens.ColorTokens
	}{
		{"light", tokens.DefaultLight},
		{"dark", tokens.DefaultDark},
	} {
		t.Run(c.name, func(t *testing.T) {
			def := render(-1, c.colors)
			act := render(0, c.colors)
			if def == nil || act == nil {
				return
			}
			if n := pixelDiff(def, act); n == 0 {
				t.Errorf("%s: active and default render identically; expected Primary tint pixels", c.name)
			}
		})
	}
}

// ---- Interaction tests ----

// liveWidget subscribes to sb, drains the trampoline scheduler, and
// returns the latest emitted layout.Widget. State referenced by the
// widget closure remains valid for the test's lifetime because it is
// captured by the rx.Defer scope.
func liveWidget(t *testing.T, sb rx.Observable[layout.Widget]) layout.Widget {
	t.Helper()
	var w layout.Widget
	if err := sb.Subscribe(context.Background(), func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}).Wait(); err != nil {
		t.Fatalf("Sidebar subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Sidebar did not emit an initial widget")
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

// TestSidebarArrowTraversalAndEnter verifies that
//   - Arrow-Down from item 0 moves focus to item 1,
//   - Arrow-Up from item 1 moves focus to item 0,
//   - Enter activates the focused item via its OnClick.
//
// With PxPerDp=1 and an expanded sidebar (192 wide), the toggle
// occupies y∈[0,48] and item i occupies y∈[48+48i, 48+48(i+1)]. A
// pointer click at (96, 72) lands on item 0 and gives it focus —
// the seed used to drive subsequent arrow-key traversal.
func TestSidebarArrowTraversalAndEnter(t *testing.T) {
	var fired [3]int
	props := sidebar.Props{
		Items: []sidebar.Item{
			{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) { fired[0]++ }},
			{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) { fired[1]++ }},
			{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) { fired[2]++ }},
		},
		Collapsed: rx.Of(false),
		Shaper:    defaultShaper(t),
	}
	w := liveWidget(t, sidebar.Sidebar(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	driveFrame(w, ops, r, expandedSize)
	driveFrame(w, ops, r, expandedSize)

	// Click item 0 → fires item 0 and gives it focus. With the E1.4
	// density pitch (Comfortable ControlHeight = 36) the toggle occupies
	// y∈[0,36) and item 0 y∈[36,72); (96, 54) lands mid-item-0.
	hit := f32.Pt(96, 54)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: hit, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: hit, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, expandedSize)
	if fired != [3]int{1, 0, 0} {
		t.Fatalf("after click on item 0, fired=%v; want [1 0 0]", fired)
	}

	// Down then Enter → item 1 fires. widget.Clickable requires
	// matched Press + Release on key.NameReturn to register a click.
	r.Queue(key.Event{Name: key.NameDownArrow, State: key.Press})
	driveFrame(w, ops, r, expandedSize)
	r.Queue(
		key.Event{Name: key.NameReturn, State: key.Press},
		key.Event{Name: key.NameReturn, State: key.Release},
	)
	driveFrame(w, ops, r, expandedSize)
	if fired != [3]int{1, 1, 0} {
		t.Fatalf("after Down+Enter, fired=%v; want [1 1 0]", fired)
	}

	// Up then Enter → item 0 fires again.
	r.Queue(key.Event{Name: key.NameUpArrow, State: key.Press})
	driveFrame(w, ops, r, expandedSize)
	r.Queue(
		key.Event{Name: key.NameReturn, State: key.Press},
		key.Event{Name: key.NameReturn, State: key.Release},
	)
	driveFrame(w, ops, r, expandedSize)
	if fired != [3]int{2, 1, 0} {
		t.Fatalf("after Up+Enter, fired=%v; want [2 1 0]", fired)
	}
}

// TestSidebarToggleDispatchesOnToggleCollapse verifies that clicking
// the toggle affordance invokes OnToggleCollapse exactly once. With
// PxPerDp=1, an expanded sidebar (192 wide) renders its toggle as a
// 192×36 hit area (the Comfortable control height, E1.4) at the top of
// the canvas; (96, 24) lands squarely inside.
func TestSidebarToggleDispatchesOnToggleCollapse(t *testing.T) {
	var toggleCount int
	props := sidebar.Props{
		Items: []sidebar.Item{
			{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) {}},
		},
		Collapsed:        rx.Of(false),
		OnToggleCollapse: func(_ layout.Context) { toggleCount++ },
		Shaper:           defaultShaper(t),
	}
	w := liveWidget(t, sidebar.Sidebar(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	// Two warm-up frames so the router has stable hit-test data for the
	// toggle's clip area before pointer events are queued.
	driveFrame(w, ops, r, expandedSize)
	driveFrame(w, ops, r, expandedSize)

	hit := f32.Pt(96, 24)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: hit, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: hit, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, expandedSize)

	if toggleCount != 1 {
		t.Fatalf("OnToggleCollapse fired %d time(s), want 1", toggleCount)
	}
}

// densityTheme returns a theme whose density is d, with sharp corners
// for golden determinism — the E1.4 injection idiom, mirroring prism's
// density tests.
func densityTheme(d tokens.Density) theme.Theme {
	th := theme.Default()
	th.Density = rx.Of(d)
	th.Radius = rx.Of(tokens.RadiusScale{})
	return th
}

// TestSidebarCompactGolden records or diffs the compact-density golden
// through the LIVE pipeline (the static Render path is frozen at
// tokens.Comfortable): the toggle and item pitch drop from 36 to 28 dp.
func TestSidebarCompactGolden(t *testing.T) {
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	items := make([]sidebar.Item, 3)
	for i := range items {
		items[i] = sidebar.Item{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) {}}
	}
	items[1].Active = true
	props := sidebar.Props{Items: items, Collapsed: rx.Of(false), Shaper: defaultShaper(t)}
	w := liveWidget(t, sidebar.Sidebar(rx.Of(densityTheme(tokens.Compact)), props))
	renderGolden(t, "light-compact-expanded", expandedSize, scene(w, lightBG))
}

// indexIcon returns a 16×16 filled square whose colour varies with the
// item index, so rows are visually distinguishable in the overflow
// goldens: a scrolled-to-bottom viewport cannot be mistaken for the top.
func indexIcon(i int) layout.Widget {
	c := color.NRGBA{R: uint8(20 * i), G: 0x80, B: 0xf6, A: 0xff}
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(16, 16)
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// TestSidebarOverflowGolden records or diffs the FX.6 scroll-region
// goldens: 12 items overflow the 256 px canvas at every combination of
// width and density (comfortable fits ~6 rows under the toggle, compact
// ~8). Each case scrolls to the bottom through the live pipeline's
// pointer path before capturing, and the last item is Active, so the
// golden shows the previously unreachable end of the list — the
// highlighted row against the bottom edge. A pixel diff against the
// unscrolled frame guards the scroll itself: if the wheel event stopped
// moving the list, the golden would silently pin the top view.
func TestSidebarOverflowGolden(t *testing.T) {
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	const n = 12

	cases := []struct {
		name      string
		collapsed bool
		density   tokens.Density
		size      image.Point
	}{
		{"light-overflow-expanded-comfortable", false, tokens.Comfortable, expandedSize},
		{"light-overflow-collapsed-comfortable", true, tokens.Comfortable, collapsedSize},
		{"light-overflow-expanded-compact", false, tokens.Compact, expandedSize},
		{"light-overflow-collapsed-compact", true, tokens.Compact, collapsedSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := make([]sidebar.Item, n)
			for i := range items {
				items[i] = sidebar.Item{Icon: indexIcon(i), Label: "", OnClick: func(_ layout.Context) {}}
			}
			items[n-1].Active = true
			props := sidebar.Props{
				Items:     items,
				Collapsed: rx.Of(tc.collapsed),
				Shaper:    defaultShaper(t),
			}
			w := liveWidget(t, sidebar.Sidebar(rx.Of(densityTheme(tc.density)), props))

			r := new(gioinput.Router)
			ops := new(op.Ops)
			driveFrame(w, ops, r, tc.size)
			driveFrame(w, ops, r, tc.size)
			before := capture(t, tc.size, scene(w, lightBG))

			// One wheel event larger than the total overflow; the list
			// clamps at the end. The frame that absorbs the scroll still
			// draws from the old first index; the settled frame follows.
			r.Queue(pointer.Event{
				Kind:     pointer.Scroll,
				Position: f32.Pt(24, 128),
				Scroll:   f32.Pt(0, 600),
				Source:   pointer.Mouse,
			})
			driveFrame(w, ops, r, tc.size)
			driveFrame(w, ops, r, tc.size)

			after := capture(t, tc.size, scene(w, lightBG))
			if before != nil && after != nil {
				if d := pixelDiff(before, after); d == 0 {
					t.Fatalf("scroll event moved nothing: overflowing list did not scroll")
				}
			}
			renderGolden(t, tc.name, tc.size, scene(w, lightBG))
		})
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
	// A size change is a failure in its own right, and it has to be caught
	// here: once the bounds differ there is no pixel count to compare, and
	// pixelDiff refuses to invent one.
	if sb, ib := stored.Bounds(), img.Bounds(); sb != ib {
		actualPath := strings.TrimSuffix(path, ".png") + ".actual.png"
		_ = saveImage(actualPath, img)
		t.Fatalf("%q: size changed: golden is %dx%d, render is %dx%d (actual saved to %s)",
			name, sb.Dx(), sb.Dy(), ib.Dx(), ib.Dy(), actualPath)
	}
	if n := pixelDiff(stored, img); n > 0 {
		actualPath := strings.TrimSuffix(path, ".png") + ".actual.png"
		_ = saveImage(actualPath, img)
		t.Fatalf("%q: %d pixel(s) differ (actual saved to %s)", name, n, actualPath)
	}
}

// pixelDiff counts the pixels that differ between a and b, which must have equal
// bounds. It panics if they do not.
//
// The panic replaces a returned -1. There is no pixel count to report for two
// images of different shapes, and -1 read as "no difference" to every `n > 0`
// test — which is how a golden whose size had moved compared as a pass, here
// and across the whole organization. A caller for which a size change is a
// real outcome rather than a defect — the stored-golden comparison, and only
// it — must compare Bounds itself before calling.
func pixelDiff(a, b *image.RGBA) int {
	if a.Bounds() != b.Bounds() {
		panic(fmt.Sprintf("pixelDiff: images must have equal bounds, got %v and %v",
			a.Bounds(), b.Bounds()))
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
