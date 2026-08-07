package breadcrumb_test

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"

	"github.com/vibrantgio/cadence/breadcrumb"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 320, 32
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

// scene renders w into a canvas-sized constraint over a flat background.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// trail is the three-segment fixture: a real path, in document order, so the
// last segment is the current location and the two before it are the
// low-contrast ancestors. The labels were blank until F4.4b, on the theory
// that font rasterisation was non-deterministic; F4.2 pinned the faces by
// configuration and F4.3 moved every golden onto DeterministicShaper, so
// Latin text in Roboto rasterises identically on every machine. ASCII only,
// per F4.2 — no symbol reaches a stored image.
func trail() []breadcrumb.Item {
	return []breadcrumb.Item{{Label: "Home"}, {Label: "Design"}, {Label: "Tokens"}}
}

// TestBreadcrumbGolden records or diffs the three Measurable goldens. The
// chevron separators are deterministic clip paths and the labels carry the
// typography; the single-segment golden is the structural assertion ("no
// chevrons when n == 1") and now also shows that the lone segment takes the
// current-location colour that internal_test asserts numerically.
//
// The two three-segment images moved one pixel in F5.3, and the reason is
// worth keeping. capture constrains the canvas with layout.Exact, and a
// horizontal layout.Flex passes its own cross minimum straight to every Rigid
// child — so each segment label was handed Min.Y == Max.Y == 32. widget.Label
// constrained its result to that 32 and spectrum/typeset then added the line
// box deficit on top, so a label reported 35 px for a 32 px slot and the
// chevrons centred against a row 3 px taller than the row actually was.
// typeset now constrains the corrected height instead of correcting the
// constrained one, the trail measures its slot, and the chevrons sit one pixel
// higher. The labels themselves did not move.
func TestBreadcrumbGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	threeItems := trail()
	singleItem := []breadcrumb.Item{{Label: "Home"}}

	cases := []struct {
		name   string
		items  []breadcrumb.Item
		colors tokens.ColorTokens
		bg     color.NRGBA
	}{
		{"light-three-segments", threeItems, tokens.DefaultLight, lightBG},
		{"dark-three-segments", threeItems, tokens.DefaultDark, darkBG},
		{"light-single-segment", singleItem, tokens.DefaultLight, lightBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := breadcrumb.Props{Items: tc.items, Shaper: shaper}
			w := breadcrumb.Render(shaper, props, tc.colors, tokens.Spacing, tokens.DefaultTypography.TitleSmall)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestBreadcrumbThreeVsSingle confirms a three-segment breadcrumb renders
// differently from a single-segment breadcrumb. Catches regressions where
// the chevron separators silently no-op.
func TestBreadcrumbThreeVsSingle(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}

	render := func(items []breadcrumb.Item) *image.RGBA {
		props := breadcrumb.Props{Items: items, Shaper: shaper}
		w := breadcrumb.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography.TitleSmall)
		return golden.Capture(t, canvasSize, scene(w, bg))
	}

	three := render(trail())
	single := render([]breadcrumb.Item{{Label: "Home"}})
	if n := golden.PixelDiff(three, single); n == 0 {
		t.Errorf("three-segment and single-segment render identically; expected chevrons in three-segment")
	}
}

// TestBreadcrumbLightDarkDiffer confirms swapping the colour token set
// changes the rendered output.
func TestBreadcrumbLightDarkDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	items := trail()
	propsL := breadcrumb.Props{Items: items, Shaper: shaper}
	propsD := breadcrumb.Props{Items: items, Shaper: shaper}
	light := breadcrumb.Render(shaper, propsL, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography.TitleSmall)
	dark := breadcrumb.Render(shaper, propsD, tokens.DefaultDark, tokens.Spacing, tokens.DefaultTypography.TitleSmall)

	imgLight := golden.Capture(t, canvasSize, scene(light, bg))
	imgDark := golden.Capture(t, canvasSize, scene(dark, bg))
	if n := golden.PixelDiff(imgLight, imgDark); n == 0 {
		t.Errorf("light and dark render identically; expected chevron colour differences")
	}
}
