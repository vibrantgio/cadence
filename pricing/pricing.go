// Package pricing provides the Cadence Pricing pattern: a horizontal row
// of tier cards with an optional emphasised tier, suitable for a
// marketing landing or onboarding screen.
//
// The package follows the Phase 4 Composition contract: Pricing is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. The source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// Layout: each Tier renders as a rounded Surface card with an S5 inset.
// Cards sit in an equal-width horizontal row separated by an S4 gutter,
// each containing — top to bottom — an optional "Popular" Primary chip
// (Highlighted tier only), the tier name in title typography, a price /
// cadence pair in display typography with the cadence muted, a vertical
// feature list with a leading checkmark glyph rendered from a clip.Path,
// and a footer CTA button reusing prism/button's filled visual. The
// Highlighted tier swaps the 1 dp Outline border for a 2 dp Primary
// border.
//
// No responsive breakpoint to stack tiers vertically is provided —
// adopting this pattern at narrow widths is left to the caller.
package pricing

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/pointer"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/prism/button"
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

// CTA describes a per-tier call-to-action. Label populates the button
// label and seeds the accessibility name; OnClick fires on activation.
type CTA struct {
	Label   string
	OnClick func(gtx layout.Context)
}

// Tier describes a single pricing card.
type Tier struct {
	// Name is the tier label rendered in title typography.
	Name string

	// Price is the prominent monetary string (e.g., "$29").
	Price string

	// Cadence is the muted suffix following Price (e.g., "/mo").
	Cadence string

	// Features is the vertical bullet list rendered under the price row.
	// Each entry gets a leading checkmark glyph.
	Features []string

	// CTA is the footer call-to-action button. May be nil to omit.
	CTA *CTA

	// Highlighted selects the emphasised tier: a 2 dp Primary border and
	// a small "Popular" chip rendered above the tier name.
	Highlighted bool
}

// Props configures a Pricing row.
type Props struct {
	// Tiers is the ordered list of tier cards. Length 0 renders an empty
	// row; length 1 renders a single full-width card.
	Tiers []Tier

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The
	// default shaper is created once per subscription inside the rx.Defer
	// scope, so it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	typ     tokens.TypeScale
}

// Pricing returns an rx.Observable[layout.Widget] that emits a new
// widget whenever any consumed theme token changes. CTA click state
// survives across emissions: one widget.Clickable per tier is allocated
// once per subscription inside the rx.Defer scope.
func Pricing(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest4(t.Color, t.Spacing, t.Radius, t.Type),
			func(n rx.Tuple4[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale, tokens.TypeScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, radius: n.Third, typ: n.Fourth}
			},
		)
	})

	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}
		clicks := make([]widget.Clickable, len(props.Tiers))

		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			return func(gtx layout.Context) layout.Dimensions {
				for i := range props.Tiers {
					tier := &props.Tiers[i]
					if tier.CTA != nil && tier.CTA.OnClick != nil && clicks[i].Clicked(gtx) {
						tier.CTA.OnClick(gtx)
					}
				}
				return drawPricing(gtx, shaper, props, tok, clicks)
			}
		})
	})
}

// Render produces a layout.Widget for a pricing row with pre-resolved
// tokens. Intended for golden-image testing and static demonstrations;
// production code should use Pricing. No event work is performed: the
// CTAs render as inert visuals.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
	ts tokens.TypeScale,
) layout.Widget {
	tok := resolvedTokens{color: colors, spacing: sp, radius: rad, typ: ts}
	return func(gtx layout.Context) layout.Dimensions {
		return drawPricing(gtx, shaper, props, tok, nil)
	}
}

func drawPricing(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	clicks []widget.Clickable,
) layout.Dimensions {
	if len(props.Tiers) == 0 {
		return layout.Dimensions{}
	}
	gap := pllayout.HSpacer(tok.spacing.S4)
	children := make([]layout.FlexChild, 0, 2*len(props.Tiers)-1)
	for i := range props.Tiers {
		if i > 0 {
			children = append(children, layout.Rigid(gap))
		}
		i := i
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			var click *widget.Clickable
			if i < len(clicks) {
				click = &clicks[i]
			}
			return drawTier(gtx, shaper, props.Tiers[i], tok, click)
		}))
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx, children...)
}

// drawTier draws a single tier card: a rounded Surface filled to its
// allocated width with content height matching the inner stack plus
// S5 padding on all sides. The border is 2 dp Primary when Highlighted,
// 1 dp Outline otherwise.
func drawTier(
	gtx layout.Context,
	shaper *text.Shaper,
	tier Tier,
	tok resolvedTokens,
	click *widget.Clickable,
) layout.Dimensions {
	pad := gtx.Dp(unit.Dp(tok.spacing.S5))
	width := gtx.Constraints.Max.X

	inner := gtx
	inner.Constraints.Min = image.Point{}
	inner.Constraints.Max.X = max(0, width-2*pad)
	inner.Constraints.Max.Y = math.MaxInt32

	macro := op.Record(gtx.Ops)
	innerDims := drawTierContent(inner, shaper, tier, tok, click)
	contentCall := macro.Stop()

	height := innerDims.Size.Y + 2*pad
	r := gtx.Dp(unit.Dp(tok.radius.Lg))
	rrect := clip.RRect{Rect: image.Rectangle{Max: image.Pt(width, height)}, SE: r, SW: r, NE: r, NW: r}

	paint.FillShape(gtx.Ops, tok.color.Surface, rrect.Op(gtx.Ops))

	strokeW := float32(gtx.Dp(unit.Dp(1)))
	strokeColor := tok.color.Outline
	if tier.Highlighted {
		strokeW = float32(gtx.Dp(unit.Dp(2)))
		strokeColor = tok.color.Primary
	}
	paint.FillShape(gtx.Ops, strokeColor, clip.Stroke{Path: rrect.Path(gtx.Ops), Width: strokeW}.Op())

	off := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
	contentCall.Add(gtx.Ops)
	off.Pop()

	return layout.Dimensions{Size: image.Pt(width, height)}
}

// drawTierContent stacks the tier's inner widgets top-to-bottom with
// S3 gaps between adjacent items.
func drawTierContent(
	gtx layout.Context,
	shaper *text.Shaper,
	tier Tier,
	tok resolvedTokens,
	click *widget.Clickable,
) layout.Dimensions {
	var ws []layout.Widget
	if tier.Highlighted {
		ws = append(ws, popularChipWidget(shaper, tok))
	}
	ws = append(ws, tierNameWidget(shaper, tier.Name, tok))
	ws = append(ws, priceRowWidget(shaper, tier.Price, tier.Cadence, tok))
	for _, f := range tier.Features {
		ws = append(ws, featureRowWidget(shaper, f, tok))
	}
	if tier.CTA != nil {
		ws = append(ws, ctaWidget(shaper, tier.CTA, tok, click))
	}

	gap := tok.spacing.S3
	spaced := make([]layout.Widget, 0, 2*len(ws)-1)
	for i, w := range ws {
		if i > 0 {
			spaced = append(spaced, pllayout.VSpacer(gap))
		}
		spaced = append(spaced, w)
	}
	return pllayout.Col(gtx, spaced...)
}

// popularChipWidget renders a Primary-filled pill containing "Popular"
// in OnPrimary, with S2 horizontal and S1 vertical padding and a Full
// corner radius. Sized to its label rather than filling the card width.
func popularChipWidget(shaper *text.Shaper, tok resolvedTokens) layout.Widget {
	const label = "Popular"
	return func(gtx layout.Context) layout.Dimensions {
		padH := gtx.Dp(unit.Dp(tok.spacing.S2))
		padV := gtx.Dp(unit.Dp(tok.spacing.S1))
		rad := gtx.Dp(unit.Dp(tok.radius.Full))

		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: tok.color.OnPrimary}.Add(gtx.Ops)
		material := mColor.Stop()

		labelGtx := gtx
		labelGtx.Constraints.Min = image.Point{}
		mLabel := op.Record(gtx.Ops)
		wl := widget.Label{MaxLines: 1}
		labelDims := wl.Layout(labelGtx, shaper, font.Font{Weight: font.SemiBold}, unit.Sp(tok.typ.LabelSmall), label, material)
		labelCall := mLabel.Stop()

		w := labelDims.Size.X + 2*padH
		h := labelDims.Size.Y + 2*padV
		if minW := 2 * padH; w < minW {
			w = minW
		}
		if minH := 2 * padV; h < minH {
			h = minH
		}
		paint.FillShape(gtx.Ops, tok.color.Primary, pllayout.Pill(gtx.Ops, image.Rectangle{Max: image.Pt(w, h)}, rad))

		st := op.Offset(image.Pt(padH, padV)).Push(gtx.Ops)
		labelCall.Add(gtx.Ops)
		st.Pop()
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
}

// tierNameWidget renders the tier name in TitleLarge SemiBold OnSurface.
func tierNameWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return textWidget(shaper, label, tok.color.OnSurface, unit.Sp(tok.typ.TitleLarge), font.Font{Weight: font.SemiBold})
}

// priceRowWidget renders the price (DisplaySmall OnSurface) followed by
// the muted cadence (BodyMedium OnSurfaceVariant) in a horizontal row
// with an S1 gap. Cross-axis Alignment.End approximates baseline
// alignment for the prominent price next to its smaller cadence suffix.
func priceRowWidget(shaper *text.Shaper, price, cadence string, tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		priceW := textWidget(shaper, price, tok.color.OnSurface, unit.Sp(tok.typ.DisplaySmall), font.Font{Weight: font.SemiBold})
		cadenceW := textWidget(shaper, cadence, tok.color.OnSurfaceVariant, unit.Sp(tok.typ.BodyMedium), font.Font{})
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
			layout.Rigid(priceW),
			layout.Rigid(pllayout.HSpacer(tok.spacing.S1)),
			layout.Rigid(cadenceW),
		)
	}
}

// featureRowWidget renders a single feature bullet: a Primary checkmark
// glyph followed by the feature label in BodyMedium OnSurface, joined
// by an S2 gap and centered vertically.
func featureRowWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(checkmarkWidget(tok)),
			layout.Rigid(pllayout.HSpacer(tok.spacing.S2)),
			layout.Rigid(textWidget(shaper, label, tok.color.OnSurface, unit.Sp(tok.typ.BodyMedium), font.Font{})),
		)
	}
}

// checkmarkWidget paints a small Primary-stroked check ("✓") inside an
// S4 box using a clip.Path. The path is a two-segment polyline traced
// over the box; the stroke width is 2 dp.
func checkmarkWidget(tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		box := gtx.Dp(unit.Dp(tok.spacing.S4))
		stroke := float32(gtx.Dp(unit.Dp(2)))
		s := float32(box)

		var path clip.Path
		path.Begin(gtx.Ops)
		path.MoveTo(f32.Pt(s*0.2, s*0.55))
		path.LineTo(f32.Pt(s*0.45, s*0.8))
		path.LineTo(f32.Pt(s*0.8, s*0.25))
		paint.FillShape(gtx.Ops, tok.color.Primary, clip.Stroke{
			Path:  path.End(),
			Width: stroke,
		}.Op())
		return layout.Dimensions{Size: image.Pt(box, box)}
	}
}

// ctaWidget renders the per-tier CTA as a prism/button filled visual,
// wrapped in widget.Clickable when a click target is provided. The
// button fills the card's inner width (prism/button's intrinsic
// "fill Max.X" sizing), giving the typical full-width pricing CTA.
func ctaWidget(shaper *text.Shaper, cta *CTA, tok resolvedTokens, click *widget.Clickable) layout.Widget {
	rendered := button.Render(shaper, cta.Label, tok.color, tok.spacing, tok.radius, tok.typ, button.RenderState{})
	return func(gtx layout.Context) layout.Dimensions {
		if click == nil {
			return rendered(gtx)
		}
		return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			semantic.LabelOp(cta.Label).Add(gtx.Ops)
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return rendered(gtx)
		})
	}
}

// textWidget renders a single-line widget.Label in the supplied colour,
// size, and font. Empty labels collapse to zero dimensions so adjacent
// section gaps are the only vertical contribution.
func textWidget(shaper *text.Shaper, label string, fg color.NRGBA, size unit.Sp, f font.Font) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if label == "" {
			return layout.Dimensions{}
		}
		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: fg}.Add(gtx.Ops)
		material := mColor.Stop()
		wl := widget.Label{MaxLines: 1}
		return wl.Layout(gtx, shaper, f, size, label, material)
	}
}
