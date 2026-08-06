// Package alert provides the Cadence Alert pattern: a tinted-Surface
// rounded banner with a leading variant icon, a Title, and an arbitrary
// Body widget. Variants are Info, Success, Warning, and Error.
//
// The package follows the Phase 4 Composition contract: Alert is a callable
// Go function consuming a Prism theme observable, returning a stream of
// layout.Widget. Source is intentionally short and free of opaque
// configuration — copy it into your own app and modify as needed.
//
// Two things about the variants are worth knowing before you theme them.
// Only Info and Error read a token role — Primary and Error. Success and
// Warning have no role in the token set yet, so they fall back to Tailwind
// green and amber shades defined in this file, chosen between light and
// dark by comparing the luminance of Surface against Text. A custom
// theme's colours therefore reach two of the four variants and not the
// other two. And all four draw the same right-pointing chevron glyph,
// differing only in colour; the per-variant icon set arrives with
// prism/icon.
//
// The banner fills the constraints it is given rather than shrinking to
// its content — it reports gtx.Constraints.Max as its size — so an Alert
// handed a full-height column takes the whole column. Give it a
// height-constrained slot.
package alert

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/reactivego/rx"
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

// Variant selects the alert's semantic palette.
type Variant int

const (
	Info Variant = iota
	Success
	Warning
	Error
)

// Props configures an Alert. Title may be empty (the title row is omitted);
// Body may be nil (only the icon and title render).
type Props struct {
	Variant Variant
	Title   string
	Body    layout.Widget

	// Shaper is an explicit per-instance override of the text shaper.
	// Leave it nil in normal use: the alert then shapes its title with
	// the theme's shaper (Typography.Shaper()), which is built once and
	// cached inside the theme's Typography value. Set it only when this
	// instance must shape with a different shaper than the theme provides.
	Shaper *text.Shaper
}

// Alert returns an rx.Observable[layout.Widget] that emits a new widget
// whenever any consumed theme token changes.
func Alert(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	// Flatten the nested theme observables into a concrete snapshot. The
	// typography emission supplies both the TitleMedium text style and the
	// theme's cached shaper (ADR-003: the theme owns the typeface).
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest4(t.Color, t.Spacing, t.Radius, t.Typography),
			func(n rx.Tuple4[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale, tokens.Typography]) resolvedTokens {
				typ := n.Fourth
				return resolvedTokens{
					color:   n.First,
					spacing: n.Second,
					radius:  n.Third,
					title:   typ.TitleMedium,
					shaper:  typ.Shaper(),
				}
			},
		)
	})
	return rx.Defer(func() rx.Observable[layout.Widget] {
		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			// Props.Shaper is an explicit override; the theme's shaper is
			// the default.
			shaper := props.Shaper
			if shaper == nil {
				shaper = tok.shaper
			}
			return func(gtx layout.Context) layout.Dimensions {
				return drawAlert(gtx, shaper, props, tok.color, tok.spacing, tok.radius, tok.title)
			}
		})
	})
}

// Render produces a layout.Widget for an alert with pre-resolved tokens.
// Intended for golden-image testing and static demonstrations; production
// code should use Alert, which reads the shaper and the same text style
// off the theme.
//
// title is the TitleMedium role's whole text style — typeface, weight,
// size and line height all reach the shaper, exactly as they do on the
// live path. Pass tokens.DefaultTypography.TitleMedium for the default
// desktop look. There is no density parameter: an alert is a surface
// sized by its content, not a control.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
	title tokens.TextStyle,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return drawAlert(gtx, shaper, props, colors, sp, rad, title)
	}
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	title   tokens.TextStyle // the TitleMedium role: typeface, weight, size, line height
	shaper  *text.Shaper     // the theme's shaper; nil in the Render path
}

const iconDp = 20

func drawAlert(gtx layout.Context, shaper *text.Shaper, props Props, colors tokens.ColorTokens, sp tokens.SpacingScale, rad tokens.RadiusScale, title tokens.TextStyle) layout.Dimensions {
	size := gtx.Constraints.Max
	r := gtx.Dp(unit.Dp(rad.Lg))

	accent := accentColor(props.Variant, colors)
	bg := tintSurface(colors.Surface, accent)

	rrect := clip.RRect{Rect: image.Rectangle{Max: size}, SE: r, SW: r, NE: r, NW: r}
	paint.FillShape(gtx.Ops, bg, rrect.Op(gtx.Ops))

	layout.UniformInset(unit.Dp(sp.S4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
			layout.Rigid(iconWidget(iconDp, accent)),
			layout.Rigid(pllayout.HSpacer(sp.S3)),
			layout.Flexed(1, contentColumn(shaper, props, colors, sp, title)),
		)
	})

	return layout.Dimensions{Size: size}
}

// iconWidget renders the variant icon — a right-pointing filled chevron —
// into a fixed sizeDp square. The richer per-variant icon set will arrive
// once prism/icon lands; until then all variants share the chevron shape
// and differentiate by colour.
func iconWidget(sizeDp float32, col color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		sz := gtx.Dp(unit.Dp(sizeDp))
		drawChevron(gtx, sz/2, sz/2, sz, col)
		return layout.Dimensions{Size: image.Pt(sz, sz)}
	}
}

func contentColumn(shaper *text.Shaper, props Props, colors tokens.ColorTokens, sp tokens.SpacingScale, title tokens.TextStyle) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		var ws []layout.Widget
		if props.Title != "" {
			ws = append(ws, titleWidget(shaper, props.Title, colors.Text, title))
		}
		if props.Body != nil {
			if len(ws) > 0 {
				ws = append(ws, pllayout.VSpacer(sp.S1))
			}
			ws = append(ws, props.Body)
		}
		if len(ws) == 0 {
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
		}
		return pllayout.Col(gtx, ws...)
	}
}

func titleWidget(shaper *text.Shaper, label string, fg color.NRGBA, style tokens.TextStyle) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: fg}.Add(gtx.Ops)
		material := mColor.Stop()
		// Shape with the TitleMedium role's typeface, weight, size and
		// line height. The legacy Render path synthesizes a size-only
		// style; its zero weight falls back to SemiBold so the title keeps
		// its pre-Typography emphasis against the body.
		f := font.Font{Typeface: font.Typeface(style.Typeface), Weight: font.SemiBold}
		if style.Weight != 0 {
			f.Weight = tokens.FontWeight(style.Weight)
		}
		wl := widget.Label{MaxLines: 1}
		if style.LineHeight != 0 {
			wl.LineHeight = unit.Sp(style.LineHeight)
			wl.LineHeightScale = 1
		}
		return wl.Layout(gtx, shaper, f, unit.Sp(style.Size), label, material)
	}
}

func drawChevron(gtx layout.Context, cx, cy, sz int, col color.NRGBA) {
	half := float32(sz) / 2
	quarter := float32(sz) / 4
	fcx := float32(cx)
	fcy := float32(cy)

	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(fcx-quarter, fcy-half))
	p.LineTo(f32.Pt(fcx+quarter, fcy))
	p.LineTo(f32.Pt(fcx-quarter, fcy+half))
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: p.End()}.Op())
}

// accentColor maps Variant to its accent colour. Info and Error read directly
// from token roles so they flip automatically with light/dark; Success and
// Warning fall back to locally-defined Tailwind palettes (no token role
// exists for those semantics yet).
func accentColor(v Variant, c tokens.ColorTokens) color.NRGBA {
	switch v {
	case Info:
		return c.Primary
	case Error:
		return c.Error
	case Success:
		return localAccent(c, green700, green400)
	case Warning:
		return localAccent(c, amber700, amber400)
	default:
		return c.Primary
	}
}

// Locally-defined Tailwind palettes for variants without a token role.
var (
	green700 = color.NRGBA{0x15, 0x80, 0x3d, 0xff}
	green400 = color.NRGBA{0x4a, 0xde, 0x80, 0xff}
	amber700 = color.NRGBA{0xb4, 0x54, 0x09, 0xff}
	amber400 = color.NRGBA{0xfb, 0xbf, 0x24, 0xff}
)

// localAccent picks the light- or dark-mode shade based on the relative
// luminance of Surface vs Text in the active token set.
func localAccent(c tokens.ColorTokens, lightShade, darkShade color.NRGBA) color.NRGBA {
	if isLightMode(c) {
		return lightShade
	}
	return darkShade
}

func isLightMode(c tokens.ColorTokens) bool {
	return luminance(c.Surface) > luminance(c.Text)
}

func luminance(c color.NRGBA) int {
	return int(c.R) + int(c.G) + int(c.B)
}

// tintSurface overlays accent onto surface at ~12% alpha. The result has
// a soft variant tint while preserving Text legibility.
func tintSurface(surface, accent color.NRGBA) color.NRGBA {
	return blend(surface, accent, 0x1F)
}

func blend(base, over color.NRGBA, alpha uint8) color.NRGBA {
	a := float32(alpha) / 255
	return color.NRGBA{
		R: uint8(float32(over.R)*a + float32(base.R)*(1-a)),
		G: uint8(float32(over.G)*a + float32(base.G)*(1-a)),
		B: uint8(float32(over.B)*a + float32(base.B)*(1-a)),
		A: 0xff,
	}
}
