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
// variant matching prism/button's geometry (44 dp min height, S4 horizontal
// padding, Md corner radius). Click hit-testing is wired through
// widget.Clickable in Hero — Render is static and performs no event work.
package hero

import (
	"image"
	"image/color"

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

// minButtonHeight mirrors prism/button's 44 dp minimum interactive target so
// the locally-rendered Secondary outlined CTA aligns vertically with the
// Primary filled CTA produced by button.Render.
const minButtonHeight = unit.Dp(44)

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
	OnClick func()
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

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The default
	// shaper is created once per subscription inside the rx.Defer scope, so
	// it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	typ     tokens.TypeScale
}

// Hero returns an rx.Observable[layout.Widget] that emits a new widget
// whenever any consumed theme token changes. CTA click state survives
// across emissions: the widget.Clickable for each CTA is allocated once
// per subscription inside the rx.Defer scope.
func Hero(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
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
		var primaryClick, secondaryClick widget.Clickable

		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			return func(gtx layout.Context) layout.Dimensions {
				if props.PrimaryCTA != nil && props.PrimaryCTA.OnClick != nil && primaryClick.Clicked(gtx) {
					props.PrimaryCTA.OnClick()
				}
				if props.SecondaryCTA != nil && props.SecondaryCTA.OnClick != nil && secondaryClick.Clicked(gtx) {
					props.SecondaryCTA.OnClick()
				}
				return drawHero(gtx, shaper, props, tok, &primaryClick, &secondaryClick)
			}
		})
	})
}

// Render produces a layout.Widget for a hero with pre-resolved tokens.
// Intended for golden-image testing and static demonstrations; production
// code should use Hero. No event work is performed: the CTAs render as
// inert visuals.
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
		bg := tintColor(tok.color.Primary, tok.color.Surface)
		paint.FillShape(gtx.Ops, bg, pllayout.Pill(gtx.Ops, image.Rectangle{Max: image.Pt(w, h)}, rad))

		st := op.Offset(image.Pt(padH, padV)).Push(gtx.Ops)
		labelCall.Add(gtx.Ops)
		st.Pop()
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
}

// titleWidget renders the display-typography title in OnSurface.
func titleWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return textWidget(shaper, label, tok.color.OnSurface, unit.Sp(tok.typ.DisplaySmall), font.Font{Weight: font.SemiBold})
}

// subtitleWidget renders the body-large subtitle in OnSurfaceVariant.
func subtitleWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return textWidget(shaper, label, tok.color.OnSurfaceVariant, unit.Sp(tok.typ.BodyLarge), font.Font{})
}

func textWidget(shaper *text.Shaper, label string, fg color.NRGBA, size unit.Sp, f font.Font) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if label == "" {
			return layout.Dimensions{}
		}
		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: fg}.Add(gtx.Ops)
		material := mColor.Stop()
		wl := widget.Label{MaxLines: 2}
		return wl.Layout(gtx, shaper, f, size, label, material)
	}
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
	rendered := button.Render(shaper, label, tok.color, tok.spacing, tok.radius, tok.typ, button.RenderState{})
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
// outlined button. Geometry mirrors prism/button (44 dp min height, S4
// horizontal padding, Md corner radius) so the two CTAs line up; the fill
// is Surface and the perimeter carries a 1 dp Outline stroke.
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
	padH := gtx.Dp(unit.Dp(tok.spacing.S4))
	padV := gtx.Dp(unit.Dp(tok.spacing.S2))
	minH := gtx.Dp(minButtonHeight)
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
	wl := widget.Label{MaxLines: 1}
	labelDims := wl.Layout(labelGtx, shaper, font.Font{}, unit.Sp(tok.typ.LabelLarge), label, material)
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
	paint.FillShape(gtx.Ops, tok.color.Outline, clip.Stroke{Path: rrect.Path(gtx.Ops), Width: stroke}.Op())

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

// tintColor blends accent over base at ~12% alpha. Used for the eyebrow
// pill so the tag remains a low-contrast surface against the canvas while
// still legibly carrying Primary-coloured text.
func tintColor(accent, base color.NRGBA) color.NRGBA {
	const a = 0x1F
	af := float32(a) / 255
	return color.NRGBA{
		R: uint8(float32(accent.R)*af + float32(base.R)*(1-af)),
		G: uint8(float32(accent.G)*af + float32(base.G)*(1-af)),
		B: uint8(float32(accent.B)*af + float32(base.B)*(1-af)),
		A: 0xff,
	}
}
