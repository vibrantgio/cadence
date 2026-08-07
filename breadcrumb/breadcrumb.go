// Package breadcrumb provides the Cadence Breadcrumb pattern: a horizontal
// row of labels separated by chevron glyphs that indicate hierarchical
// location. The last segment renders in Text (the current location);
// preceding segments render in the low-contrast neutral-700 step and may
// invoke an OnClick callback to navigate.
//
// The package follows the Phase 4 Composition contract: Breadcrumb is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. Source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// Colour and interactivity are decided independently and can disagree.
// The Text "current location" colour goes to the last Item by
// position, whatever its OnClick; a segment is clickable exactly when its
// own OnClick is non-nil. The conventional trail — every segment but the
// last carrying an OnClick — is the caller's to build, and nothing here
// enforces it, so a clickable final segment still renders as though it
// were where you already are.
//
// There is no overflow behaviour. Every Item renders, in one horizontal
// row, each label clamped to a single line; a trail deeper than its
// constraint is clipped rather than collapsed to a leading ellipsis. An
// empty Items renders to zero Dimensions.
package breadcrumb

import (
	"image"
	"image/color"

	"gioui.org/f32"
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
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
	"github.com/vibrantgio/spectrum/typeset"
)

// Item is one segment in the breadcrumb trail. OnClick may be nil, in which
// case the segment is treated as a non-interactive current location.
// Conventionally the last item carries OnClick == nil; the package does not
// enforce this — interactivity follows the OnClick field per item.
type Item struct {
	Label   string
	OnClick func(gtx layout.Context)
}

// Props configures a Breadcrumb. Items must contain at least one entry;
// an empty slice renders to zero-sized Dimensions.
type Props struct {
	Items []Item

	// Shaper is an explicit per-instance override of the text shaper. Leave
	// it nil in normal use: the breadcrumb then shapes its labels with the
	// theme's shaper (Typography.Shaper()), which is built once for the
	// process and shared by every component reading that typography — the
	// cache lives behind the Typography value, so it survives the copy this
	// component's map function makes of it (spectrum F5.1). Set it only when
	// this instance must shape with a different shaper than the theme
	// provides.
	//
	// A shaper is not safe to use from two goroutines; Gio lays the widget
	// forest out on the one goroutine that runs the event loop, which is
	// what makes sharing it correct. See spectrum/tokens.Typography.Shaper.
	Shaper *text.Shaper
}

// Breadcrumb returns an rx.Observable[layout.Widget] that emits a new
// widget whenever any consumed theme token changes. Click handlers fire
// for any item whose OnClick is non-nil; mirror the prism/button
// interaction model (widget.Clickable + semantic ops) per segment.
func Breadcrumb(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	// Flatten the nested theme observables into a concrete snapshot. The
	// typography emission supplies both the TitleSmall text style and the
	// theme's cached shaper (ADR-003: the theme owns the typeface).
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest3(t.Color, t.Spacing, t.Typography),
			func(n rx.Tuple3[tokens.ColorTokens, tokens.SpacingScale, tokens.Typography]) resolvedTokens {
				typ := n.Third
				return resolvedTokens{
					color:   n.First,
					spacing: n.Second,
					label:   typ.TitleSmall,
					shaper:  typ.Shaper(),
				}
			},
		)
	})

	return rx.Defer(func() rx.Observable[layout.Widget] {
		clicks := make([]widget.Clickable, len(props.Items))

		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			// Props.Shaper is an explicit override; the theme's shaper is
			// the default.
			shaper := props.Shaper
			if shaper == nil {
				shaper = tok.shaper
			}
			return func(gtx layout.Context) layout.Dimensions {
				for i := range props.Items {
					if props.Items[i].OnClick != nil && clicks[i].Clicked(gtx) {
						props.Items[i].OnClick(gtx)
					}
				}
				return drawBreadcrumb(gtx, shaper, props.Items, clicks, tok.color, tok.spacing, tok.label)
			}
		})
	})
}

// Render produces a layout.Widget for a breadcrumb with pre-resolved
// tokens. Intended for golden-image testing and static demonstrations;
// production code should use Breadcrumb, which reads the shaper and the
// same text style off the theme.
//
// label is the TitleSmall role's whole text style — typeface, weight,
// size and line height all reach the shaper, exactly as they do on the
// live path. Pass tokens.DefaultTypography.TitleSmall for the default
// desktop look. There is no density parameter: a breadcrumb trail is a
// text row, not a control.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	label tokens.TextStyle,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return drawBreadcrumb(gtx, shaper, props.Items, nil, colors, sp, label)
	}
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	label   tokens.TextStyle // the TitleSmall role: typeface, weight, size, line height
	shaper  *text.Shaper     // the theme's shaper; nil in the Render path
}

const chevronDp = 12

func drawBreadcrumb(
	gtx layout.Context,
	shaper *text.Shaper,
	items []Item,
	clicks []widget.Clickable,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	style tokens.TextStyle,
) layout.Dimensions {
	if len(items) == 0 {
		return layout.Dimensions{}
	}

	children := make([]layout.FlexChild, 0, 2*len(items)-1)
	for i, item := range items {
		fg := labelColor(i, len(items), colors)
		if i > 0 {
			children = append(children,
				layout.Rigid(pllayout.HSpacer(sp.S2)),
				layout.Rigid(chevronWidget(chevronDp, colors.Ramps.Neutral.Step(700))),
				layout.Rigid(pllayout.HSpacer(sp.S2)),
			)
		}
		children = append(children, layout.Rigid(segmentWidget(shaper, item, clickFor(clicks, i), fg, style)))
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

// labelColor returns the foreground colour for the segment at index i in
// a breadcrumb of n items. The last segment uses Text (current location);
// preceding segments use the low-contrast neutral-700 step.
func labelColor(i, n int, colors tokens.ColorTokens) color.NRGBA {
	if i == n-1 {
		return colors.Text
	}
	return colors.Ramps.Neutral.Step(700)
}

func clickFor(clicks []widget.Clickable, i int) *widget.Clickable {
	if i >= len(clicks) {
		return nil
	}
	return &clicks[i]
}

func segmentWidget(shaper *text.Shaper, item Item, click *widget.Clickable, fg color.NRGBA, style tokens.TextStyle) layout.Widget {
	label := labelWidget(shaper, item.Label, fg, style)
	if click == nil || item.OnClick == nil {
		return label
	}
	return func(gtx layout.Context) layout.Dimensions {
		return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			semantic.LabelOp(item.Label).Add(gtx.Ops)
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return label(gtx)
		})
	}
}

func labelWidget(shaper *text.Shaper, label string, fg color.NRGBA, style tokens.TextStyle) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: fg}.Add(gtx.Ops)
		material := mColor.Stop()
		// Shape with the TitleSmall role's typeface, weight, size and line
		// height. Zero fields (the legacy Render path synthesizes a
		// size-only style) fall back to the shaper's defaults.
		f := typeset.Font(style, font.Normal)
		wl := typeset.Label(style, 1)
		return typeset.Layout(gtx, shaper, wl, f, unit.Sp(style.Size), label, material)
	}
}

func chevronWidget(sizeDp float32, col color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		sz := gtx.Dp(unit.Dp(sizeDp))
		drawChevron(gtx, sz/2, sz/2, sz, col)
		return layout.Dimensions{Size: image.Pt(sz, sz)}
	}
}

// drawChevron paints a right-pointing filled triangle centred at (cx, cy)
// fitting within an sz × sz square. The apex points along +X — i.e., in the
// reading direction of the breadcrumb trail (parent → child).
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
