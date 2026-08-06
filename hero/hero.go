// Package hero provides the Cadence Hero pattern: a marketing landing
// block with an optional eyebrow tag, a display Title, a Subtitle, an
// optional Visual slot, and an optional dual call-to-action pair.
//
// The package follows the Phase 4 Composition contract: Hero is a callable
// Go function consuming a Prism theme observable, returning a stream of
// layout.Widget. The source is intentionally short and free of opaque
// configuration — copy it into your own app and modify as needed.
//
// Layout: an S6 outer inset. When Visual is nil the content stacks in a
// single centered column; when Visual is non-nil the text column and the
// Visual occupy two equal-width columns separated by an S6 gutter.
//
// CTA visuals: the Primary CTA reuses prism/button's filled visual via
// button.Render; the Secondary CTA is rendered locally as an outlined
// variant matching prism/button's geometry (Density.ControlHeight tall,
// Density.PaddingX/PaddingY inside, Md corner radius). Click hit-testing is
// wired through widget.Clickable in Hero — Render is static and performs no
// event work.
package hero

import (
	"image"
	"image/color"

	"gioui.org/font"
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
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

// ctaIntrinsicWidth is the natural CTA cell width in dp. Both CTAs are
// constrained to this width so prism/button's "fill available Max" sizing
// produces a fixed-width filled button, and the locally-rendered outlined
// twin matches its footprint. Wider labels still grow the button because
// button.Render lifts btnW to label width + 2×padH when needed.
const ctaIntrinsicWidth = unit.Dp(120)

// CTA describes a hero call-to-action. Label populates the button label and
// also seeds the accessibility name; OnClick fires on activation.
type CTA struct {
	Label   string
	OnClick func(gtx layout.Context)
}

// Props configures a Hero. Any field may be its zero value; the layout
// adapts to the presence of each slot.
type Props struct {
	// Eyebrow is the optional small tag rendered above the Title. An empty
	// string omits the eyebrow row entirely.
	Eyebrow string

	Title    string
	Subtitle string

	// PrimaryCTA renders as a Primary-filled button; SecondaryCTA renders
	// as an outlined button. Either or both may be nil.
	PrimaryCTA   *CTA
	SecondaryCTA *CTA

	// Visual is an optional illustration slot. When nil the hero is a
	// centered single-column text block; when non-nil the layout splits
	// into two equal columns with text leading and the visual trailing.
	Visual layout.Widget

	// Shaper is an explicit per-instance override of the text shaper. Leave
	// it nil in normal use: the hero then shapes its eyebrow, title,
	// subtitle and CTA labels with the theme's shaper (Typography.Shaper()),
	// which is built once and cached inside the theme's Typography value.
	// Set it only when this instance must shape with a different shaper
	// than the theme provides.
	Shaper *text.Shaper
}

type resolvedTokens struct {
	color    tokens.ColorTokens
	spacing  tokens.SpacingScale
	radius   tokens.RadiusScale
	eyebrow  tokens.TextStyle // the LabelSmall role: typeface, weight, size, line height
	title    tokens.TextStyle // the DisplaySmall role
	subtitle tokens.TextStyle // the BodyLarge role
	label    tokens.TextStyle // the LabelLarge role (CTA labels)
	density  tokens.Density   // CTA control height and inner padding (E1.4)
	shaper   *text.Shaper     // the theme's shaper; nil in the Render path
}

// Hero returns an rx.Observable[layout.Widget] that emits a new widget
// whenever any consumed theme token changes. CTA click state survives
// across emissions: the widget.Clickable for each CTA is allocated once
// per subscription inside the rx.Defer scope.
func Hero(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	// Flatten the nested theme observables into a concrete snapshot. The
	// typography emission supplies the LabelSmall/DisplaySmall/BodyLarge/
	// LabelLarge text styles and the theme's cached shaper (ADR-003: the
	// theme owns the typeface); the density sizes both CTAs.
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest5(t.Color, t.Spacing, t.Radius, t.Typography, t.Density),
			func(n rx.Tuple5[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale, tokens.Typography, tokens.Density]) resolvedTokens {
				typ := n.Fourth
				return resolvedTokens{
					color:    n.First,
					spacing:  n.Second,
					radius:   n.Third,
					eyebrow:  typ.LabelSmall,
					title:    typ.DisplaySmall,
					subtitle: typ.BodyLarge,
					label:    typ.LabelLarge,
					density:  n.Fifth,
					shaper:   typ.Shaper(),
				}
			},
		)
	})

	return rx.Defer(func() rx.Observable[layout.Widget] {
		var primaryClick, secondaryClick widget.Clickable

		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			// Props.Shaper is an explicit override; the theme's shaper is
			// the default.
			shaper := props.Shaper
			if shaper == nil {
				shaper = tok.shaper
			}
			return func(gtx layout.Context) layout.Dimensions {
				if props.PrimaryCTA != nil && props.PrimaryCTA.OnClick != nil && primaryClick.Clicked(gtx) {
					props.PrimaryCTA.OnClick(gtx)
				}
				if props.SecondaryCTA != nil && props.SecondaryCTA.OnClick != nil && secondaryClick.Clicked(gtx) {
					props.SecondaryCTA.OnClick(gtx)
				}
				return drawHero(gtx, shaper, props, tok, &primaryClick, &secondaryClick)
			}
		})
	})
}

// Render produces a layout.Widget for a hero with pre-resolved tokens.
// Intended for golden-image testing and static demonstrations; production
// code should use Hero, which reads both of the parameters below off the
// theme. No event work is performed: the CTAs render as inert visuals.
//
// typo supplies the four roles the hero draws — LabelSmall for the
// eyebrow, DisplaySmall for the title, BodyLarge for the subtitle,
// LabelLarge for the CTA labels — whole, so typeface, weight and line
// height reach the shaper exactly as they do on the live path. A pattern
// that spends more than one role takes the whole tokens.Typography rather
// than a role's tokens.TextStyle each: the roles it picks stay its own
// business, as they are on the live path. d is the density both CTAs draw
// at — the filled one through prism/button, the outlined twin through the
// matching local geometry. Pass tokens.DefaultTypography and
// tokens.Comfortable for the default desktop look.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
	typo tokens.Typography,
	d tokens.Density,
) layout.Widget {
	tok := resolvedTokens{
		color:    colors,
		spacing:  sp,
		radius:   rad,
		eyebrow:  typo.LabelSmall,
		title:    typo.DisplaySmall,
		subtitle: typo.BodyLarge,
		label:    typo.LabelLarge,
		density:  d,
	}
	return func(gtx layout.Context) layout.Dimensions {
		return drawHero(gtx, shaper, props, tok, nil, nil)
	}
}

func drawHero(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	primaryClick, secondaryClick *widget.Clickable,
) layout.Dimensions {
	pad := unit.Dp(tok.spacing.S6)
	return layout.UniformInset(pad).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		textCol := textColumn(shaper, props, tok, primaryClick, secondaryClick)
		if props.Visual == nil {
			return textCol(gtx)
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, textCol),
			layout.Rigid(pllayout.HSpacer(tok.spacing.S6)),
			layout.Flexed(1, props.Visual),
		)
	})
}

// textColumn lays out the eyebrow, title, subtitle, and CTA row in a
// single vertical Flex with S3 gaps between adjacent non-nil children.
func textColumn(
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	primaryClick, secondaryClick *widget.Clickable,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		var ws []layout.Widget
		if props.Eyebrow != "" {
			ws = append(ws, eyebrowWidget(shaper, props.Eyebrow, tok))
		}
		ws = append(ws, titleWidget(shaper, props.Title, tok))
		ws = append(ws, subtitleWidget(shaper, props.Subtitle, tok))
		if cta := ctaRowWidget(shaper, props, tok, primaryClick, secondaryClick); cta != nil {
			ws = append(ws, cta)
		}
		gap := tok.spacing.S3
		spaced := make([]layout.Widget, 0, len(ws)*2-1)
		for i, w := range ws {
			if i > 0 {
				spaced = append(spaced, pllayout.VSpacer(gap))
			}
			spaced = append(spaced, w)
		}
		return pllayout.Col(gtx, spaced...)
	}
}

// eyebrowWidget renders a Primary-tinted pill containing the eyebrow label
// in Primary color. The pill background keeps the eyebrow visible even when
// the label rasterises to zero width (e.g., in deterministic empty-label
// golden tests).
func eyebrowWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		padH := gtx.Dp(unit.Dp(tok.spacing.S2))
		padV := gtx.Dp(unit.Dp(tok.spacing.S1))
		rad := gtx.Dp(unit.Dp(tok.radius.Full))

		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: tok.color.Primary}.Add(gtx.Ops)
		material := mColor.Stop()

		labelGtx := gtx
		labelGtx.Constraints.Min = image.Point{}
		mLabel := op.Record(gtx.Ops)
		wl := styleLabel(1, tok.eyebrow)
		labelDims := wl.Layout(labelGtx, shaper, styleFont(tok.eyebrow, font.SemiBold), unit.Sp(tok.eyebrow.Size), label, material)
		labelCall := mLabel.Stop()

		w := labelDims.Size.X + 2*padH
		h := labelDims.Size.Y + 2*padV
		if minW := 2 * padH; w < minW {
			w = minW
		}
		if minH := 2 * padV; h < minH {
			h = minH
		}
		// A Primary tinted fill at the card-tint depth (ADR-007: ramp
		// steps 100–300 are tinted fills): same lightness zone as the
		// Surface ground, carrying the primary hue at full ramp chroma
		// instead of the old 12% alpha blend.
		bg := tok.color.Ramps.Primary.Step(200)
		paint.FillShape(gtx.Ops, bg, pllayout.Pill(gtx.Ops, image.Rectangle{Max: image.Pt(w, h)}, rad))

		st := op.Offset(image.Pt(padH, padV)).Push(gtx.Ops)
		labelCall.Add(gtx.Ops)
		st.Pop()
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
}

// titleWidget renders the DisplaySmall-role title in Text. A zero
// style weight (the legacy Render path synthesizes size-only styles)
// falls back to SemiBold, matching the pre-Typography rendering.
func titleWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return textWidget(shaper, label, tok.color.Text, tok.title, font.SemiBold)
}

// subtitleWidget renders the BodyLarge-role subtitle in the low-contrast
// text step (neutral 700).
func subtitleWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return textWidget(shaper, label, tok.color.Ramps.Neutral.Step(700), tok.subtitle, font.Normal)
}

func textWidget(shaper *text.Shaper, label string, fg color.NRGBA, style tokens.TextStyle, fallbackWeight font.Weight) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if label == "" {
			return layout.Dimensions{}
		}
		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: fg}.Add(gtx.Ops)
		material := mColor.Stop()
		wl := styleLabel(2, style)
		return wl.Layout(gtx, shaper, styleFont(style, fallbackWeight), unit.Sp(style.Size), label, material)
	}
}

// styleFont builds the font.Font for style. The style's typeface is
// honoured; a zero style weight falls back to fallback, the
// pre-Typography hard-coded weight for the draw site.
func styleFont(style tokens.TextStyle, fallback font.Weight) font.Font {
	f := font.Font{Typeface: font.Typeface(style.Typeface), Weight: fallback}
	if style.Weight != 0 {
		f.Weight = tokens.FontWeight(style.Weight)
	}
	return f
}

// styleLabel builds a widget.Label honouring the style's line height; a
// zero line height stays at the shaper's default.
func styleLabel(maxLines int, style tokens.TextStyle) widget.Label {
	wl := widget.Label{MaxLines: maxLines}
	if style.LineHeight != 0 {
		wl.LineHeight = unit.Sp(style.LineHeight)
		wl.LineHeightScale = 1
	}
	return wl
}

// ctaRowWidget lays out the optional Primary/Secondary CTAs in a horizontal
// row with S3 gap. Returns nil when both CTAs are nil.
func ctaRowWidget(
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	primaryClick, secondaryClick *widget.Clickable,
) layout.Widget {
	if props.PrimaryCTA == nil && props.SecondaryCTA == nil {
		return nil
	}
	return func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		if props.PrimaryCTA != nil {
			children = append(children, layout.Rigid(primaryCTAWidget(shaper, props.PrimaryCTA.Label, tok, primaryClick)))
		}
		if props.PrimaryCTA != nil && props.SecondaryCTA != nil {
			children = append(children, layout.Rigid(pllayout.HSpacer(tok.spacing.S3)))
		}
		if props.SecondaryCTA != nil {
			children = append(children, layout.Rigid(secondaryCTAWidget(shaper, props.SecondaryCTA.Label, tok, secondaryClick)))
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	}
}

// primaryCTAWidget renders the Primary CTA as a prism/button filled visual,
// wrapped in widget.Clickable when a click target is provided. Sizing is
// intrinsic — the button shrinks to its label rather than filling the row.
func primaryCTAWidget(shaper *text.Shaper, label string, tok resolvedTokens, click *widget.Clickable) layout.Widget {
	rendered := button.Render(shaper, label, tok.color, tok.spacing, tok.radius, tok.label, tok.density, button.RenderState{})
	return func(gtx layout.Context) layout.Dimensions {
		cgtx := ctaGtx(gtx)
		if click == nil {
			return rendered(cgtx)
		}
		return click.Layout(cgtx, func(gtx layout.Context) layout.Dimensions {
			semantic.LabelOp(label).Add(gtx.Ops)
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return rendered(gtx)
		})
	}
}

// secondaryCTAWidget renders the Secondary CTA as a locally-painted
// outlined button. Geometry mirrors prism/button (Density.ControlHeight
// tall, Density.PaddingX/PaddingY inside, Md corner radius) so the two CTAs
// line up; the fill is Surface and the perimeter carries a 1 dp Outline
// stroke.
func secondaryCTAWidget(shaper *text.Shaper, label string, tok resolvedTokens, click *widget.Clickable) layout.Widget {
	draw := func(gtx layout.Context) layout.Dimensions {
		return drawOutlinedButton(gtx, shaper, label, tok)
	}
	return func(gtx layout.Context) layout.Dimensions {
		cgtx := ctaGtx(gtx)
		if click == nil {
			return draw(cgtx)
		}
		return click.Layout(cgtx, func(gtx layout.Context) layout.Dimensions {
			semantic.LabelOp(label).Add(gtx.Ops)
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return draw(gtx)
		})
	}
}

func drawOutlinedButton(gtx layout.Context, shaper *text.Shaper, label string, tok resolvedTokens) layout.Dimensions {
	// E1.4: mirror prism/button exactly — the drawn height is the density's
	// ControlHeight and the inner padding is its PaddingX/PaddingY. Before
	// F3.4 this was a hardcoded 44 dp, which had been prism/button's height
	// until E1.3 re-cut it; the twin had been 8 dp taller than the filled
	// CTA it is meant to line up with ever since.
	padH := gtx.Dp(unit.Dp(tok.density.PaddingX))
	padV := gtx.Dp(unit.Dp(tok.density.PaddingY))
	minH := gtx.Dp(unit.Dp(tok.density.ControlHeight))
	rad := gtx.Dp(unit.Dp(tok.radius.Md))
	stroke := float32(gtx.Dp(unit.Dp(1)))

	mColor := op.Record(gtx.Ops)
	paint.ColorOp{Color: tok.color.Primary}.Add(gtx.Ops)
	material := mColor.Stop()

	labelGtx := gtx
	labelGtx.Constraints.Min = image.Point{}
	maxLabelW := gtx.Constraints.Max.X - 2*padH
	if maxLabelW > 0 {
		labelGtx.Constraints.Max.X = maxLabelW
	}
	mLabel := op.Record(gtx.Ops)
	wl := styleLabel(1, tok.label)
	labelDims := wl.Layout(labelGtx, shaper, styleFont(tok.label, font.Normal), unit.Sp(tok.label.Size), label, material)
	labelCall := mLabel.Stop()

	w := labelDims.Size.X + 2*padH
	h := labelDims.Size.Y + 2*padV
	if h < minH {
		h = minH
	}
	if w < minH {
		w = minH
	}

	rrect := clip.RRect{Rect: image.Rectangle{Max: image.Pt(w, h)}, SE: rad, SW: rad, NE: rad, NW: rad}
	paint.FillShape(gtx.Ops, tok.color.Surface, rrect.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, tok.color.Ramps.Neutral.Step(500), clip.Stroke{Path: rrect.Path(gtx.Ops), Width: stroke}.Op())

	offX := (w - labelDims.Size.X) / 2
	offY := (h - labelDims.Size.Y) / 2
	st := op.Offset(image.Pt(offX, offY)).Push(gtx.Ops)
	labelCall.Add(gtx.Ops)
	st.Pop()
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// ctaGtx clamps a CTA cell to the package-wide intrinsic CTA width so the
// Primary filled CTA (which fills its Max.X) and the Secondary outlined
// CTA share a deterministic footprint inside the CTA row.
func ctaGtx(gtx layout.Context) layout.Context {
	w := gtx.Dp(ctaIntrinsicWidth)
	gtx.Constraints.Min = image.Point{}
	if w < gtx.Constraints.Max.X || gtx.Constraints.Max.X == 0 {
		gtx.Constraints.Max.X = w
	}
	return gtx
}
