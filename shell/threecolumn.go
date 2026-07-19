package shell

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
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

// asideDragState is captured once per subscription and survives all
// emissions for the lifetime of the Shell instance. The aside divider
// tracks an absolute width rather than a ratio: when the window
// resizes, the aside keeps its width and the main column absorbs the
// change.
type asideDragState struct {
	tag      dragTag
	pressX   float32 // pointer X at press, in shell-local coords
	startW   unit.Dp // aside width at press
	active   bool
	current  unit.Dp // last seen width (from observable or drag)
	lastEmit unit.Dp
	emitted  bool
}

func threeColumnObservable(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	sb := props.Sidebar
	if sb == nil {
		sb = rx.Of[layout.Widget](emptyWidget)
	}
	nb := navbar.Navbar(th, props.Navbar)
	colorObs := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[tokens.ColorTokens] {
		return t.Color
	})
	hasAside := props.Aside != nil
	aside := props.Aside
	if aside == nil {
		aside = rx.Of[layout.Widget](emptyWidget)
	}
	widthObs := props.AsideWidth
	if widthObs == nil {
		widthObs = rx.Of(unit.Dp(defaultAsideDp))
	}
	inputs := rx.CombineLatest5(colorObs, sb, nb, aside, widthObs)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		ds := &asideDragState{current: defaultAsideDp}
		return rx.Map(inputs, func(next rx.Tuple5[tokens.ColorTokens, layout.Widget, layout.Widget, layout.Widget, unit.Dp]) layout.Widget {
			colors, sbW, nbW, asW, wdp := next.First, next.Second, next.Third, next.Fourth, next.Fifth
			ext := clampAsideWidth(wdp)
			if asW == nil {
				asW = emptyWidget
			}
			main := props.Main
			footer := props.Footer
			onResize := props.OnAsideResize
			// applied defers the external-width hand-off to the widget:
			// asideDragState must only ever be touched on the frame
			// goroutine. This projector runs on the rx scheduler, so
			// writing ds here races with processAsideDrag and
			// drawThreeColumn during layout.
			applied := false
			return func(gtx layout.Context) layout.Dimensions {
				if hasAside {
					// External width updates win when the user isn't
					// actively dragging — otherwise the displayed width
					// would jump back to whatever the caller most recently
					// fed in mid-drag. An emission arriving mid-drag is
					// applied on the first frame after release.
					if !applied && !ds.active {
						ds.current = ext
						applied = true
					}
					processAsideDrag(gtx, ds, onResize)
				}
				return drawThreeColumn(gtx, nbW, sbW, main, asW, footer, ds.current, colors, ds, hasAside)
			}
		})
	})
}

// RenderThreeColumn produces a layout.Widget for a ThreeColumn shell
// with pre-resolved tokens and no event processing. Intended for
// golden-image testing and static demonstrations; production code
// should use Shell. sidebarW and asideW are pre-built widgets for the
// leading and trailing columns (Props.Sidebar and Props.Aside are not
// consulted); a nil sidebarW renders an empty leading column, and a
// nil asideW omits the aside column and its divider entirely.
func RenderThreeColumn(
	shaper *text.Shaper,
	props Props,
	sidebarW, asideW layout.Widget,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
	asideWidth unit.Dp,
) layout.Widget {
	if sidebarW == nil {
		sidebarW = emptyWidget
	}
	nbW := navbar.Render(shaper, props.Navbar, colors, sp, ts)
	hasAside := asideW != nil
	w := clampAsideWidth(asideWidth)
	return func(gtx layout.Context) layout.Dimensions {
		return drawThreeColumn(gtx, nbW, sidebarW, props.Main, asideW, props.Footer, w, colors, nil, hasAside)
	}
}

func processAsideDrag(gtx layout.Context, ds *asideDragState, onResize func(gtx layout.Context, width unit.Dp)) {
	scale := gtx.Metric.PxPerDp
	if scale <= 0 {
		scale = 1
	}
	for {
		e, ok := gtx.Event(pointer.Filter{
			Target: &ds.tag,
			Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel,
		})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			ds.pressX = pe.Position.X
			ds.startW = ds.current
			ds.active = true
		case pointer.Drag:
			if !ds.active {
				continue
			}
			// The aside sits trailing of the divider, so dragging right
			// shrinks it.
			delta := pe.Position.X - ds.pressX
			w := clampAsideWidth(ds.startW - unit.Dp(delta/scale))
			ds.current = w
			if onResize != nil && (!ds.emitted || ds.lastEmit != w) {
				ds.lastEmit = w
				ds.emitted = true
				onResize(gtx, w)
			}
		case pointer.Release, pointer.Cancel:
			ds.active = false
		}
	}
}

// drawThreeColumn lays out navbar, sidebar, main, divider+aside and
// footer in that op-stream order, so Tab focus traversal follows the
// visual reading order. Every column receives the full row height —
// scrolling belongs to slot content, not to the shell.
func drawThreeColumn(
	gtx layout.Context,
	nb, sb, main, aside, footer layout.Widget,
	asideDp unit.Dp,
	colors tokens.ColorTokens,
	ds *asideDragState, // nil disables the divider hit area (static path)
	hasAside bool,
) layout.Dimensions {
	size := gtx.Constraints.Max
	navH := gtx.Dp(unit.Dp(navbarHDp))
	if navH > size.Y {
		navH = size.Y
	}
	footH := 0
	if footer != nil {
		footH = gtx.Dp(unit.Dp(footerHDp))
	}
	rowH := size.Y - navH - footH
	if rowH < 0 {
		rowH = 0
	}

	// Background so the divider and empty slots read against Surface.
	paint.FillShape(gtx.Ops, colors.Surface, clip.Rect{Max: size}.Op())

	// Navbar spans the full width — unlike SidebarHeaderMain, where the
	// sidebar claims the full height and the navbar starts after it.
	ngtx := gtx
	ngtx.Constraints = layout.Exact(image.Pt(size.X, navH))
	nb(ngtx)

	// Sidebar sizes its own width and fills the row height.
	sbW := 0
	if rowH > 0 {
		st := op.Offset(image.Pt(0, navH)).Push(gtx.Ops)
		sgtx := gtx
		sgtx.Constraints = layout.Constraints{Max: image.Pt(size.X, rowH)}
		sbDims := sb(sgtx)
		st.Pop()
		sbW = sbDims.Size.X
		if sbW > size.X {
			sbW = size.X
		}
	}

	dividerW := 0
	asidePx := 0
	if hasAside {
		dividerW = gtx.Dp(unit.Dp(dividerDp))
		if dividerW < 1 {
			dividerW = 1
		}
		asidePx = gtx.Dp(asideDp)
		avail := size.X - sbW - dividerW
		if avail < 0 {
			avail = 0
		}
		if asidePx > avail {
			asidePx = avail
		}
	}
	mainW := size.X - sbW - dividerW - asidePx
	if mainW < 0 {
		mainW = 0
	}

	// Main.
	if main != nil && rowH > 0 {
		st := op.Offset(image.Pt(sbW, navH)).Push(gtx.Ops)
		mgtx := gtx
		mgtx.Constraints = layout.Exact(image.Pt(mainW, rowH))
		main(mgtx)
		st.Pop()
	}

	if hasAside {
		// Divider. Its hit area is registered in shell-local coordinates
		// (no offset transform pushed) so drag deltas are measured against
		// a stable origin even as the divider itself moves.
		dividerRect := image.Rect(sbW+mainW, navH, sbW+mainW+dividerW, navH+rowH)
		paint.FillShape(gtx.Ops, dividerColor(colors), clip.Rect(dividerRect).Op())
		if ds != nil {
			area := clip.Rect(dividerRect).Push(gtx.Ops)
			event.Op(gtx.Ops, &ds.tag)
			pointer.CursorColResize.Add(gtx.Ops)
			area.Pop()
		}

		// Aside.
		if rowH > 0 {
			st := op.Offset(image.Pt(sbW+mainW+dividerW, navH)).Push(gtx.Ops)
			agtx := gtx
			agtx.Constraints = layout.Exact(image.Pt(asidePx, rowH))
			aside(agtx)
			st.Pop()
		}
	}

	// Footer.
	if footer != nil && footH > 0 {
		st := op.Offset(image.Pt(0, navH+rowH)).Push(gtx.Ops)
		fgtx := gtx
		fgtx.Constraints = layout.Exact(image.Pt(size.X, footH))
		footer(fgtx)
		st.Pop()
	}

	return layout.Dimensions{Size: size}
}

func clampAsideWidth(w unit.Dp) unit.Dp {
	if w < minAsideDp {
		return minAsideDp
	}
	if w > maxAsideDp {
		return maxAsideDp
	}
	return w
}
