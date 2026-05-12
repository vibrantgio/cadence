// Package navbar provides the Cadence Navbar pattern: a horizontal
// Surface bar with three slots — a leading Brand, a centred row of
// Links, and trailing Actions. The active link is marked with a
// Primary-coloured underline.
//
// The package follows the Phase 4 Composition contract: Navbar is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. Source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
package navbar

import (
	"image"

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

// Link is one entry in the navbar's link row. OnClick may be nil, in
// which case the link is treated as non-interactive and does not
// participate in focus traversal. Active selects the Primary-underline
// indicator and is independent of OnClick.
type Link struct {
	Label   string
	OnClick func()
	Active  bool
}

// Props configures a Navbar. Brand is optional (a nil Brand collapses
// the leading slot to zero width while preserving document order).
// Actions entries that are nil are filtered before layout. Links may
// be empty.
type Props struct {
	Brand   layout.Widget
	Links   []Link
	Actions []layout.Widget

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The
	// default shaper is created once per subscription inside the
	// rx.Defer scope, so it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

// Navbar returns an rx.Observable[layout.Widget] that emits a new
// widget whenever any consumed theme token changes. Click handlers
// fire for any Link whose OnClick is non-nil; interaction mirrors the
// prism/button model (widget.Clickable + semantic ops) per link.
func Navbar(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
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
		clicks := make([]widget.Clickable, len(props.Links))
		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			return func(gtx layout.Context) layout.Dimensions {
				for i := range props.Links {
					if props.Links[i].OnClick != nil && clicks[i].Clicked(gtx) {
						props.Links[i].OnClick()
					}
				}
				return drawNavbar(gtx, shaper, props, clicks, tok.color, tok.spacing, tok.typ)
			}
		})
	})
}

// Render produces a layout.Widget for a navbar with pre-resolved
// tokens and no event processing. Intended for golden-image testing
// and static demonstrations; production code should use Navbar.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return drawNavbar(gtx, shaper, props, nil, colors, sp, ts)
	}
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	typ     tokens.TypeScale
}

// underlineDp is the thickness of the Active-link Primary indicator.
const underlineDp = 2

func drawNavbar(gtx layout.Context, shaper *text.Shaper, props Props, clicks []widget.Clickable, colors tokens.ColorTokens, sp tokens.SpacingScale, ts tokens.TypeScale) layout.Dimensions {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, colors.Surface, clip.Rect{Max: size}.Op())

	layout.UniformInset(unit.Dp(sp.S4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(brandSlot(props.Brand)),
			layout.Flexed(1, emptyWidget),
			layout.Rigid(linksRow(shaper, props.Links, clicks, colors, sp, ts)),
			layout.Flexed(1, emptyWidget),
			layout.Rigid(actionsRow(props.Actions, sp)),
		)
	})

	return layout.Dimensions{Size: size}
}

func brandSlot(w layout.Widget) layout.Widget {
	if w == nil {
		return emptyWidget
	}
	return w
}

// emptyWidget reports the minimum-constraint size so a Flexed parent's
// allocated space is honoured for offset arithmetic. Returning a zero
// Dimensions breaks Flex placement: subsequent children are positioned
// as if no space were consumed.
func emptyWidget(gtx layout.Context) layout.Dimensions {
	return layout.Dimensions{Size: gtx.Constraints.Min}
}

func linksRow(shaper *text.Shaper, links []Link, clicks []widget.Clickable, colors tokens.ColorTokens, sp tokens.SpacingScale, ts tokens.TypeScale) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if len(links) == 0 {
			return layout.Dimensions{}
		}
		children := make([]layout.FlexChild, 0, 2*len(links)-1)
		for i, l := range links {
			if i > 0 {
				children = append(children, layout.Rigid(pllayout.HSpacer(sp.S2)))
			}
			children = append(children, layout.Rigid(linkWidget(shaper, l, clickFor(clicks, i), colors, sp, ts)))
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	}
}

func actionsRow(actions []layout.Widget, sp tokens.SpacingScale) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		first := true
		for _, a := range actions {
			if a == nil {
				continue
			}
			if !first {
				children = append(children, layout.Rigid(pllayout.HSpacer(sp.S2)))
			}
			children = append(children, layout.Rigid(a))
			first = false
		}
		if len(children) == 0 {
			return layout.Dimensions{}
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	}
}

func clickFor(clicks []widget.Clickable, i int) *widget.Clickable {
	if i >= len(clicks) {
		return nil
	}
	return &clicks[i]
}

// linkWidget renders a single link as a label centred inside
// (S3, S2) padding. The cell width is at least 2×S3 so the Active
// underline is visible even when the label rasterises to zero width
// (e.g., in deterministic empty-label golden tests).
func linkWidget(shaper *text.Shaper, l Link, click *widget.Clickable, colors tokens.ColorTokens, sp tokens.SpacingScale, ts tokens.TypeScale) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		inner := func(gtx layout.Context) layout.Dimensions {
			padH := gtx.Dp(unit.Dp(sp.S3))
			padV := gtx.Dp(unit.Dp(sp.S2))
			underlineH := gtx.Dp(unit.Dp(underlineDp))

			labelGtx := gtx
			labelGtx.Constraints.Min = image.Point{}
			labelGtx.Constraints.Max.X -= 2 * padH
			if labelGtx.Constraints.Max.X < 0 {
				labelGtx.Constraints.Max.X = 0
			}

			mColor := op.Record(gtx.Ops)
			paint.ColorOp{Color: colors.OnSurface}.Add(gtx.Ops)
			textMaterial := mColor.Stop()

			mLabel := op.Record(gtx.Ops)
			wl := widget.Label{MaxLines: 1}
			labelDims := wl.Layout(labelGtx, shaper, font.Font{}, unit.Sp(ts.LabelLarge), l.Label, textMaterial)
			labelCall := mLabel.Stop()

			cellW := labelDims.Size.X + 2*padH
			cellH := labelDims.Size.Y + 2*padV + underlineH

			st := op.Offset(image.Pt(padH, padV)).Push(gtx.Ops)
			labelCall.Add(gtx.Ops)
			st.Pop()

			if l.Active {
				underline := image.Rect(0, cellH-underlineH, cellW, cellH)
				paint.FillShape(gtx.Ops, colors.Primary, clip.Rect(underline).Op())
			}
			return layout.Dimensions{Size: image.Pt(cellW, cellH)}
		}

		if click == nil || l.OnClick == nil {
			return inner(gtx)
		}
		return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			semantic.LabelOp(l.Label).Add(gtx.Ops)
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return inner(gtx)
		})
	}
}
