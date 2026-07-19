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
	// Combine the per-section streams into one []layout.Widget stream so
	// any section emission (typically a theme change) re-emits the shell.
	sectionObs := make([]rx.Observable[layout.Widget], len(props.Sections))
	for i, s := range props.Sections {
		if s == nil {
			s = rx.Of[layout.Widget](emptyWidget)
		}
		sectionObs[i] = s
	}
	sections := rx.Of([]layout.Widget(nil))
	if len(sectionObs) > 0 {
		sections = rx.CombineLatest(sectionObs...)
	}
	inputs := rx.CombineLatest3(colorObs, nb, sections)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		// The scroll position is captured once per subscription so it
		// survives re-emissions for the lifetime of the Shell instance.
		list := &layout.List{Axis: layout.Vertical}
		return rx.Map(inputs, func(next rx.Tuple3[tokens.ColorTokens, layout.Widget, []layout.Widget]) layout.Widget {
			colors, nbW, secW := next.First, next.Second, next.Third
			footer := props.Footer
			return func(gtx layout.Context) layout.Dimensions {
				return drawStackedPage(gtx, nbW, secW, footer, colors, list)
			}
		})
	})
}

// RenderStackedPage produces a layout.Widget for a StackedPage shell
// with pre-resolved tokens and no event processing. Intended for
// golden-image testing and static demonstrations; production code
// should use Shell. sections are pre-built widgets for the scroll
// region (Props.Sections is not consulted); Footer is taken from props
// and appended after the last section.
func RenderStackedPage(
	shaper *text.Shaper,
	props Props,
	sections []layout.Widget,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	nbW := navbar.Render(shaper, props.Navbar, colors, sp, ts)
	list := &layout.List{Axis: layout.Vertical}
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
			w := footer
			if i < len(sections) {
				w = sections[i]
			}
			if w == nil {
				return layout.Dimensions{}
			}
			return w(gtx)
		})
		st.Pop()
	}

	return layout.Dimensions{Size: size}
}
