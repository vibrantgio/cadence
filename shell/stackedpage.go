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
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
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
	densityObs := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[tokens.Density] {
		return t.Density
	})
	inputs := rx.CombineLatest4(colorObs, nb, sections, densityObs)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		// The scroll position is captured once per subscription so it
		// survives re-emissions for the lifetime of the Shell instance.
		list := &layout.List{Axis: layout.Vertical}
		return rx.Map(inputs, func(next rx.Tuple4[tokens.ColorTokens, layout.Widget, []layout.Widget, tokens.Density]) layout.Widget {
			colors, nbW, secW := next.First, next.Second, next.Third
			navH := navbarHeight(next.Fourth)
			footer := props.Footer
			maxW := props.ContentMaxWidth
			return func(gtx layout.Context) layout.Dimensions {
				return drawStackedPage(gtx, nbW, secW, footer, colors, maxW, list, navH)
			}
		})
	})
}

// RenderStackedPage produces a layout.Widget for a StackedPage shell
// with pre-resolved tokens and no event processing. Intended for
// golden-image testing and static demonstrations; production code
// should use Shell. sections are pre-built widgets for the scroll
// region (Props.Sections is not consulted); Footer and ContentMaxWidth
// are taken from props, with Footer appended after the last section.
//
// label is the LabelLarge role's whole text style, which the page spends
// on its navbar, and d is the density both the navbar and the navbar
// slot's pinned height derive from. Pass
// tokens.DefaultTypography.LabelLarge and tokens.Comfortable for the
// default desktop look; before F3.4 the static path was pinned to
// Comfortable with no way to say otherwise.
func RenderStackedPage(
	shaper *text.Shaper,
	props Props,
	sections []layout.Widget,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	label tokens.TextStyle,
	d tokens.Density,
) layout.Widget {
	nbW := navbar.Render(shaper, props.Navbar, colors, sp, label, d)
	list := &layout.List{Axis: layout.Vertical}
	footer := props.Footer
	maxW := props.ContentMaxWidth
	return func(gtx layout.Context) layout.Dimensions {
		return drawStackedPage(gtx, nbW, sections, footer, colors, maxW, list, navbarHeight(d))
	}
}

// drawStackedPage pins the navbar across the top and lays Sections
// (then Footer) in a vertical layout.List that owns scrolling and
// clips to the viewport. The list preserves child order in the op
// stream, so Tab focus traversal flows navbar → sections top to
// bottom → footer; offscreen sections are not laid out at all. A
// positive maxW clamps each scroll child to that width, centered on
// the page.
func drawStackedPage(
	gtx layout.Context,
	nb layout.Widget,
	sections []layout.Widget,
	footer layout.Widget,
	colors tokens.ColorTokens,
	maxW unit.Dp,
	list *layout.List,
	navbarH unit.Dp,
) layout.Dimensions {
	size := gtx.Constraints.Max
	navH := gtx.Dp(navbarH)
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
		contentW := size.X
		if px := gtx.Dp(maxW); maxW > 0 && px < contentW {
			contentW = px
		}
		margin := (size.X - contentW) / 2
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
			if margin == 0 {
				return w(gtx)
			}
			// Clamp the child to the content column and center it. The
			// returned dimensions claim the full page width so the
			// list's cross-axis geometry (and scrolling) is unchanged.
			cgtx := gtx
			cgtx.Constraints.Min.X = contentW
			cgtx.Constraints.Max.X = contentW
			off := op.Offset(image.Pt(margin, 0)).Push(gtx.Ops)
			dims := w(cgtx)
			off.Pop()
			return layout.Dimensions{
				Size:     image.Pt(size.X, dims.Size.Y),
				Baseline: dims.Baseline,
			}
		})
		st.Pop()
	}

	return layout.Dimensions{Size: size}
}
