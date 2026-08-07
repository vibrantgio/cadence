package tooltip_test

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/cadence/tooltip"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 320, 240
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius. Anti-aliased rounded corners vary slightly
	// between GPU contexts, breaking determinism.
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

// fixedRect is a sharp-edged solid widget with explicit width and height.
// Used as the Trigger stand-in so the hit rect is predictable and the
// goldens stay deterministic.
func fixedRect(c color.NRGBA, widthDp, heightDp float32) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(gtx.Dp(unit.Dp(widthDp)), gtx.Dp(unit.Dp(heightDp)))
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// scene renders w over a flat background sized to the constraints.
func scene(w layout.Widget, bg color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// ---- Golden tests ----

// TestTooltipGolden records or diffs the two Measurable goldens —
// light-shown-top and dark-shown-bottom. The trigger is a small solid
// rectangle and the surface contains a short label rendered in the
// theme's Surface colour against the Text-filled bubble.
//
// Tooltip is the one component here that F4.4b found already carrying real
// text: Text is part of its contract, so these goldens have rasterised glyphs
// since they were first recorded, and every case already passed Props.Shaper.
// What they take from F4.3 is the pinned face — before it, "Save" shaped with
// whatever the machine had, and a flake here was indistinguishable from a
// missing font.
func TestTooltipGolden(t *testing.T) {
	shaper := defaultShaper(t)
	trigger := fixedRect(color.NRGBA{R: 80, G: 160, B: 220, A: 255}, 60, 28)

	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	cases := []struct {
		name      string
		placement tooltip.Placement
		colors    tokens.ColorTokens
		bg        color.NRGBA
	}{
		{"light-shown-top", tooltip.Top, tokens.DefaultLight, lightBG},
		{"dark-shown-bottom", tooltip.Bottom, tokens.DefaultDark, darkBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := tooltip.Props{
				Text:      "Save",
				Trigger:   trigger,
				Placement: tc.placement,
				Shaper:    shaper,
			}
			w := tooltip.Render(shaper, props, true, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelSmall)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestTooltipShownAndHiddenDiffer confirms that flipping the shown flag
// changes the rendered output. Catches regressions where the shown
// branch silently no-ops.
func TestTooltipShownAndHiddenDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	trigger := fixedRect(color.NRGBA{R: 80, G: 160, B: 220, A: 255}, 60, 28)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	props := tooltip.Props{Text: "Save", Trigger: trigger, Placement: tooltip.Top, Shaper: shaper}

	shown := tooltip.Render(shaper, props, true, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelSmall)
	hidden := tooltip.Render(shaper, props, false, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelSmall)

	imgShown := golden.Capture(t, canvasSize, scene(shown, bg))
	imgHidden := golden.Capture(t, canvasSize, scene(hidden, bg))
	if n := golden.PixelDiff(imgShown, imgHidden); n == 0 {
		t.Error("shown and hidden tooltip render identically; expected the bubble + label to appear when shown")
	}
}
