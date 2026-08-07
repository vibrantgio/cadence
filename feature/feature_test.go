package feature_test

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/cadence/feature"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 720, 320
	// scene leaves a small margin around the grid so the outer cells
	// retain breathing room from the canvas edge.
	marginPx = 16
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

// scene renders w into a canvas-sized constraint over a flat background
// with a uniform margin so the outer cells do not touch the canvas edge.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.UniformInset(unit.Dp(float32(marginPx))).Layout(gtx, w)
	}
}

// iconFill returns a solid-colour widget that fills its (icon-cell-sized)
// constraints. Used as an Icon stand-in so the goldens carry a
// deterministic structural marker for the icon slot without depending on
// any vector asset.
func iconFill(c color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// featureBody is the description every cell carries. It is long enough to
// wrap onto more than one line in a three-up cell, which is the whole point:
// the BodyMedium role's line height only shows in the gap between baselines,
// so a body that fits on one line would pin everything about the role except
// that (F4.4). ASCII only — both embedded faces carry every rune, so no
// machine-dependent fallback face can reach a stored image.
const featureBody = "Every token flows from one theme value, so a change lands everywhere at once."

// item returns an Item with the deterministic icon fill and real text. The
// labels were blank until F4.4, on the theory that font rasterisation was
// non-deterministic; F4.2 pinned the faces by configuration, and Latin text in
// Roboto rasterises identically on every machine.
func item(title string) feature.Item {
	return feature.Item{
		Icon:  iconFill(color.NRGBA{R: 60, G: 110, B: 200, A: 255}),
		Title: title,
		Body:  featureBody,
	}
}

// featureTitles names the cells in document order, so a six-item grid is
// legible as six distinct cells rather than six copies.
var featureTitles = []string{"Tokens", "Density", "Elevation", "Motion", "Contrast", "Focus"}

// items returns the first n cells.
func items(n int) []feature.Item {
	out := make([]feature.Item, n)
	for i := range out {
		out[i] = item(featureTitles[i])
	}
	return out
}

// TestFeatureGolden records or diffs the four Measurable goldens: the icon
// fills and grid geometry carry the structural differences between cases, and
// the titles and bodies carry the typography.
func TestFeatureGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	three := items(3)
	two := items(2)
	six := items(6)

	cases := []struct {
		name    string
		colors  tokens.ColorTokens
		bg      color.NRGBA
		columns int
		items   []feature.Item
		size    image.Point
	}{
		{"light-3-up", tokens.DefaultLight, lightBG, 3, three, canvasSize},
		{"dark-3-up", tokens.DefaultDark, darkBG, 3, three, canvasSize},
		{"light-2-up", tokens.DefaultLight, lightBG, 2, two, canvasSize},
		// Two rows of real text do not fit the one-row canvas: with blank
		// labels a cell was an icon fill and nothing else, and 320 px was
		// generous. The taller canvas is what keeps the second row's bodies
		// on screen rather than cut off at the edge.
		{"light-6-items-3-up", tokens.DefaultLight, lightBG, 3, six, image.Pt(canvasW, 2*canvasH)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := feature.Props{Columns: tc.columns, Items: tc.items}
			w := feature.Render(shaper, props, tc.colors, tokens.Spacing, tokens.DefaultTypography)
			golden.Render(t, tc.name, tc.size, scene(w, tc.bg))
		})
	}
}

// TestFeatureColumnsDefaultsToThree confirms Columns=0 renders the same
// as Columns=3.
func TestFeatureColumnsDefaultsToThree(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	cells := items(3)

	zero := feature.Render(shaper, feature.Props{Columns: 0, Items: cells}, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography)
	three := feature.Render(shaper, feature.Props{Columns: 3, Items: cells}, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography)

	a := golden.Capture(t, canvasSize, scene(zero, bg))
	b := golden.Capture(t, canvasSize, scene(three, bg))
	if n := golden.PixelDiff(a, b); n != 0 {
		t.Errorf("Columns=0 default-to-3 contract broken: %d pixel(s) differ from Columns=3", n)
	}
}

// ---- typography (F4.4) ----

// withBodyLineHeight returns a copy of the default typography whose BodyMedium
// role — the role the cell bodies draw in — is on a taller line box, and
// nothing else changed.
func withBodyLineHeight(lh float32) tokens.Typography {
	typo := tokens.DefaultTypography
	typo.BodyMedium.LineHeight = lh
	return typo
}

// featureLineHeightWidget renders the three-up grid with BodyMedium on the
// given line height, on the light theme.
func featureLineHeightWidget(t *testing.T, lh float32) layout.Widget {
	t.Helper()
	w := feature.Render(defaultShaper(t), feature.Props{Columns: 3, Items: items(3)},
		tokens.DefaultLight, tokens.Spacing, withBodyLineHeight(lh))
	return scene(w, color.NRGBA{R: 240, G: 240, B: 240, A: 255})
}

// TestFeatureLineHeightGolden is the golden that pins a role's line height at
// a value that is not the role's own — the only one in the org that varies the
// property rather than inheriting it — and it lives here because feature's cell
// body is the widest wrapped run the system draws.
//
// It used to live here for a narrower reason, worth recording because it is no
// longer true. gioui.org/text spends the line height on the gap between
// baselines and nowhere else — calculateYOffsets baselines the first line at
// that line's own ascent — and widget.Label reports the glyph ink bounds as its
// size, so through widget.Label alone a MaxLines:1 label renders identically at
// any LineHeight. That made a wrapped run the only place the property was
// observable at all, and this was the only golden that had one. F4.4c built
// spectrum/typeset to correct it and F4.4d put every cadence label on it, so
// the property now moves single-line controls too and a dozen goldens in prism
// pin it.
//
// What is left is still worth a golden, and it says more than it did. typeset
// adds the missing first-line box as a deficit rather than a floor, so this
// three-line body occupies exactly 3 × the line height instead of two gaps plus
// one line of glyph ink. Measured on BodyMedium at 14 dp, whose natural line
// inks 17 px: the run is 60 px at line height 20 and 96 px at 32, so the +12
// this test applies lengthens it by 36 px. Before typeset the same two renders
// were 57 and 81, and the +12 bought only 24 — the first line's own leading was
// the part Gio never drew.
func TestFeatureLineHeightGolden(t *testing.T) {
	golden.Render(t, "light-3-up-tall-body-lines", canvasSize,
		featureLineHeightWidget(t, tokens.DefaultTypography.BodyMedium.LineHeight+12))
}

// TestFeatureLineHeightIsDetectable proves the golden above is an instrument
// and not decoration: raising only the BodyMedium role's line height has to
// change pixels. If the property were dropped between tokens.TextStyle and
// widget.Label the two renders would be identical, and this test — not a stale
// image — says so.
func TestFeatureLineHeightIsDetectable(t *testing.T) {
	base := golden.Capture(t, canvasSize, featureLineHeightWidget(t, tokens.DefaultTypography.BodyMedium.LineHeight))
	tall := golden.Capture(t, canvasSize, featureLineHeightWidget(t, tokens.DefaultTypography.BodyMedium.LineHeight+12))
	if n := golden.PixelDiff(base, tall); n == 0 {
		t.Error("raising BodyMedium's line height changed no pixels; the role's line height never reaches the shaper")
	}
}
