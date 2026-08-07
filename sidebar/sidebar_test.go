package sidebar_test

import (
	"context"
	"image"
	"image/color"
	"testing"

	"gioui.org/f32"
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
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

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

// itemLabels names the navigation items in document order, twelve deep so the
// overflow goldens read as twelve distinct rows rather than twelve copies.
// They were blank until F4.4b, on the theory that font rasterisation was
// non-deterministic; F4.2 pinned the faces by configuration and F4.3 moved
// every golden onto DeterministicShaper, so Latin text in Roboto rasterises
// identically on every machine. ASCII only, per F4.2 — no symbol reaches a
// stored image.
//
// They are short because the expanded rail is 192 px wide and a row is the
// icon, a gap and a MaxLines:1 label: a longer name is ellipsized, not
// wrapped. The collapsed rail drops the label entirely, which is what makes
// the collapsed goldens still meaningful.
var itemLabels = []string{
	"Overview", "Tokens", "Colour", "Type", "Density", "Motion",
	"Elevation", "Icons", "Layout", "Forms", "Tables", "Charts",
}

// navItems returns n items with the default icon, labelled in order, and the
// i'th marked Active when i == activeIdx (activeIdx < 0 marks none).
func navItems(n, activeIdx int) []sidebar.Item {
	out := make([]sidebar.Item, n)
	for i := range out {
		out[i] = sidebar.Item{Icon: testIcon(), Label: itemLabels[i], OnClick: func(_ layout.Context) {}}
		out[i].Active = i == activeIdx
	}
	return out
}

func scene(w layout.Widget, bg color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// TestSidebarGolden records or diffs the three Measurable goldens. The icon
// is a fixed colour square so it renders identically across themes; the labels
// carry the typography, and light-collapsed is the assertion that the
// collapsed rail drops them.
func TestSidebarGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

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
			props := sidebar.Props{Items: navItems(3, tc.activeIdx), Shaper: shaper}
			w := sidebar.Render(shaper, props, tc.collapsed, tc.colors, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
			golden.Render(t, tc.name, tc.size, scene(w, tc.bg))
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

	// render takes the *testing.T it is called with rather than closing over
	// the outer one, and that parameter is the whole point of it. golden.Capture
	// skips — via t.Skipf, which is runtime.Goexit — on any machine without a
	// headless GPU, which is every CI runner this organization has. Handed the
	// parent's t from inside a subtest, that Goexit unwinds the subtest's
	// goroutine while marking the parent skipped, and the testing package reports
	// it as "subtest may have called FailNow on a parent test" — a failure, not a
	// skip. That is precisely why cadence was red on all sixteen CI runs while
	// green on every developer machine: locally Capture never reaches the skip.
	render := func(t *testing.T, activeIdx int, colors tokens.ColorTokens) *image.RGBA {
		t.Helper()
		props := sidebar.Props{Items: navItems(2, activeIdx), Shaper: shaper}
		w := sidebar.Render(shaper, props, false, colors, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
		return golden.Capture(t, expandedSize, scene(w, bg))
	}

	for _, c := range []struct {
		name   string
		colors tokens.ColorTokens
	}{
		{"light", tokens.DefaultLight},
		{"dark", tokens.DefaultDark},
	} {
		t.Run(c.name, func(t *testing.T) {
			def := render(t, -1, c.colors)
			act := render(t, 0, c.colors)
			if n := golden.PixelDiff(def, act); n == 0 {
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
//   - Arrow-Down from item 0 moves the selection to item 1,
//   - Arrow-Up from item 1 moves it back to item 0,
//   - Enter activates the selected item via its OnClick.
//
// Since F4.7 the selection lives in the prism/list scroll region and the
// rail has one focus tag rather than one per row, so what the click at
// the start seeds is the list's focus, not the row's.
//
// With PxPerDp=1 and an expanded sidebar (192 wide), the toggle
// occupies y∈[0,48] and item i occupies y∈[48+48i, 48+48(i+1)]. A
// pointer click at (96, 72) lands on item 0 and gives it focus —
// the seed used to drive subsequent arrow-key traversal.
func TestSidebarArrowTraversalAndEnter(t *testing.T) {
	var fired [3]int
	props := sidebar.Props{
		Items: []sidebar.Item{
			{Icon: testIcon(), Label: itemLabels[0], OnClick: func(_ layout.Context) { fired[0]++ }},
			{Icon: testIcon(), Label: itemLabels[1], OnClick: func(_ layout.Context) { fired[1]++ }},
			{Icon: testIcon(), Label: itemLabels[2], OnClick: func(_ layout.Context) { fired[2]++ }},
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

// TestSidebarKeyboardReachesAnItemNeverLaidOut is the FX.6 gap, closed.
//
// Twelve items in a 256 px rail at Comfortable: the toggle takes the
// first 36 px and each item 36 more, so the last row a frame could
// possibly lay out starts at y=36+36×6=252 and item 11 would start at
// y=432 — 176 px past the bottom of the canvas. It is not merely
// offscreen, it has never existed: no clip area, no focus tag, nothing
// for Tab to find. Before F4.7 the keyboard therefore stopped at the
// viewport edge and items 7..11 were unreachable without a mouse.
//
// End must select item 11 anyway, and Enter must fire its OnClick.
func TestSidebarKeyboardReachesAnItemNeverLaidOut(t *testing.T) {
	const n = 12
	const rowH = 36 // tokens.Comfortable.ControlHeight at PxPerDp=1
	if top := rowH + rowH*(n-1); top <= canvasH {
		t.Fatalf("item %d starts at y=%d, inside the %d px canvas; this test needs it to be unlaid-out",
			n-1, top, canvasH)
	}

	fired := make([]int, n)
	items := make([]sidebar.Item, n)
	for i := range items {
		i := i
		items[i] = sidebar.Item{Icon: indexIcon(i), Label: itemLabels[i], OnClick: func(_ layout.Context) { fired[i]++ }}
	}
	props := sidebar.Props{Items: items, Collapsed: rx.Of(false), Shaper: defaultShaper(t)}
	w := liveWidget(t, sidebar.Sidebar(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	driveFrame(w, ops, r, expandedSize)
	driveFrame(w, ops, r, expandedSize)

	// Seed the rail's focus the way a user does: click item 0, which sits
	// at y∈[36,72).
	hit := f32.Pt(96, 54)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: hit, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: hit, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, expandedSize)
	if fired[0] != 1 {
		t.Fatalf("click on item 0 fired %d time(s), want 1", fired[0])
	}

	before := golden.Capture(t, expandedSize, w)

	// End selects the last item — the one no frame has laid out.
	r.Queue(key.Event{Name: key.NameEnd, State: key.Press})
	driveFrame(w, ops, r, expandedSize)
	after := golden.Capture(t, expandedSize, w)
	if before != nil && after != nil {
		if d := golden.PixelDiff(before, after); d == 0 {
			t.Error("End changed nothing on screen: the selection did not move or the list did not follow it")
		}
	}

	// Enter activates it.
	r.Queue(
		key.Event{Name: key.NameReturn, State: key.Press},
		key.Event{Name: key.NameReturn, State: key.Release},
	)
	driveFrame(w, ops, r, expandedSize)
	if fired[n-1] != 1 {
		t.Fatalf("after End+Enter, item %d fired %d time(s), want 1; fired=%v", n-1, fired[n-1], fired)
	}

	// Home walks the whole way back.
	r.Queue(key.Event{Name: key.NameHome, State: key.Press})
	driveFrame(w, ops, r, expandedSize)
	r.Queue(
		key.Event{Name: key.NameReturn, State: key.Press},
		key.Event{Name: key.NameReturn, State: key.Release},
	)
	driveFrame(w, ops, r, expandedSize)
	if fired[0] != 2 {
		t.Fatalf("after Home+Enter, item 0 fired %d time(s), want 2; fired=%v", fired[0], fired)
	}
}

// TestSidebarActiveSeedsTheSelection pins the reconciliation F4.7 chose
// between the caller's Item.Active and the list's own selection: Active
// is a seed, not a competitor. The rail starts highlighting the Active
// row, and the keyboard then moves the highlight off it — which is only
// observable because both render through the same code path.
func TestSidebarActiveSeedsTheSelection(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	props := sidebar.Props{Items: navItems(3, 1), Collapsed: rx.Of(false), Shaper: shaper}
	w := liveWidget(t, sidebar.Sidebar(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	driveFrame(w, ops, r, expandedSize)
	driveFrame(w, ops, r, expandedSize)

	// The live rail with Active=1 must look like the static render with
	// Active=1: the seed reaches the same highlight the Render path draws.
	live := golden.Capture(t, expandedSize, scene(w, bg))
	static := sidebar.Render(shaper, sidebar.Props{Items: navItems(3, 1), Shaper: shaper},
		false, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
	want := golden.Capture(t, expandedSize, scene(static, bg))
	if live != nil && want != nil {
		if d := golden.PixelDiff(live, want); d != 0 {
			t.Errorf("live rail seeded from Active differs from the static render by %d pixel(s); want 0", d)
		}
	}

	// Tab into the rail. It is a single keyboard stop since F4.7 — the
	// scroll region — so one FocusForward is all it takes, and there is
	// nothing else in this widget for the router to land on.
	r.MoveFocus(key.FocusForward)
	driveFrame(w, ops, r, expandedSize)

	// Home moves the selection to item 0, and the highlight with it.
	r.Queue(key.Event{Name: key.NameHome, State: key.Press})
	driveFrame(w, ops, r, expandedSize)
	moved := golden.Capture(t, expandedSize, scene(w, bg))
	if live != nil && moved != nil {
		if d := golden.PixelDiff(live, moved); d == 0 {
			t.Error("Home left the highlight on the Active item; the selection is not driving it")
		}
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
			{Icon: testIcon(), Label: itemLabels[0], OnClick: func(_ layout.Context) {}},
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
	props := sidebar.Props{Items: navItems(3, 1), Collapsed: rx.Of(false), Shaper: defaultShaper(t)}
	w := liveWidget(t, sidebar.Sidebar(rx.Of(densityTheme(tokens.Compact)), props))
	golden.Render(t, "light-compact-expanded", expandedSize, scene(w, lightBG))
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
				items[i] = sidebar.Item{Icon: indexIcon(i), Label: itemLabels[i], OnClick: func(_ layout.Context) {}}
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
			before := golden.Capture(t, tc.size, scene(w, lightBG))

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

			after := golden.Capture(t, tc.size, scene(w, lightBG))
			if before != nil && after != nil {
				if d := golden.PixelDiff(before, after); d == 0 {
					t.Fatalf("scroll event moved nothing: overflowing list did not scroll")
				}
			}
			golden.Render(t, tc.name, tc.size, scene(w, lightBG))
		})
	}
}
