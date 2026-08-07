package card_test

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/vibrantgio/cadence/card"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 280, 200
	// The card draws into its full constraints. For the elevated variant
	// we leave a 16-px margin so the shadow strip has room to extend
	// outside the card's perimeter and remain visible in the golden.
	marginPx = 16
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius. Anti-aliased rounded corners vary slightly
	// between GPU contexts, breaking determinism. Sharp edges still
	// exercise the fill colour, outline stroke, and shadow presence.
	sharpRadius = tokens.RadiusScale{}
)

// fillRect is a simple sharp-edged solid widget used as a slot stand-in
// wherever the case is about slot geometry rather than slot content.
func fillRect(c color.NRGBA, heightDp float32) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(heightDp))
		size := image.Pt(gtx.Constraints.Max.X, h)
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// defaultShaper returns the shaper every golden here draws with: the default
// typography's faces pinned, system fonts off, so the stored images are the
// same on every machine. A golden test pins its faces with
// DeterministicShaper; application code takes the fallback Shaper. See
// AGENTS.md.
func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return tokens.DefaultTypography.DeterministicShaper()
}

// textSlot returns a slot widget that draws s in the given role.
//
// Card is the one pattern here whose Props carries no Shaper, because it draws
// no text of its own: all three slots are caller-supplied widgets, so the
// typeface inside a card is settled by whoever builds them. This is that
// caller. Filling the slots with text rather than coloured bars is what makes
// the goldens show the slot stack absorbing real content — the S3 gaps between
// surviving slots, and whether anything is clipped at the card's inner edge.
//
// ASCII only, per F4.2 — no symbol reaches a stored image.
func textSlot(shaper *text.Shaper, style tokens.TextStyle, c color.NRGBA, maxLines int, s string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		m := op.Record(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		material := m.Stop()

		f := font.Font{Typeface: font.Typeface(style.Typeface)}
		if style.Weight != 0 {
			f.Weight = tokens.FontWeight(style.Weight)
		}
		l := widget.Label{MaxLines: maxLines}
		if style.LineHeight != 0 {
			l.LineHeight = unit.Sp(style.LineHeight)
			l.LineHeightScale = 1
		}
		gtx.Constraints.Min = image.Point{}
		return l.Layout(gtx, shaper, f, unit.Sp(style.Size), s, material)
	}
}

// slots returns the header / body / footer trio every card case draws, in the
// colours of the given token set: a title, a body long enough to wrap inside a
// 280 px card, and a footer line.
func slots(t *testing.T, c tokens.ColorTokens) (header, body, footer layout.Widget) {
	t.Helper()
	shaper := defaultShaper(t)
	typo := tokens.DefaultTypography
	return textSlot(shaper, typo.TitleMedium, c.Text, 1, "Density"),
		textSlot(shaper, typo.BodyMedium, c.Ramps.Neutral.Step(700), 3,
			"Comfortable and Compact set the control height and the padding around it."),
		textSlot(shaper, typo.LabelMedium, c.Primary, 1, "Read the token")
}

// scene renders w into a canvas-sized constraint. The optional margin
// leaves room around the widget for ornamental output (e.g., shadows
// extending outside the widget's nominal bounds).
func scene(w layout.Widget, margin int, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.UniformInset(unit.Dp(float32(margin))).Layout(gtx, w)
	}
}

// TestCardGolden records or diffs the four canonical card variants. The slots
// carry real text since F4.4b: light-header-only is then the assertion that a
// lone slot is not padded as though the other two were there but empty, which
// is legible in a way three coloured bars never made it.
func TestCardGolden(t *testing.T) {
	cases := []struct {
		name       string
		colors     tokens.ColorTokens
		headerOnly bool
		elevated   bool
		bg         color.NRGBA
		margin     int
	}{
		{
			name:   "light-normal",
			colors: tokens.DefaultLight,
			bg:     color.NRGBA{R: 240, G: 240, B: 240, A: 255},
			margin: 0,
		},
		{
			name:   "dark-normal",
			colors: tokens.DefaultDark,
			bg:     color.NRGBA{R: 20, G: 20, B: 20, A: 255},
			margin: 0,
		},
		{
			name:       "light-header-only",
			colors:     tokens.DefaultLight,
			headerOnly: true,
			bg:         color.NRGBA{R: 240, G: 240, B: 240, A: 255},
			margin:     0,
		},
		{
			name:     "light-elevated",
			colors:   tokens.DefaultLight,
			elevated: true,
			bg:       color.NRGBA{R: 240, G: 240, B: 240, A: 255},
			margin:   marginPx,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header, body, footer := slots(t, tc.colors)
			props := card.Props{Header: header, Body: body, Footer: footer, Elevated: tc.elevated}
			if tc.headerOnly {
				props = card.Props{Header: header}
			}
			w := card.Render(props, tc.colors, tokens.Spacing, sharpRadius)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.margin, tc.bg))
		})
	}
}

// TestCardElevatedDiffersFromOutlined confirms the elevated variant
// produces visibly different pixels from the outlined variant. Catches
// regressions where the Elevated flag silently no-ops.
func TestCardElevatedDiffersFromOutlined(t *testing.T) {
	// A flat bar, not text: the two renders must differ only in the card's
	// own surface treatment, and an identical slot in both is the cleanest
	// way to say so.
	header := fillRect(color.NRGBA{R: 60, G: 110, B: 200, A: 255}, 24)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}

	outlined := card.Render(card.Props{Header: header}, tokens.DefaultLight, tokens.Spacing, sharpRadius)
	elevated := card.Render(card.Props{Header: header, Elevated: true}, tokens.DefaultLight, tokens.Spacing, sharpRadius)

	imgOut := golden.Capture(t, canvasSize, scene(outlined, marginPx, bg))
	imgElev := golden.Capture(t, canvasSize, scene(elevated, marginPx, bg))
	if n := golden.PixelDiff(imgOut, imgElev); n == 0 {
		t.Error("elevated and outlined cards render identically; expected shadow/outline difference")
	}
}

// TestCardLightDarkDiffer confirms that swapping the colour token set
// changes the rendered output.
func TestCardLightDarkDiffer(t *testing.T) {
	header := fillRect(color.NRGBA{R: 60, G: 110, B: 200, A: 255}, 24)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 48)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	light := card.Render(card.Props{Header: header, Body: body}, tokens.DefaultLight, tokens.Spacing, sharpRadius)
	dark := card.Render(card.Props{Header: header, Body: body}, tokens.DefaultDark, tokens.Spacing, sharpRadius)

	imgLight := golden.Capture(t, canvasSize, scene(light, 0, bg))
	imgDark := golden.Capture(t, canvasSize, scene(dark, 0, bg))
	if n := golden.PixelDiff(imgLight, imgDark); n == 0 {
		t.Error("light and dark cards render identically; expected colour differences")
	}
}
