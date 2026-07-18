package shell

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/navbar"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

func stackedPageObservable(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	nb := navbar.Navbar(th, props.Navbar)
	colorObs := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[tokens.ColorTokens] {
		return t.Color
	})
	inputs := rx.CombineLatest2(colorObs, nb)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		// The scroll position is captured once per subscription so it
		// survives re-emissions for the lifetime of the Shell instance.
		list := &layout.List{Axis: layout.Vertical}
		return rx.Map(inputs, func(next rx.Tuple2[tokens.ColorTokens, layout.Widget]) layout.Widget {
			colors, nbW := next.First, next.Second
			sections := props.Sections
			footer := props.Footer
			return func(gtx layout.Context) layout.Dimensions {
				return drawStackedPage(gtx, nbW, sections, footer, colors, list)
			}
		})
	})
}

func staticStackedPage(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	nbW := navbar.Render(shaper, props.Navbar, colors, sp, ts)
	list := &layout.List{Axis: layout.Vertical}
	sections := props.Sections
	footer := props.Footer
	return func(gtx layout.Context) layout.Dimensions {
		return drawStackedPage(gtx, nbW, sections, footer, colors, list)
	}
}

// drawStackedPage pins the navbar across the top and lays Sections
// (then Footer) in a vertical layout.List that owns scrolling and
// clips to the viewport. The list preserves child order in the op
// stream, so Tab focus traversal flows navbar → sections top to
// bottom → footer; offscreen sections are not laid out at all.
func drawStackedPage(
	gtx layout.Context,
	nb layout.Widget,
	sections []layout.Widget,
	footer layout.Widget,
	colors tokens.ColorTokens,
	list *layout.List,
) layout.Dimensions {
	size := gtx.Constraints.Max
	navH := gtx.Dp(unit.Dp(navbarHDp))
	if navH > size.Y {
		navH = size.Y
	}
	bodyH := size.Y - navH

	// Page ground behind content shorter than the viewport.
	paint.FillShape(gtx.Ops, colors.Background, clip.Rect{Max: size}.Op())

	// Navbar pinned across the full width; sections scroll beneath it.
	ngtx := gtx
	ngtx.Constraints = layout.Exact(image.Pt(size.X, navH))
	nb(ngtx)

	children := len(sections)
	if footer != nil {
		children++
	}
	if bodyH > 0 && children > 0 {
		st := op.Offset(image.Pt(0, navH)).Push(gtx.Ops)
		bgtx := gtx
		// Exact viewport constraints force every child to the full page
		// width; the list gives children an unbounded height.
		bgtx.Constraints = layout.Exact(image.Pt(size.X, bodyH))
		list.Layout(bgtx, children, func(gtx layout.Context, i int) layout.Dimensions {
			if i < len(sections) {
				return sections[i](gtx)
			}
			return footer(gtx)
		})
		st.Pop()
	}

	return layout.Dimensions{Size: size}
}
