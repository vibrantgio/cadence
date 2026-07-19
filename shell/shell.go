// Package shell provides the Cadence Shell pattern: a top-level
// application layout. Four variants are offered via Props.Layout —
// SidebarHeaderMain composes a leading sidebar, a top navbar, and a
// main content slot; SplitPane composes two slots separated by a
// draggable divider on either axis; ThreeColumn composes a full-width
// top navbar, a leading sidebar, a main column, an optional resizable
// trailing aside, and an optional footer strip; StackedPage composes a
// pinned full-width navbar over a shell-owned vertical scroll of page
// sections — the marketing-page shell.
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
	// StackedPage renders a navbar pinned across the full top edge and
	// the Sections slots stacked in a shell-owned vertical scroll
	// region below it, with Footer appended after the last section so
	// it scrolls with the content instead of pinning to the viewport.
	// This is the marketing-page shell: hero, feature, pricing and
	// testimonial sections slot in as Sections.
	StackedPage
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

	// SplitPane slots. Left is the leading pane and Right the trailing
	// pane; when SplitAxis is layout.Vertical, Left is the top pane and
	// Right the bottom pane.
	Left, Right layout.Widget

	// SplitAxis selects the axis along which Left and Right are
	// arranged. The zero value (layout.Horizontal) places them side by
	// side separated by a vertical divider; layout.Vertical stacks Left
	// above Right separated by a horizontal divider.
	SplitAxis layout.Axis

	// SplitRatio drives the position of the divider as a fraction in
	// [0, 1] along SplitAxis. A nil SplitRatio is treated as a
	// constant 0.5.
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

	// StackedPage slots. Navbar is shared with SidebarHeaderMain and
	// ThreeColumn; Footer is shared with ThreeColumn, but here it
	// scrolls with the content at its natural height instead of
	// pinning to the viewport at a fixed height.
	//
	// Sections are stacked top to bottom in a scroll region owned by
	// the shell. Each section spans the full page width and receives
	// an unbounded height, so it must return its natural height.
	// Sections own their internal max-width/centering — a full-bleed
	// background with a centered inner column composes naturally.
	Sections []layout.Widget
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
	case StackedPage:
		return stackedPageObservable(th, props)
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
// StackedPage renders fully from Props (Sections and Footer are plain
// widgets); sidebarW and splitRatio are ignored for it.
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
		return staticSplitPane(props.Left, props.Right, splitRatio, colors, props.SplitAxis)
	case ThreeColumn:
		return RenderThreeColumn(shaper, props, sidebarW, nil, colors, sp, ts, defaultAsideDp)
	case StackedPage:
		return staticStackedPage(shaper, props, colors, sp, ts)
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
	press    float32 // pointer main-axis position at press, in shell-local coords
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
			colors := next.First
			ext := clampRatio(next.Second)
			left := props.Left
			right := props.Right
			axis := props.SplitAxis
			onChange := props.OnSplitChange
			// applied defers the external-ratio hand-off to the widget:
			// dragState must only ever be touched on the frame goroutine.
			// This projector runs on the rx scheduler, so writing ds here
			// races with processDrag/drawSplitPane during layout.
			applied := false
			return func(gtx layout.Context) layout.Dimensions {
				// External ratio updates win when the user isn't actively
				// dragging — otherwise the displayed position would jump
				// back to whatever the caller most recently fed in
				// mid-drag. An emission arriving mid-drag is applied on
				// the first frame after release.
				if !applied && !ds.active {
					ds.current = ext
					applied = true
				}
				processDrag(gtx, ds, axis, onChange)
				return drawSplitPane(gtx, ds.current, left, right, colors, ds, axis)
			}
		})
	})
}

func staticSplitPane(left, right layout.Widget, ratio float32, colors tokens.ColorTokens, axis layout.Axis) layout.Widget {
	r := clampRatio(ratio)
	return func(gtx layout.Context) layout.Dimensions {
		return drawSplitPane(gtx, r, left, right, colors, nil, axis)
	}
}

func processDrag(gtx layout.Context, ds *dragState, axis layout.Axis, onChange func(gtx layout.Context, ratio float32)) {
	total := float32(axis.Convert(gtx.Constraints.Max).X)
	if total <= 0 {
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
			ds.press = axis.FConvert(pe.Position).X
			ds.startR = ds.current
			ds.active = true
		case pointer.Drag:
			if !ds.active {
				continue
			}
			delta := axis.FConvert(pe.Position).X - ds.press
			r := clampRatio(ds.startR + delta/total)
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

// drawSplitPane lays the panes along axis: for layout.Horizontal the
// panes sit side by side separated by a vertical divider line; for
// layout.Vertical they stack with a horizontal one. Geometry is
// computed in main-axis terms and mapped back through axis.Convert.
func drawSplitPane(
	gtx layout.Context,
	ratio float32,
	left, right layout.Widget,
	colors tokens.ColorTokens,
	ds *dragState,
	axis layout.Axis,
) layout.Dimensions {
	size := gtx.Constraints.Max
	total := axis.Convert(size).X
	cross := axis.Convert(size).Y
	dividerPx := gtx.Dp(unit.Dp(dividerDp))
	if dividerPx < 1 {
		dividerPx = 1
	}
	inner := total - dividerPx
	if inner < 0 {
		inner = 0
	}
	leftPx := int(float32(inner)*ratio + 0.5)
	if leftPx < 0 {
		leftPx = 0
	}
	if leftPx > inner {
		leftPx = inner
	}
	rightPx := inner - leftPx

	// Background to make the divider visible even if Left/Right are nil.
	paint.FillShape(gtx.Ops, colors.Surface, clip.Rect{Max: size}.Op())

	// Leading pane.
	if left != nil {
		st := op.Offset(image.Point{}).Push(gtx.Ops)
		lgtx := gtx
		lgtx.Constraints = layout.Exact(axis.Convert(image.Pt(leftPx, cross)))
		left(lgtx)
		st.Pop()
	}

	// Divider.
	dividerRect := image.Rectangle{
		Min: axis.Convert(image.Pt(leftPx, 0)),
		Max: axis.Convert(image.Pt(leftPx+dividerPx, cross)),
	}
	paint.FillShape(gtx.Ops, dividerColor(colors), clip.Rect(dividerRect).Op())
	if ds != nil {
		area := clip.Rect(dividerRect).Push(gtx.Ops)
		event.Op(gtx.Ops, &ds.tag)
		cursor := pointer.CursorColResize
		if axis == layout.Vertical {
			cursor = pointer.CursorRowResize
		}
		cursor.Add(gtx.Ops)
		area.Pop()
	}

	// Trailing pane.
	if right != nil {
		st := op.Offset(axis.Convert(image.Pt(leftPx+dividerPx, 0))).Push(gtx.Ops)
		rgtx := gtx
		rgtx.Constraints = layout.Exact(axis.Convert(image.Pt(rightPx, cross)))
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
