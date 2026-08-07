package pagination_test

import (
	"context"
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/pagination"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 360, 48
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

// TestPaginationGolden records or diffs the three Measurable goldens.
//
// Pagination is the one component here that F4.4b had nothing to fill in: it
// has no caller-supplied label at all, and every cell already draws its own
// page number in the LabelLarge role, so these goldens have carried real
// glyphs since they were first recorded. What they take from F4.3 is the
// pinned face — before it, the digits shaped with whatever the machine had.
//
// A zero radius scale (sharp corners) keeps the cell edges deterministic. The
// distinguishing signal across goldens is which cell carries the Primary fill:
// page-1-of-5 highlights the first cell, page-3-of-5 the third, and the light
// and dark variants flip the palette.
func TestPaginationGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}
	sharpRadius := tokens.RadiusScale{}

	cases := []struct {
		name      string
		page      int
		pageCount int
		colors    tokens.ColorTokens
		bg        color.NRGBA
	}{
		{"light-page-1-of-5", 1, 5, tokens.DefaultLight, lightBG},
		{"light-page-3-of-5", 3, 5, tokens.DefaultLight, lightBG},
		{"dark-page-3-of-5", 3, 5, tokens.DefaultDark, darkBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := pagination.Props{Page: tc.page, PageCount: tc.pageCount, Shaper: shaper}
			w := pagination.Render(shaper, props, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestPaginationCurrentPagePositionDiffers confirms that moving the active
// page shifts the Primary-coloured cell to a different x position. Guards
// against a regression in which all cells render with identical styling.
func TestPaginationCurrentPagePositionDiffers(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	sharpRadius := tokens.RadiusScale{}

	render := func(page int) *image.RGBA {
		props := pagination.Props{Page: page, PageCount: 5, Shaper: shaper}
		w := pagination.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
		return golden.Capture(t, canvasSize, scene(w, bg))
	}

	one := render(1)
	three := render(3)
	if n := golden.PixelDiff(one, three); n == 0 {
		t.Error("page-1-of-5 and page-3-of-5 render identically; expected the Primary-coloured cell to move")
	}
}

// TestPaginationLightDarkDiffer confirms swapping the colour token set
// changes the rendered output.
func TestPaginationLightDarkDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	sharpRadius := tokens.RadiusScale{}

	props := pagination.Props{Page: 3, PageCount: 5, Shaper: shaper}
	light := pagination.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
	dark := pagination.Render(shaper, props, tokens.DefaultDark, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)

	imgLight := golden.Capture(t, canvasSize, scene(light, bg))
	imgDark := golden.Capture(t, canvasSize, scene(dark, bg))
	if n := golden.PixelDiff(imgLight, imgDark); n == 0 {
		t.Error("light and dark render identically; expected token-pair colour differences")
	}
}

// liveWidget subscribes to obs and returns its last emitted widget.
func liveWidget(t *testing.T, obs rx.Observable[layout.Widget]) layout.Widget {
	t.Helper()
	var w layout.Widget
	if err := obs.Subscribe(context.Background(), func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}).Wait(); err != nil {
		t.Fatalf("Pagination subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Pagination did not emit an initial widget")
	}
	return w
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

// TestPaginationCompactGolden records or diffs the compact-density golden
// through the LIVE pipeline (the static Render path is frozen at
// tokens.Comfortable): the page squares densify to the 28 dp
// ControlHeight and the chevron glyphs to the 16 dp icon size.
func TestPaginationCompactGolden(t *testing.T) {
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	props := pagination.Props{Page: 3, PageCount: 5, Shaper: defaultShaper(t)}
	w := liveWidget(t, pagination.Pagination(rx.Of(densityTheme(tokens.Compact)), props))
	golden.Render(t, "light-compact-page-3-of-5", canvasSize, scene(w, lightBG))
}
