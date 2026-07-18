// Package shell provides the Cadence Shell pattern: a top-level
// application layout. Three variants are offered via Props.Layout —
// SidebarHeaderMain composes a leading sidebar, a top navbar, and a
// main content slot; SplitPane composes two slots separated by a
// draggable vertical divider; ThreeColumn composes a full-width top
// navbar, a leading sidebar, a main column, an optional resizable
// trailing aside, and an optional footer strip.
//
// Shell follows the Phase 4 Composition contract: it is a callable
// Go function consuming a Prism theme observable, returning a stream
// of layout.Widget. Source is intentionally short — copy it into
// your own app and modify as needed.
//
// The Sidebar slot accepts any rx.Observable[layout.Widget], so callers
// can supply a cadence/sidebar instance, a cadence/accordion-based
// column, or any other pre-built widget stream. The static Render path
// accepts a pre-built layout.Widget for the sidebar slot; Props.Sidebar
// is not consulted by Render.
package shell

import (
	"image"
	"image/color"

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

// Layout selects which composition Shell renders.
type Layout int

const (
	// SidebarHeaderMain renders a sidebar on the leading edge, a navbar
	// across the top of the remaining area, and a main content slot
	// below the navbar.
	SidebarHeaderMain Layout = iota
	// SplitPane renders Left and Right slots separated by a draggable
	// vertical divider whose position is governed by SplitRatio.
	SplitPane
	// ThreeColumn renders a navbar across the full width of the top
	// edge (unlike SidebarHeaderMain, where the sidebar claims the full
	// height and the navbar starts after it), then a leading sidebar, a
	// flexed main column, and a trailing aside column separated from
	// main by a draggable vertical divider, with an optional full-width
	// footer strip along the bottom. A nil Aside omits the trailing
	// column and its divider, degenerating into a header-first sidebar
	// layout; a nil Footer omits the bottom strip. Each column scrolls
	// (or not) on its own — the shell hands every slot its full height.
	ThreeColumn
)

// Props configures a Shell. Fields not used by the chosen Layout are
// ignored (e.g., Left/Right/SplitRatio are unused when Layout is
// SidebarHeaderMain).
type Props struct {
	Layout Layout

	// SidebarHeaderMain slots.
	//
	// Sidebar is the pre-built sidebar widget stream. Any
	// rx.Observable[layout.Widget] is accepted — pass sidebar.Sidebar(th,
	// sidebarProps) for the default cadence/sidebar, or any other widget
	// stream. A nil Sidebar renders an empty leading column.
	Sidebar rx.Observable[layout.Widget]
	Navbar  navbar.Props
	Main    layout.Widget

	// SplitPane slots.
	Left, Right layout.Widget

	// SplitRatio drives the position of the vertical divider as a
	// fraction in [0, 1]. A nil SplitRatio is treated as a constant 0.5.
	SplitRatio rx.Observable[float32]

	// OnSplitChange is invoked when the user drags the divider. The
	// value is the new ratio in [0, 1]. May be nil.
	OnSplitChange func(gtx layout.Context, ratio float32)

	// ThreeColumn slots. Sidebar, Navbar and Main are shared with
	// SidebarHeaderMain (see above).
	//
	// Aside is the trailing column widget stream — a comments panel, an
	// inspector, or any other contextual surface. A nil Aside omits the
	// column and its divider entirely.
	Aside rx.Observable[layout.Widget]

	// Footer is an optional full-width strip below the columns (a
	// status or transport bar). It is laid out at a fixed footerHDp
	// height; a nil Footer omits the strip.
	Footer layout.Widget

	// AsideWidth drives the width of the aside column as an absolute dp
	// value. Unlike SplitRatio, a window resize keeps the aside at its
	// width and lets the main column absorb the change — the right
	// behaviour for annotation and inspector panels. Values are clamped
	// to [minAsideDp, maxAsideDp]. A nil AsideWidth is treated as a
	// constant defaultAsideDp. External updates win only while the user
	// is not dragging the divider.
	AsideWidth rx.Observable[unit.Dp]

	// OnAsideResize is invoked when the user drags the aside divider.
	// The value is the new clamped width in dp. May be nil.
	OnAsideResize func(gtx layout.Context, width unit.Dp)
}

// Layout-affecting constants. The navbar and footer slots have fixed
// heights so the main area is deterministic; the divider has a fixed
// pixel width large enough to register a hit area on touch pointers.
// The aside column tracks an absolute dp width clamped to
// [minAsideDp, maxAsideDp].
const (
	navbarHDp      = 64
	footerHDp      = 48
	dividerDp      = 6
	minRatio       = 0.05
	maxRatio       = 0.95
	minAsideDp     = 160
	maxAsideDp     = 640
	defaultAsideDp = 320
)

// Shell returns an rx.Observable[layout.Widget] that emits a new
// widget whenever a consumed theme token, the SplitRatio observable,
// or a composed sub-widget changes. Sidebar and navbar event handling
// is delegated to the respective packages; Shell only owns the
// SplitPane divider's drag handler.
func Shell(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	switch props.Layout {
	case SplitPane:
		return splitPaneObservable(th, props)
	case ThreeColumn:
		return threeColumnObservable(th, props)
	default:
		return sidebarHeaderMainObservable(th, props)
	}
}

// Render produces a layout.Widget for a shell with pre-resolved tokens
// and no event processing. Intended for golden-image testing and
// static demonstrations; production code should use Shell. splitRatio
// is honoured by SplitPane; SidebarHeaderMain uses the supplied sidebarW
// directly (Props.Sidebar is not consulted). Pass nil sidebarW to render
// an empty sidebar column. A ThreeColumn Props renders without an aside
// column — use RenderThreeColumn to supply a pre-built aside widget.
func Render(
	shaper *text.Shaper,
	props Props,
	sidebarW layout.Widget,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
	splitRatio float32,
) layout.Widget {
	switch props.Layout {
	case SplitPane:
		return staticSplitPane(props.Left, props.Right, splitRatio, colors)
	case ThreeColumn:
		return RenderThreeColumn(shaper, props, sidebarW, nil, colors, sp, ts, defaultAsideDp)
	default:
		return staticSidebarHeaderMain(sidebarW, shaper, props, colors, sp, ts)
	}
}

// ---- SidebarHeaderMain ---------------------------------------------------

func sidebarHeaderMainObservable(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	sb := props.Sidebar
	if sb == nil {
		sb = rx.Of[layout.Widget](emptyWidget)
	}
	nb := navbar.Navbar(th, props.Navbar)
	combined := rx.CombineLatest2(sb, nb)
	return rx.Map(combined, func(next rx.Tuple2[layout.Widget, layout.Widget]) layout.Widget {
		sbW, nbW := next.First, next.Second
		main := props.Main
		return composeSidebarHeaderMain(sbW, nbW, main)
	})
}

func staticSidebarHeaderMain(
	sidebarW layout.Widget,
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	if sidebarW == nil {
		sidebarW = emptyWidget
	}
	nbW := navbar.Render(shaper, props.Navbar, colors, sp, ts)
	return composeSidebarHeaderMain(sidebarW, nbW, props.Main)
}

// composeSidebarHeaderMain stacks the three slots so that Tab focus
// traversal flows sidebar → navbar → main. Flex preserves child order
// in the op stream, which is the order Gio's focus group walks.
func composeSidebarHeaderMain(sb, nb, main layout.Widget) layout.Widget {
	if main == nil {
		main = emptyWidget
	}
	return func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		navH := gtx.Dp(unit.Dp(navbarHDp))
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(sb),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.Y = size.Y
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.Y = navH
						gtx.Constraints.Max.Y = navH
						return nb(gtx)
					}),
					layout.Flexed(1, main),
				)
			}),
		)
	}
}

// ---- SplitPane -----------------------------------------------------------

// dragState is captured once per subscription and survives all
// emissions for the lifetime of the Shell instance.
type dragState struct {
	tag      dragTag
	pressX   float32 // pointer X at press, in local coords of the divider
	startR   float32 // ratio at press
	active   bool
	current  float32 // last seen ratio (from observable or drag)
	lastEmit float32 // last ratio passed to OnSplitChange
	emitted  bool
}

// dragTag is a non-zero-size type so its address is a unique event
// tag for the divider's pointer hit area.
type dragTag struct{ _ byte }

func splitPaneObservable(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	ratioObs := props.SplitRatio
	if ratioObs == nil {
		ratioObs = rx.Of(float32(0.5))
	}
	colorObs := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[tokens.ColorTokens] {
		return t.Color
	})
	inputs := rx.CombineLatest2(colorObs, ratioObs)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		ds := &dragState{current: 0.5}
		return rx.Map(inputs, func(next rx.Tuple2[tokens.ColorTokens, float32]) layout.Widget {
			colors, r := next.First, next.Second
			// External ratio updates win when the user isn't actively
			// dragging — otherwise the displayed position would jump back
			// to whatever the caller most recently fed in mid-drag.
			if !ds.active {
				ds.current = clampRatio(r)
			}
			left := props.Left
			right := props.Right
			onChange := props.OnSplitChange
			return func(gtx layout.Context) layout.Dimensions {
				processDrag(gtx, ds, onChange)
				return drawSplitPane(gtx, ds.current, left, right, colors, ds)
			}
		})
	})
}

func staticSplitPane(left, right layout.Widget, ratio float32, colors tokens.ColorTokens) layout.Widget {
	r := clampRatio(ratio)
	return func(gtx layout.Context) layout.Dimensions {
		return drawSplitPane(gtx, r, left, right, colors, nil)
	}
}

func processDrag(gtx layout.Context, ds *dragState, onChange func(gtx layout.Context, ratio float32)) {
	totalW := float32(gtx.Constraints.Max.X)
	if totalW <= 0 {
		return
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
			ds.startR = ds.current
			ds.active = true
		case pointer.Drag:
			if !ds.active {
				continue
			}
			delta := pe.Position.X - ds.pressX
			r := clampRatio(ds.startR + delta/totalW)
			ds.current = r
			if onChange != nil && (!ds.emitted || ds.lastEmit != r) {
				ds.lastEmit = r
				ds.emitted = true
				onChange(gtx, r)
			}
		case pointer.Release, pointer.Cancel:
			ds.active = false
		}
	}
}

func drawSplitPane(
	gtx layout.Context,
	ratio float32,
	left, right layout.Widget,
	colors tokens.ColorTokens,
	ds *dragState,
) layout.Dimensions {
	size := gtx.Constraints.Max
	dividerW := gtx.Dp(unit.Dp(dividerDp))
	if dividerW < 1 {
		dividerW = 1
	}
	innerW := size.X - dividerW
	if innerW < 0 {
		innerW = 0
	}
	leftW := int(float32(innerW)*ratio + 0.5)
	if leftW < 0 {
		leftW = 0
	}
	if leftW > innerW {
		leftW = innerW
	}
	rightW := innerW - leftW

	// Background to make the divider visible even if Left/Right are nil.
	paint.FillShape(gtx.Ops, colors.Surface, clip.Rect{Max: size}.Op())

	// Left pane.
	if left != nil {
		st := op.Offset(image.Point{}).Push(gtx.Ops)
		lgtx := gtx
		lgtx.Constraints = layout.Exact(image.Pt(leftW, size.Y))
		left(lgtx)
		st.Pop()
	}

	// Divider.
	dividerRect := image.Rect(leftW, 0, leftW+dividerW, size.Y)
	paint.FillShape(gtx.Ops, dividerColor(colors), clip.Rect(dividerRect).Op())
	if ds != nil {
		area := clip.Rect(dividerRect).Push(gtx.Ops)
		event.Op(gtx.Ops, &ds.tag)
		pointer.CursorColResize.Add(gtx.Ops)
		area.Pop()
	}

	// Right pane.
	if right != nil {
		st := op.Offset(image.Pt(leftW+dividerW, 0)).Push(gtx.Ops)
		rgtx := gtx
		rgtx.Constraints = layout.Exact(image.Pt(rightW, size.Y))
		right(rgtx)
		st.Pop()
	}

	return layout.Dimensions{Size: size}
}

// dividerColor selects an Outline-ish tint that registers a non-trivial
// pixel delta against Surface on both light and dark schemes.
func dividerColor(c tokens.ColorTokens) color.NRGBA {
	return c.Outline
}

// ---- helpers -------------------------------------------------------------

func clampRatio(r float32) float32 {
	if r < minRatio {
		return minRatio
	}
	if r > maxRatio {
		return maxRatio
	}
	return r
}

func emptyWidget(gtx layout.Context) layout.Dimensions {
	return layout.Dimensions{Size: gtx.Constraints.Min}
}
