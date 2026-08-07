package hero_test

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/cadence/hero"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 480, 240
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius keeps the goldens deterministic — anti-aliased
	// rounded corners and the eyebrow pill's Full radius both vary slightly
	// between GPU contexts, breaking pixel-exact diffs.
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

// The hero's four text slots. They were empty — and the eyebrow a single
// space, which is how it got a pill with nothing in it — until F4.4b, on the
// theory that font rasterisation was non-deterministic; F4.2 pinned the faces
// by configuration and F4.3 moved every golden onto DeterministicShaper, so
// Latin text in Roboto rasterises identically on every machine. ASCII only,
// per F4.2 — no symbol reaches a stored image.
//
// The title is short on purpose: with a Visual the text column is half of a
// 480 px canvas, and DisplaySmall is the largest role in the scale, so a
// longer title would wrap to three lines and push the subtitle off the bottom.
const (
	heroEyebrow  = "Design system"
	heroTitle    = "Vibrant Gio"
	heroSubtitle = "One coherent system for native desktop apps."
	primaryCTA   = "Get started"
	secondaryCTA = "Read docs"
)

// Both CTA labels are short for a reason worth writing down, because it is
// the one thing filling these labels in revealed. ctaGtx clamps every CTA
// cell to ctaIntrinsicWidth (120 dp) so the filled and outlined twins share a
// footprint, and both button bodies then clamp the label to that width minus
// 2×PaddingX and lay it out MaxLines:1 — so a label wider than roughly
// 88 px is ellipsized, not grown into. "Read the docs" came out as
// "Read the do…". That contradicts ctaIntrinsicWidth's own doc comment,
// which promises "wider labels still grow the button"; the growth branch in
// prism/button can never fire, because the label was already clamped to the
// width being compared against. These two labels fit, so the goldens record
// the hero rather than the clamp.

// heroText returns the Props every case starts from: a title and a subtitle,
// no eyebrow, no CTAs, no visual.
func heroText(shaper *text.Shaper) hero.Props {
	return hero.Props{Title: heroTitle, Subtitle: heroSubtitle, Shaper: shaper}
}

// withVisual adds the illustration slot, which splits the hero into two equal
// columns with the text leading.
func withVisual(p hero.Props, visual layout.Widget) hero.Props {
	p.Visual = visual
	return p
}

// withEyebrowAndCTAs adds the eyebrow pill and both call-to-action buttons.
func withEyebrowAndCTAs(p hero.Props) hero.Props {
	p.Eyebrow = heroEyebrow
	p.PrimaryCTA = &hero.CTA{Label: primaryCTA}
	p.SecondaryCTA = &hero.CTA{Label: secondaryCTA}
	return p
}

// fillRect is a sharp-edged solid widget used as a Visual stand-in: Visual is
// a caller-supplied illustration slot, so a flat block keeps it a structural
// marker while the hero's own four roles carry the text.
func fillRect(c color.NRGBA, heightDp float32) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(heightDp))
		size := image.Pt(gtx.Constraints.Max.X, h)
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

// TestHeroGolden records or diffs the four Measurable goldens. The structural
// variations — Visual slot presence, eyebrow pill, dual CTA backgrounds —
// distinguish the cases; the four text roles (DisplaySmall title, BodyLarge
// subtitle, LabelSmall eyebrow, LabelLarge CTA labels) carry the typography.
func TestHeroGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}
	visual := fillRect(color.NRGBA{R: 60, G: 110, B: 200, A: 255}, 120)

	cases := []struct {
		name   string
		colors tokens.ColorTokens
		bg     color.NRGBA
		props  hero.Props
	}{
		{
			name:   "light-text-only",
			colors: tokens.DefaultLight,
			bg:     lightBG,
			props:  heroText(shaper),
		},
		{
			name:   "dark-text-only",
			colors: tokens.DefaultDark,
			bg:     darkBG,
			props:  heroText(shaper),
		},
		{
			name:   "light-with-visual",
			colors: tokens.DefaultLight,
			bg:     lightBG,
			props:  withVisual(heroText(shaper), visual),
		},
		{
			name:   "light-eyebrow-and-dual-cta",
			colors: tokens.DefaultLight,
			bg:     lightBG,
			props:  withEyebrowAndCTAs(heroText(shaper)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := hero.Render(shaper, tc.props, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypography, tokens.Comfortable)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestHeroVisualSlotShiftsLayout confirms that supplying a Visual moves the
// hero from a single-column layout into a two-column split — without a
// Visual, the right half of the canvas is empty; with a Visual the right
// half carries the Visual's pixels.
func TestHeroVisualSlotShiftsLayout(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	visual := fillRect(color.NRGBA{R: 60, G: 110, B: 200, A: 255}, 120)

	textOnly := hero.Render(shaper, heroText(shaper), tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography, tokens.Comfortable)
	split := hero.Render(shaper, withVisual(heroText(shaper), visual), tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography, tokens.Comfortable)

	imgA := golden.Capture(t, canvasSize, scene(textOnly, bg))
	imgB := golden.Capture(t, canvasSize, scene(split, bg))
	if n := golden.PixelDiff(imgA, imgB); n == 0 {
		t.Error("text-only and with-visual hero render identically; expected the Visual slot to introduce a two-column split")
	}
}

// TestHeroLightDarkDiffer confirms that swapping the colour token set
// changes the rendered output.
func TestHeroLightDarkDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	props := withEyebrowAndCTAs(heroText(shaper))
	light := hero.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography, tokens.Comfortable)
	dark := hero.Render(shaper, props, tokens.DefaultDark, tokens.Spacing, sharpRadius, tokens.DefaultTypography, tokens.Comfortable)

	imgLight := golden.Capture(t, canvasSize, scene(light, bg))
	imgDark := golden.Capture(t, canvasSize, scene(dark, bg))
	if n := golden.PixelDiff(imgLight, imgDark); n == 0 {
		t.Error("light and dark hero render identically; expected colour differences")
	}
}

// TestLongCTALabelGrowsTheButton pins ctaIntrinsicWidth as a floor rather than
// a cap, which is the third of the three false sizing claims F4.4/F4.4b found:
// its own doc promised that "wider labels still grow the button", and the
// clamp made that impossible. The cell pinned Max.X to 120 dp, prism/button
// clamped its MaxLines:1 label to 120 − 2×PaddingX, and the growth branch then
// compared the cell against a label already trimmed to fit inside it. "Read
// the docs" drew as "Read the do…" and nothing in the suite noticed.
//
// The measurement is the filled CTA's own pixels: the widest unbroken run of
// the Primary fill on any scanline is the button's width, since the button is
// the only Primary-filled block in a hero and sharpRadius keeps its corners
// square. A short label must sit at the 120 dp floor; a long one must be wider
// than the floor and wide enough for its label plus both paddings.
func TestLongCTALabelGrowsTheButton(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	fill := tokens.DefaultLight.SolidStateColor(tokens.RolePrimary, tokens.StateNormal)

	ctaWidth := func(label string) int {
		p := heroText(shaper)
		p.PrimaryCTA = &hero.CTA{Label: label}
		w := hero.Render(shaper, p, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography, tokens.Comfortable)
		img := golden.Capture(t, canvasSize, scene(w, bg))
		return widestRunOf(img, fill)
	}

	const floor = 120 // ctaIntrinsicWidth in px at the 1:1 metric capture uses

	short := ctaWidth("Go")
	if short < 0 {
		return // headless unavailable; capture called t.Skip
	}
	if short != floor {
		t.Errorf("short CTA drew %d px wide, want the %d px intrinsic floor", short, floor)
	}

	long := ctaWidth("Read the docs")
	if long <= floor {
		t.Errorf("CTA labelled %q drew %d px wide, still at or under the %d px floor: the label is being clipped to the cell instead of sizing it",
			"Read the docs", long, floor)
	}
	if long <= short {
		t.Errorf("a long CTA label (%d px) is no wider than a short one (%d px)", long, short)
	}
}

// widestRunOf returns the longest unbroken horizontal run of exactly c in img.
func widestRunOf(img *image.RGBA, c color.NRGBA) int {
	want := color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
	b := img.Bounds()
	best := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		run := 0
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.RGBAAt(x, y) == want {
				run++
				if run > best {
					best = run
				}
			} else {
				run = 0
			}
		}
	}
	return best
}
