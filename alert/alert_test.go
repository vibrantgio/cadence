package alert_test

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/cadence/alert"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 320, 96
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

// variantTitle is the Title each variant carries. Alert draws its title in
// the TitleMedium role and omits the title row entirely when Title is empty —
// which every golden here did until F4.4b, so the row itself was unrecorded.
// F4.2 pinned the faces by configuration and F4.3 moved every golden onto
// DeterministicShaper, so Latin text in Roboto rasterises identically on every
// machine. ASCII only, per F4.2 — no symbol reaches a stored image.
func variantTitle(v alert.Variant) string {
	switch v {
	case alert.Success:
		return "Changes saved"
	case alert.Warning:
		return "Unsaved changes"
	case alert.Error:
		return "Could not save"
	default:
		return "Draft autosaved"
	}
}

// fillRect is a sharp-edged solid widget used as a Body stand-in: Body is an
// arbitrary caller-supplied widget, so a flat block keeps it a structural
// marker and leaves the alert's own typography — the title — to carry the
// text.
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

// TestAlertGolden records or diffs every variant × {light, dark} pair.
// The Measurable contract requires at minimum info-light, info-dark,
// warning-light, error-light; the full 4×2 matrix is recorded so cross-
// variant regressions surface immediately.
func TestAlertGolden(t *testing.T) {
	shaper := defaultShaper(t)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 32)

	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	cases := []struct {
		name    string
		variant alert.Variant
		colors  tokens.ColorTokens
		bg      color.NRGBA
	}{
		{"info-light", alert.Info, tokens.DefaultLight, lightBG},
		{"info-dark", alert.Info, tokens.DefaultDark, darkBG},
		{"success-light", alert.Success, tokens.DefaultLight, lightBG},
		{"success-dark", alert.Success, tokens.DefaultDark, darkBG},
		{"warning-light", alert.Warning, tokens.DefaultLight, lightBG},
		{"warning-dark", alert.Warning, tokens.DefaultDark, darkBG},
		{"error-light", alert.Error, tokens.DefaultLight, lightBG},
		{"error-dark", alert.Error, tokens.DefaultDark, darkBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := alert.Props{
				Variant: tc.variant,
				Title:   variantTitle(tc.variant),
				Body:    body,
				Shaper:  shaper,
			}
			w := alert.Render(shaper, props, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypography.TitleMedium)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestAlertVariantsDiffer confirms each variant produces visibly distinct
// pixels in the same theme. Catches regressions where the Variant flag
// silently no-ops.
func TestAlertVariantsDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 32)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}

	render := func(v alert.Variant) *image.RGBA {
		props := alert.Props{Variant: v, Title: variantTitle(v), Body: body, Shaper: shaper}
		w := alert.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.TitleMedium)
		return golden.Capture(t, canvasSize, scene(w, bg))
	}

	variants := []struct {
		name string
		v    alert.Variant
	}{
		{"info", alert.Info},
		{"success", alert.Success},
		{"warning", alert.Warning},
		{"error", alert.Error},
	}
	imgs := make([]*image.RGBA, len(variants))
	for i, v := range variants {
		imgs[i] = render(v.v)
	}
	for i := range variants {
		for j := i + 1; j < len(variants); j++ {
			if n := golden.PixelDiff(imgs[i], imgs[j]); n == 0 {
				t.Errorf("%s and %s render identically; expected variant-specific accent", variants[i].name, variants[j].name)
			}
		}
	}
}

// TestAlertLightDarkDiffer confirms swapping the colour token set changes
// the rendered output.
func TestAlertLightDarkDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 32)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	for _, v := range []alert.Variant{alert.Info, alert.Success, alert.Warning, alert.Error} {
		propsL := alert.Props{Variant: v, Title: variantTitle(v), Body: body, Shaper: shaper}
		propsD := alert.Props{Variant: v, Title: variantTitle(v), Body: body, Shaper: shaper}
		light := alert.Render(shaper, propsL, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.TitleMedium)
		dark := alert.Render(shaper, propsD, tokens.DefaultDark, tokens.Spacing, sharpRadius, tokens.DefaultTypography.TitleMedium)

		imgLight := golden.Capture(t, canvasSize, scene(light, bg))
		imgDark := golden.Capture(t, canvasSize, scene(dark, bg))
		if n := golden.PixelDiff(imgLight, imgDark); n == 0 {
			t.Errorf("variant %v: light and dark render identically; expected colour differences", v)
		}
	}
}
