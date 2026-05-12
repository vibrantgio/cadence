// Package tooltip provides the Cadence Tooltip pattern: a small hover/
// focus annotation rendered adjacent to a caller-supplied trigger after
// a short delay. Hover or focus exit hides the tooltip; opening another
// tooltip via prism/coordination arbitration hides the previous one so
// only one tooltip is visible across the window at a time.
//
// The package follows the Phase 4 Composition contract: Tooltip is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. The source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// The trigger renders at the canvas centre; the tooltip surface is placed
// adjacent per Placement. Show/hide is instantaneous in this package;
// entrance/exit transitions are deferred to a later Pulse-integration
// goal. Touch long-press is out of scope.
package tooltip

import (
	"image"
	"time"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

// DefaultDelay is the show-after-entry delay applied when Props.Delay is
// zero or negative.
const DefaultDelay = 500 * time.Millisecond

// Placement is the side of the trigger on which the tooltip surface sits.
type Placement int

const (
	Top Placement = iota
	Bottom
	Left
	Right
)

// Props configures a Tooltip. Trigger must be non-nil; Text may be empty
// (the surface still renders, but at minimum padded size).
type Props struct {
	Text      string
	Trigger   layout.Widget
	Delay     time.Duration
	Placement Placement

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The
	// default shaper is created once per subscription inside the
	// rx.Defer scope, so it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	typ     tokens.TypeScale
}

// Tooltip returns an rx.Observable[layout.Widget] that emits a new widget
// whenever the theme changes. State (the arbitration id, hover gesture,
// focus tag, entry-time stamp, default shaper) persists across emissions
// in the rx.Defer scope.
func Tooltip(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	delay := props.Delay
	if delay <= 0 {
		delay = DefaultDelay
	}
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest4(t.Color, t.Spacing, t.Radius, t.Type),
			func(n rx.Tuple4[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale, tokens.TypeScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, radius: n.Third, typ: n.Fourth}
			},
		)
	})
	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}
		st := newState()
		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			return func(gtx layout.Context) layout.Dimensions {
				return drawTooltip(gtx, shaper, props, delay, tok, st, true)
			}
		})
	})
}

// Render produces a layout.Widget for a tooltip with pre-resolved tokens
// and an explicit shown flag. Intended for golden-image testing and
// static demonstrations; production code should use Tooltip. The returned
// widget performs no input handling or arbitration: pass shown=true to
// render the trigger plus the floating surface, shown=false to render
// only the trigger.
func Render(
	shaper *text.Shaper,
	props Props,
	shown bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
	ts tokens.TypeScale,
) layout.Widget {
	tok := resolvedTokens{color: colors, spacing: sp, radius: rad, typ: ts}
	return func(gtx layout.Context) layout.Dimensions {
		return drawStatic(gtx, shaper, props, tok, shown)
	}
}

// tooltipState holds the per-subscription arbitration id, hover/focus
// trackers, entry-time stamp, and the shown-flag tracker. One instance
// is owned by each Tooltip subscription.
type tooltipState struct {
	id      int64
	shown   bool
	entryAt time.Time

	hov      gesture.Hover
	focusTag int
}

func newState() *tooltipState { return &tooltipState{id: allocID()} }

// drawTooltip runs the per-frame logic: process hover/focus events,
// update entry-time and arbitration state, paint the trigger, and (when
// shown) paint the floating surface.
func drawTooltip(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	delay time.Duration,
	tok resolvedTokens,
	st *tooltipState,
	live bool,
) layout.Dimensions {
	canvas := gtx.Constraints.Max

	// 1. Record the trigger into a macro to measure its dims; centre it
	//    on the canvas. The trigger's centred rect is the basis for both
	//    the hit area registered for hover/focus and the surface
	//    positioning math below.
	triggerMacro := op.Record(gtx.Ops)
	triggerGtx := gtx
	triggerGtx.Constraints = layout.Constraints{Max: canvas}
	var triggerDims layout.Dimensions
	if props.Trigger != nil {
		triggerDims = props.Trigger(triggerGtx)
	}
	triggerOps := triggerMacro.Stop()
	triggerPos := image.Pt((canvas.X-triggerDims.Size.X)/2, (canvas.Y-triggerDims.Size.Y)/2)
	triggerRect := image.Rectangle{Min: triggerPos, Max: triggerPos.Add(triggerDims.Size)}

	// 2. Drain hover and focus events. The drains must happen before the
	//    hit-area registration in step 4, so the gesture/focus trackers
	//    reflect this frame's state.
	var active bool
	if live {
		for {
			if _, ok := gtx.Event(key.FocusFilter{Target: &st.focusTag}); !ok {
				break
			}
		}
		hovered := st.hov.Update(gtx.Source)
		focused := gtx.Focused(&st.focusTag)
		active = hovered || focused
	}

	// 3. Entry-time and arbitration transitions.
	if live {
		switch {
		case active && st.entryAt.IsZero():
			// Hover/focus entry: record the entry time and schedule a
			// redraw at entry+delay so we wake to show even when no
			// other input arrives.
			st.entryAt = gtx.Now
			gtx.Execute(op.InvalidateCmd{At: st.entryAt.Add(delay)})
		case !active && !st.entryAt.IsZero():
			// Hover/focus exit: clear entry; release arbitration top if
			// we held it.
			st.entryAt = time.Time{}
			if st.shown {
				clearTop(st.id)
				st.shown = false
			}
		}
		if active && !st.shown && !gtx.Now.Before(st.entryAt.Add(delay)) {
			setTop(st.id)
			st.shown = true
		}
		// Another tooltip overtook us while we remained active; drop the
		// shown flag locally without touching arbitration.
		if st.shown && !isTop(st.id) {
			st.shown = false
		}
	}

	// 4. Paint the trigger at the centred offset. When live, register
	//    the hover gesture and a focus tag clipped to the trigger rect
	//    so Enter/Leave and focus events fire for this hit area.
	{
		triggerOff := op.Offset(triggerPos).Push(gtx.Ops)
		if live {
			triggerClip := clip.Rect{Max: triggerDims.Size}.Push(gtx.Ops)
			st.hov.Add(gtx.Ops)
			event.Op(gtx.Ops, &st.focusTag)
			triggerClip.Pop()
		}
		triggerOps.Add(gtx.Ops)
		triggerOff.Pop()
	}

	// 5. Surface, only while shown.
	if st.shown {
		drawSurface(gtx, shaper, props, tok, triggerRect)
	}

	return layout.Dimensions{Size: canvas}
}

// drawStatic is the input-free variant used by Render: skips event
// processing and arbitration, but mirrors the layout math so the static
// frame matches the live frame at shown=true.
func drawStatic(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	shown bool,
) layout.Dimensions {
	canvas := gtx.Constraints.Max
	triggerMacro := op.Record(gtx.Ops)
	triggerGtx := gtx
	triggerGtx.Constraints = layout.Constraints{Max: canvas}
	var triggerDims layout.Dimensions
	if props.Trigger != nil {
		triggerDims = props.Trigger(triggerGtx)
	}
	triggerOps := triggerMacro.Stop()
	triggerPos := image.Pt((canvas.X-triggerDims.Size.X)/2, (canvas.Y-triggerDims.Size.Y)/2)
	triggerRect := image.Rectangle{Min: triggerPos, Max: triggerPos.Add(triggerDims.Size)}

	triggerOff := op.Offset(triggerPos).Push(gtx.Ops)
	triggerOps.Add(gtx.Ops)
	triggerOff.Pop()

	if shown {
		drawSurface(gtx, shaper, props, tok, triggerRect)
	}
	return layout.Dimensions{Size: canvas}
}

// drawSurface paints the rounded tooltip bubble with the Text label
// inside, positioned adjacent to triggerRect per props.Placement. The
// bubble uses the high-contrast OnSurface colour so it stands above the
// underlying Surface; the text uses Surface for the same reason.
func drawSurface(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	triggerRect image.Rectangle,
) {
	canvas := gtx.Constraints.Max
	r := gtx.Dp(unit.Dp(tok.radius.Sm))
	padH := gtx.Dp(unit.Dp(tok.spacing.S2))
	padV := gtx.Dp(unit.Dp(tok.spacing.S1))
	gap := gtx.Dp(unit.Dp(tok.spacing.S1))

	// Pre-record the label with its material so we can replay it inside
	// the surface at a known offset after measuring it.
	mColor := op.Record(gtx.Ops)
	paint.ColorOp{Color: tok.color.Surface}.Add(gtx.Ops)
	material := mColor.Stop()
	labelGtx := gtx
	labelGtx.Constraints = layout.Constraints{Max: image.Pt(canvas.X*3/4, canvas.Y/4)}
	labelGtx.Constraints.Min = image.Point{}
	mLabel := op.Record(gtx.Ops)
	wl := widget.Label{MaxLines: 1}
	labelDims := wl.Layout(labelGtx, shaper, font.Font{}, unit.Sp(tok.typ.LabelSmall), props.Text, material)
	labelCall := mLabel.Stop()

	surfW := labelDims.Size.X + 2*padH
	surfH := labelDims.Size.Y + 2*padV
	minW := gtx.Dp(unit.Dp(24))
	minH := gtx.Dp(unit.Dp(16))
	if surfW < minW {
		surfW = minW
	}
	if surfH < minH {
		surfH = minH
	}

	midX := (triggerRect.Min.X + triggerRect.Max.X) / 2
	midY := (triggerRect.Min.Y + triggerRect.Max.Y) / 2
	var pos image.Point
	switch props.Placement {
	case Top:
		pos = image.Pt(midX-surfW/2, triggerRect.Min.Y-gap-surfH)
	case Bottom:
		pos = image.Pt(midX-surfW/2, triggerRect.Max.Y+gap)
	case Left:
		pos = image.Pt(triggerRect.Min.X-gap-surfW, midY-surfH/2)
	case Right:
		pos = image.Pt(triggerRect.Max.X+gap, midY-surfH/2)
	}

	surfOff := op.Offset(pos).Push(gtx.Ops)
	rect := clip.RRect{Rect: image.Rectangle{Max: image.Pt(surfW, surfH)}, SE: r, SW: r, NE: r, NW: r}
	paint.FillShape(gtx.Ops, tok.color.OnSurface, rect.Op(gtx.Ops))
	labelOff := op.Offset(image.Pt(padH, padV)).Push(gtx.Ops)
	labelCall.Add(gtx.Ops)
	labelOff.Pop()
	surfOff.Pop()
}
