package tabs_test

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
	"github.com/vibrantgio/cadence/tabs"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 240, 128
)

var canvasSize = image.Pt(canvasW, canvasH)

// defaultShaper returns the shaper every golden here draws with: the default
// typography's faces pinned, system fonts off, so the stored images are the
// same on every machine. A golden test pins its faces with
// DeterministicShaper; application code takes the fallback Shaper. See
// AGENTS.md.
func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return tokens.DefaultTypography.DeterministicShaper()
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

// tabLabels names the three tabs in document order. They were blank until
// F4.4b, on the theory that font rasterisation was non-deterministic; F4.2
// pinned the faces by configuration and F4.3 moved every golden onto
// DeterministicShaper, so Latin text in Roboto rasterises identically on every
// machine. ASCII only, per F4.2 — no symbol reaches a stored image.
//
// They are short on purpose. A tab cell is Rigid and sized to its label plus
// 2×S3 of padding, so three long labels would run off the 240 px canvas; these
// three leave the strip comfortably inside it.
var tabLabels = []string{"Preview", "Code", "Notes"}

// threeTabs returns a deterministic three-tab fixture. The per-tab content
// colours are unrelated to the theme so the same fixture is reused for the
// light and dark goldens; the labels carry the typography.
func threeTabs() []tabs.Tab {
	return []tabs.Tab{
		{Label: tabLabels[0], Content: contentRect(color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xff})},
		{Label: tabLabels[1], Content: contentRect(color.NRGBA{R: 0x40, G: 0xc0, B: 0x60, A: 0xff})},
		{Label: tabLabels[2], Content: contentRect(color.NRGBA{R: 0x40, G: 0x70, B: 0xff, A: 0xff})},
	}
}

func singleTab() []tabs.Tab {
	return []tabs.Tab{
		{Label: tabLabels[0], Content: contentRect(color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xff})},
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
			w := tabs.Render(shaper, props, tc.selected, tc.colors, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.bg))
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
		w := tabs.Render(shaper, props, selected, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
		return golden.Capture(t, canvasSize, scene(w, bg))
	}

	none := render(-1)
	first := render(0)
	if n := golden.PixelDiff(none, first); n == 0 {
		t.Errorf("selected and unselected render identically; expected Primary underline pixels")
	}
}

// ---- Interaction tests ----

func liveWidget(t *testing.T, obs rx.Observable[layout.Widget]) layout.Widget {
	t.Helper()
	var w layout.Widget
	if err := obs.Subscribe(context.Background(), func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}).Wait(); err != nil {
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
// end-to-end. A tab cell is its label plus 2×S3 = 24 px of horizontal
// padding, never narrower than that padding alone, and stripH = 36 px (the
// Comfortable control height, E1.4; PxPerDp = 1). Tab 0 is "Preview", so it
// is wider than the 24 px minimum and starts at x = 0 — a pointer click at
// (12, 20) lands squarely inside it whatever the label, which is why this
// coordinate survived F4.4b filling the labels in.
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

// densityTheme returns a theme whose density is d, with sharp corners
// for golden determinism — the E1.4 injection idiom, mirroring prism's
// density tests.
func densityTheme(d tokens.Density) theme.Theme {
	th := theme.Default()
	th.Density = rx.Of(d)
	th.Radius = rx.Of(tokens.RadiusScale{})
	return th
}

// TestTabsCompactGolden records or diffs the compact-density golden
// through the LIVE pipeline (the static Render path is frozen at
// tokens.Comfortable): the strip height drops from 36 to 28 dp
// (ControlHeight) and the content panel grows by the difference.
func TestTabsCompactGolden(t *testing.T) {
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	props := tabs.Props{Tabs: threeTabs(), Selected: rx.Of(0), Shaper: defaultShaper(t)}
	w := liveWidget(t, tabs.Tabs(rx.Of(densityTheme(tokens.Compact)), props))
	golden.Render(t, "light-compact-three-tabs-first-selected", canvasSize, scene(w, lightBG))
}
