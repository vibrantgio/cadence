// Package breadcrumb provides the Cadence Breadcrumb pattern: a horizontal
// row of labels separated by chevron glyphs that indicate hierarchical
// location. The last segment renders in OnSurface (the current location);
// preceding segments render in OnSurfaceVariant and may invoke an OnClick
// callback to navigate.
//
// The package follows the Phase 4 Composition contract: Breadcrumb is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. Source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
package breadcrumb

import (
	"image"
	"image/color"

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
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
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

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The default
	// shaper is created once per subscription inside the rx.Defer scope, so
	// it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

// Breadcrumb returns an rx.Observable[layout.Widget] that emits a new
// widget whenever any consumed theme token changes. Click handlers fire
// for any item whose OnClick is non-nil; mirror the prism/button
// interaction model (widget.Clickable + semantic ops) per segment.
func Breadcrumb(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest3(t.Color, t.Spacing, t.Type),
			func(n rx.Tuple3[tokens.ColorTokens, tokens.SpacingScale, tokens.TypeScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, typ: n.Third}
			},
		)
	})

	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}
		clicks := make([]widget.Clickable, len(props.Items))

		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			return func(gtx layout.Context) layout.Dimensions {
				for i := range props.Items {
					if props.Items[i].OnClick != nil && clicks[i].Clicked(gtx) {
						props.Items[i].OnClick(gtx)
					}
				}
				return drawBreadcrumb(gtx, shaper, props.Items, clicks, tok.color, tok.spacing, tok.typ)
			}
		})
	})
}

// Render produces a layout.Widget for a breadcrumb with pre-resolved
// tokens. Intended for golden-image testing and static demonstrations;
// production code should use Breadcrumb.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return drawBreadcrumb(gtx, shaper, props.Items, nil, colors, sp, ts)
	}
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	typ     tokens.TypeScale
}

const chevronDp = 12

func drawBreadcrumb(
	gtx layout.Context,
	shaper *text.Shaper,
	items []Item,
	clicks []widget.Clickable,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
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
				layout.Rigid(chevronWidget(chevronDp, colors.OnSurfaceVariant)),
				layout.Rigid(pllayout.HSpacer(sp.S2)),
			)
		}
		children = append(children, layout.Rigid(segmentWidget(shaper, item, clickFor(clicks, i), fg, ts)))
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

// labelColor returns the foreground colour for the segment at index i in
// a breadcrumb of n items. The last segment uses OnSurface (current
// location); preceding segments use OnSurfaceVariant.
func labelColor(i, n int, colors tokens.ColorTokens) color.NRGBA {
	if i == n-1 {
		return colors.OnSurface
	}
	return colors.OnSurfaceVariant
}

func clickFor(clicks []widget.Clickable, i int) *widget.Clickable {
	if i >= len(clicks) {
		return nil
	}
	return &clicks[i]
}

func segmentWidget(shaper *text.Shaper, item Item, click *widget.Clickable, fg color.NRGBA, ts tokens.TypeScale) layout.Widget {
	label := labelWidget(shaper, item.Label, fg, ts)
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

func labelWidget(shaper *text.Shaper, label string, fg color.NRGBA, ts tokens.TypeScale) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: fg}.Add(gtx.Ops)
		material := mColor.Stop()
		wl := widget.Label{MaxLines: 1}
		return wl.Layout(gtx, shaper, font.Font{}, unit.Sp(ts.TitleSmall), label, material)
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
