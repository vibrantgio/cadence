package testimonial_test

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/cadence/testimonial"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 720, 280
	// scene leaves an S5-equivalent margin around the testimonial so the
	// outer cards retain breathing room from the canvas edge.
	marginPx = 20
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius keeps the goldens deterministic — anti-aliased
	// rounded corners can vary slightly between GPU contexts, breaking
	// pixel-exact diffs.
	sharpRadius = tokens.RadiusScale{}
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

// scene renders w into a canvas-sized constraint over a flat background
// with a uniform margin so the testimonial does not touch the canvas edge.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.UniformInset(unit.Dp(float32(marginPx))).Layout(gtx, w)
	}
}

// testimonials are the three items in document order, so a grid reads as
// three distinct cards rather than three copies. Every field was blank until
// F4.4b, on the theory that font rasterisation was non-deterministic; F4.2
// pinned the faces by configuration and F4.3 moved every golden onto
// DeterministicShaper, so Latin text in Roboto rasterises identically on every
// machine. ASCII only, per F4.2 — no symbol reaches a stored image, and the
// decorative opening quote is a clip path the package draws itself.
//
// Filling AuthorName also lights up the avatar placeholder, which renders the
// name's first letter when AuthorAvatar is nil: with a blank name that circle
// held nothing, so the branch drew but never showed what it draws.
var testimonials = [3]testimonial.Item{
	{
		Quote:      "The tokens finally agree across every app we ship.",
		AuthorName: "Ada Fields",
		AuthorRole: "Design lead",
	},
	{
		Quote:      "We replaced three themes with one and lost nothing.",
		AuthorName: "Ben Ortiz",
		AuthorRole: "Engineer",
	},
	{
		Quote:      "Dark mode stopped being a separate project.",
		AuthorName: "Cleo Nam",
		AuthorRole: "Product",
	},
}

// items returns the first n testimonials.
func items(n int) []testimonial.Item {
	return append([]testimonial.Item(nil), testimonials[:n]...)
}

// TestTestimonialGolden records or diffs the four Measurable goldens.
func TestTestimonialGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	one := items(1)
	three := items(3)

	cases := []struct {
		name    string
		colors  tokens.ColorTokens
		bg      color.NRGBA
		variant testimonial.Variant
		items   []testimonial.Item
	}{
		{"light-single", tokens.DefaultLight, lightBG, testimonial.Single, one},
		{"dark-single", tokens.DefaultDark, darkBG, testimonial.Single, one},
		{"light-grid-three", tokens.DefaultLight, lightBG, testimonial.Grid, three},
		{"dark-grid-three", tokens.DefaultDark, darkBG, testimonial.Grid, three},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := testimonial.Props{Variant: tc.variant, Items: tc.items, Shaper: shaper}
			w := testimonial.Render(shaper, props, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypography)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestTestimonialVariantsDiffer confirms that swapping Variant between
// Single and Grid changes the rendered output.
func TestTestimonialVariantsDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}

	three := items(3)
	single := testimonial.Render(
		shaper,
		testimonial.Props{Variant: testimonial.Single, Items: three, Shaper: shaper},
		tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography,
	)
	grid := testimonial.Render(
		shaper,
		testimonial.Props{Variant: testimonial.Grid, Items: three, Shaper: shaper},
		tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography,
	)
	a := golden.Capture(t, canvasSize, scene(single, bg))
	b := golden.Capture(t, canvasSize, scene(grid, bg))
	if n := golden.PixelDiff(a, b); n == 0 {
		t.Error("Single and Grid testimonials render identically; expected layout differences")
	}
}

// TestTestimonialLightDarkDiffer confirms that swapping the colour token
// set changes the rendered output.
func TestTestimonialLightDarkDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	three := items(3)

	light := testimonial.Render(
		shaper,
		testimonial.Props{Variant: testimonial.Grid, Items: three, Shaper: shaper},
		tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography,
	)
	dark := testimonial.Render(
		shaper,
		testimonial.Props{Variant: testimonial.Grid, Items: three, Shaper: shaper},
		tokens.DefaultDark, tokens.Spacing, sharpRadius, tokens.DefaultTypography,
	)
	imgLight := golden.Capture(t, canvasSize, scene(light, bg))
	imgDark := golden.Capture(t, canvasSize, scene(dark, bg))
	if n := golden.PixelDiff(imgLight, imgDark); n == 0 {
		t.Error("light and dark testimonials render identically; expected colour differences")
	}
}
