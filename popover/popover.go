// Package popover provides the Cadence Popover pattern: an anchored
// elevated Surface placed adjacent to a caller-supplied anchor widget,
// with a small triangular tail glyph pointing at the anchor. Outside-
// click dismissal and popover-vs-popover arbitration are coordinated via
// prism/coordination — opening a second popover dismisses the first.
//
// The package follows the Phase 4 Composition contract: Popover is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. The source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// Open/close is instantaneous in this package; entrance/exit transitions
// are deferred to a later Pulse-integration goal. No collision-aware
// reflow — if the chosen Placement would clip the viewport, the surface
// clips. Automatic flip is deferred.
package popover

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

// Placement is the side of the anchor on which the popover surface sits.
type Placement int

const (
	Top Placement = iota
	Bottom
	Left
	Right
)

// outsideMargin is how far the outside-press absorber reaches beyond the
// caller's canvas on every side. The popover cannot know the window bounds
// from inside its canvas, so the margin is simply larger than any display.
const outsideMargin = unit.Dp(8192)

// Props configures a Popover. Anchor must be non-nil; Content may be nil
// (the surface renders as an empty rounded rectangle of minimum size).
// OnDismiss is invoked when (a) a pointer.Press lands outside both the
// anchor and surface bounds, or (b) another popover takes arbitration
// top. OnDismiss may be nil.
type Props struct {
	// Open emits true to show the popover and false to hide it. A nil
	// Open is treated as a constant false (popover never opens).
	Open rx.Observable[bool]

	Anchor    layout.Widget
	Content   layout.Widget
	Placement Placement
	OnDismiss func(gtx layout.Context)
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
}

// Popover returns an rx.Observable[layout.Widget] that emits a new widget
// whenever the theme or Open state changes. State (the arbitration id,
// event tags) persists across emissions in the rx.Defer scope.
func Popover(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	open := props.Open
	if open == nil {
		open = rx.Of(false)
	}
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest3(t.Color, t.Spacing, t.Radius),
			func(n rx.Tuple3[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, radius: n.Third}
			},
		)
	})
	inputs := rx.CombineLatest2(resolved, open)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		st := newState()
		return rx.Map(inputs, func(next rx.Tuple2[resolvedTokens, bool]) layout.Widget {
			tok, openNow := next.First, next.Second

			// Open transition: claim arbitration top.
			if openNow && !st.opened {
				setTop(st.id)
				st.opened = true
			}
			if !openNow && st.opened {
				clearTop(st.id)
				st.opened = false
			}

			return func(gtx layout.Context) layout.Dimensions {
				live := openNow && isTop(st.id)
				// Arbitration: another popover overtook us while we
				// remained open; fire OnDismiss so the caller flips Open.
				if openNow && !live {
					fire(gtx, props.OnDismiss)
				}
				return drawPopover(gtx, props, tok, st, openNow, live)
			}
		})
	})
}

// Render produces a layout.Widget for a popover with pre-resolved tokens
// and an explicit open flag. Intended for golden-image testing and static
// demonstrations; production code should use Popover. The returned widget
// performs no input handling: pass open=true to render the anchor +
// floating surface, open=false to render only the anchor.
func Render(
	props Props,
	open bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
) layout.Widget {
	tok := resolvedTokens{color: colors, spacing: sp, radius: rad}
	st := newState()
	return func(gtx layout.Context) layout.Dimensions {
		return drawPopover(gtx, props, tok, st, open, false)
	}
}

// popoverState holds the per-subscription arbitration id, the transition
// tracker, and the three event tags routed by processInput. One instance
// is owned by each Popover subscription (and by static Render invocations,
// where input handling is inert).
type popoverState struct {
	id     int64
	opened bool

	outsideTag int
	anchorTag  int
	surfaceTag int
}

func newState() *popoverState { return &popoverState{id: allocID()} }

// drawPopover lays out the anchor at the canvas centre, then — when open —
// the floating surface adjacent to the anchor per Placement plus a
// triangular tail glyph pointing at the anchor. When live, it also
// registers three event tags (outside, anchor, surface) and dispatches
// the events drained for them.
func drawPopover(
	gtx layout.Context,
	props Props,
	tok resolvedTokens,
	st *popoverState,
	openNow, live bool,
) layout.Dimensions {
	canvas := gtx.Constraints.Max
	r := gtx.Dp(unit.Dp(tok.radius.Md))
	pad := gtx.Dp(unit.Dp(tok.spacing.S3))
	gap := gtx.Dp(unit.Dp(tok.spacing.S2))
	tailH := gtx.Dp(unit.Dp(6))
	tailW := gtx.Dp(unit.Dp(12))

	// 1. Record the anchor into a macro to measure its dims; centre it
	//    in the canvas. The anchor's last-recorded layout rect is the
	//    basis for surface positioning math below.
	anchorMacro := op.Record(gtx.Ops)
	anchorGtx := gtx
	anchorGtx.Constraints = layout.Constraints{Max: canvas}
	var anchorDims layout.Dimensions
	if props.Anchor != nil {
		anchorDims = props.Anchor(anchorGtx)
	}
	anchorOps := anchorMacro.Stop()
	anchorPos := image.Pt((canvas.X-anchorDims.Size.X)/2, (canvas.Y-anchorDims.Size.Y)/2)
	anchorRect := image.Rectangle{Min: anchorPos, Max: anchorPos.Add(anchorDims.Size)}

	// 2. If open, record the content into a macro to measure its dims;
	//    surface = content + 2*pad in both axes (clamped to a min size).
	var (
		surfaceRect image.Rectangle
		contentOps  op.CallOp
		contentDims layout.Dimensions
	)
	if openNow {
		contentMacro := op.Record(gtx.Ops)
		contentGtx := gtx
		contentGtx.Constraints = layout.Constraints{Max: image.Pt(canvas.X/2, canvas.Y/2)}
		if props.Content != nil {
			contentDims = props.Content(contentGtx)
		}
		contentOps = contentMacro.Stop()

		surfW := contentDims.Size.X + 2*pad
		surfH := contentDims.Size.Y + 2*pad
		minW := gtx.Dp(unit.Dp(48))
		minH := gtx.Dp(unit.Dp(24))
		if surfW < minW {
			surfW = minW
		}
		if surfH < minH {
			surfH = minH
		}

		anchorMidX := (anchorRect.Min.X + anchorRect.Max.X) / 2
		anchorMidY := (anchorRect.Min.Y + anchorRect.Max.Y) / 2
		switch props.Placement {
		case Top:
			x := anchorMidX - surfW/2
			y := anchorRect.Min.Y - gap - surfH
			surfaceRect = image.Rect(x, y, x+surfW, y+surfH)
		case Bottom:
			x := anchorMidX - surfW/2
			y := anchorRect.Max.Y + gap
			surfaceRect = image.Rect(x, y, x+surfW, y+surfH)
		case Left:
			x := anchorRect.Min.X - gap - surfW
			y := anchorMidY - surfH/2
			surfaceRect = image.Rect(x, y, x+surfW, y+surfH)
		case Right:
			x := anchorRect.Max.X + gap
			y := anchorMidY - surfH/2
			surfaceRect = image.Rect(x, y, x+surfW, y+surfH)
		}
	}

	// 3. Outside-press absorber. The caller's canvas is often just the
	//    anchor's box (the popover-canvas coupling), so the absorber extends
	//    a wide margin beyond it on every side to catch presses anywhere in
	//    the window. Registered first so that anchor- and surface-clip tags
	//    (registered later) win for presses inside their own bounds.
	if live {
		margin := gtx.Dp(outsideMargin)
		outsideClip := clip.Rect{
			Min: image.Pt(-margin, -margin),
			Max: image.Pt(canvas.X+margin, canvas.Y+margin),
		}.Push(gtx.Ops)
		event.Op(gtx.Ops, &st.outsideTag)
		outsideClip.Pop()
	}

	// 4. Anchor: anchor-absorber tag, then the recorded anchor ops at the
	//    centred offset. The absorber catches presses on the anchor so
	//    they do not bubble to the outside-absorber and dismiss.
	{
		anchorOff := op.Offset(anchorPos).Push(gtx.Ops)
		if live {
			anchorClip := clip.Rect{Max: anchorDims.Size}.Push(gtx.Ops)
			event.Op(gtx.Ops, &st.anchorTag)
			anchorClip.Pop()
		}
		anchorOps.Add(gtx.Ops)
		anchorOff.Pop()
	}

	// 5. Surface + tail + content, only when open. The surface absorbs
	//    presses; the tail is a triangular path bridging the gap to the
	//    anchor, drawn in the surface fill colour.
	if openNow {
		surfOff := op.Offset(surfaceRect.Min).Push(gtx.Ops)
		surfRRect := clip.RRect{
			Rect: image.Rectangle{Max: surfaceRect.Size()},
			SE:   r, SW: r, NE: r, NW: r,
		}
		paint.FillShape(gtx.Ops, tok.color.Surface, surfRRect.Op(gtx.Ops))
		paint.FillShape(gtx.Ops, tok.color.Outline, clip.Stroke{
			Path:  surfRRect.Path(gtx.Ops),
			Width: float32(gtx.Dp(unit.Dp(1))),
		}.Op())
		if live {
			absorbClip := clip.Rect{Max: surfaceRect.Size()}.Push(gtx.Ops)
			event.Op(gtx.Ops, &st.surfaceTag)
			absorbClip.Pop()
		}
		contentOff := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
		contentOps.Add(gtx.Ops)
		contentOff.Pop()
		surfOff.Pop()

		drawTail(gtx, anchorRect, surfaceRect, props.Placement, tailW, tailH, tok.color.Surface)
	}

	if live {
		processInput(gtx, props, st)
	}

	return layout.Dimensions{Size: canvas}
}

// drawTail paints a triangle bridging the gap between the surface and the
// anchor, with its tip pointing at the anchor. The base of the triangle
// touches the surface edge facing the anchor; the tip touches the anchor
// edge facing the surface. Coordinates are canvas-absolute (no transform
// is on the stack when this is called).
func drawTail(gtx layout.Context, anchor, surface image.Rectangle, p Placement, w, h int, fill color.NRGBA) {
	fw := float32(w)
	fh := float32(h)
	var pts [3]f32.Point
	switch p {
	case Top:
		cx := float32((anchor.Min.X + anchor.Max.X) / 2)
		baseY := float32(surface.Max.Y)
		pts = [3]f32.Point{
			{X: cx - fw/2, Y: baseY},
			{X: cx + fw/2, Y: baseY},
			{X: cx, Y: baseY + fh},
		}
	case Bottom:
		cx := float32((anchor.Min.X + anchor.Max.X) / 2)
		baseY := float32(surface.Min.Y)
		pts = [3]f32.Point{
			{X: cx - fw/2, Y: baseY},
			{X: cx + fw/2, Y: baseY},
			{X: cx, Y: baseY - fh},
		}
	case Left:
		cy := float32((anchor.Min.Y + anchor.Max.Y) / 2)
		baseX := float32(surface.Max.X)
		pts = [3]f32.Point{
			{X: baseX, Y: cy - fw/2},
			{X: baseX, Y: cy + fw/2},
			{X: baseX + fh, Y: cy},
		}
	case Right:
		cy := float32((anchor.Min.Y + anchor.Max.Y) / 2)
		baseX := float32(surface.Min.X)
		pts = [3]f32.Point{
			{X: baseX, Y: cy - fw/2},
			{X: baseX, Y: cy + fw/2},
			{X: baseX - fh, Y: cy},
		}
	}
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(pts[0])
	path.LineTo(pts[1])
	path.LineTo(pts[2])
	path.Close()
	paint.FillShape(gtx.Ops, fill, clip.Outline{Path: path.End()}.Op())
}

// processInput drains the press events for this frame: anchor- and
// surface-tag presses are absorbed silently; outside-tag presses invoke
// OnDismiss.
func processInput(gtx layout.Context, props Props, st *popoverState) {
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &st.anchorTag, Kinds: pointer.Press}); !ok {
			break
		}
	}
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &st.surfaceTag, Kinds: pointer.Press}); !ok {
			break
		}
	}
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &st.outsideTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			fire(gtx, props.OnDismiss)
		}
	}
}

// fire invokes cb when cb is non-nil. Centralised so OnDismiss is never
// called against a nil pointer.
func fire(gtx layout.Context, cb func(gtx layout.Context)) {
	if cb != nil {
		cb(gtx)
	}
}
