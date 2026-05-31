// Package modal provides the Cadence Modal pattern: a centered elevated
// Surface dialog over a full-window scrim backdrop, with a header (title +
// close affordance), padded body, and optional footer action row.
//
// The package follows the Phase 4 Composition contract: Modal is a callable
// Go function consuming a Prism theme observable, returning a stream of
// layout.Widget. The source is intentionally short and free of opaque
// configuration — copy it into your own app and modify as needed.
//
// Interaction: Escape and a backdrop click invoke Props.OnClose. Tab and
// Shift+Tab cycle keyboard focus within the modal's focusable items and do
// not escape to background content. Only the topmost modal on the
// coordination stack receives input; modals underneath remain painted but
// inert.
//
// Open/close is instantaneous in this package; entrance/exit transitions
// are deferred to a later Pulse-integration goal.
package modal

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
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

// Props configures a Modal. Body and OnClose may both be nil; Actions may
// contain nil entries (skipped). Title may be empty (the header still
// renders the close affordance).
type Props struct {
	// Open emits true to show the modal and false to hide it. A nil Open
	// is treated as a constant false (modal never opens).
	Open rx.Observable[bool]

	Title   string
	Body    layout.Widget
	OnClose func(gtx layout.Context)
	Actions []layout.Widget

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The default
	// shaper is created once per subscription inside the rx.Defer scope, so
	// it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	typ     tokens.TypeScale
}

// Modal returns an rx.Observable[layout.Widget] that emits a new widget
// whenever the theme or Open state changes. The widget renders a scrim and
// centered surface when open, or no pixels at all when closed. State
// (focus tags, the modal-stack id, the close-button clickable) persists
// across emissions in the rx.Defer scope.
func Modal(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	open := props.Open
	if open == nil {
		open = rx.Of(false)
	}

	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest4(t.Color, t.Spacing, t.Radius, t.Type),
			func(n rx.Tuple4[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale, tokens.TypeScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, radius: n.Third, typ: n.Fourth}
			},
		)
	})

	inputs := rx.CombineLatest2(resolved, open)

	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}

		st := newState(len(props.Actions))

		return rx.Map(inputs, func(next rx.Tuple2[resolvedTokens, bool]) layout.Widget {
			tok, openNow := next.First, next.Second

			// Transition tracking — push on open, pop on close.
			if openNow && !st.pushed {
				stackPush(st.id)
				st.pushed = true
				st.wantInitialFocus = true
			}
			if !openNow && st.pushed {
				stackPop(st.id)
				st.pushed = false
			}

			return func(gtx layout.Context) layout.Dimensions {
				if !openNow {
					return layout.Dimensions{Size: gtx.Constraints.Max}
				}
				live := isTop(st.id)
				return drawModal(gtx, shaper, props, tok, st, live)
			}
		})
	})
}

// Render produces a layout.Widget for a modal with pre-resolved tokens and
// an explicit open flag. Intended for golden-image testing and static
// demonstrations; production code should use Modal. The returned widget
// performs no input handling: pass open=true to render the scrim and
// surface, open=false to render nothing (the widget consumes the
// constraints but paints no pixels).
func Render(
	shaper *text.Shaper,
	props Props,
	open bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
	ts tokens.TypeScale,
) layout.Widget {
	tok := resolvedTokens{color: colors, spacing: sp, radius: rad, typ: ts}
	st := newState(len(props.Actions))
	return func(gtx layout.Context) layout.Dimensions {
		if !open {
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
		return drawModal(gtx, shaper, props, tok, st, false)
	}
}

// modalState holds per-subscription stable tags and the open-flag tracker.
// One instance is owned by each Modal subscription (and by static Render
// invocations, where focus and input handling are inert).
type modalState struct {
	id               int64
	pushed           bool
	wantInitialFocus bool

	// Stable tags so the router can route events across frames.
	scrimTag    int
	surfaceTag  int
	closeTag    int
	closeClick  widget.Clickable
	actionTags  []int

	focused int // index into focus tag list; -1 if none
}

func newState(nActions int) *modalState {
	return &modalState{
		id:         allocStackID(),
		actionTags: make([]int, nActions),
		focused:    -1,
	}
}

// focusCount returns the number of focusable elements: 1 (close button)
// plus one per non-nil action.
func focusCount(props Props) int {
	n := 1
	for _, a := range props.Actions {
		if a != nil {
			n++
		}
	}
	return n
}

// drawModal paints the scrim, centered surface, header (title + close),
// body, and footer actions, then processes input when live is true.
func drawModal(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	st *modalState,
	live bool,
) layout.Dimensions {
	canvas := gtx.Constraints.Max
	r := gtx.Dp(unit.Dp(tok.radius.Lg))
	gap := gtx.Dp(unit.Dp(tok.spacing.S3))

	// Scrim — full-canvas dimmer. Pointer events that miss the surface
	// hit the scrim tag and trigger OnClose.
	scrimColor := scrimColor(tok.color)
	scrimRect := image.Rectangle{Max: canvas}
	scrimClip := clip.Rect(scrimRect).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, scrimColor, clip.Rect(scrimRect).Op())
	if live {
		event.Op(gtx.Ops, &st.scrimTag)
	}
	scrimClip.Pop()

	// Surface size — clamp to canvas. The desired surface is 75% of canvas
	// in each axis, clamped to a sensible min/max in dp.
	surfW := clampInt(canvas.X*3/4, gtx.Dp(unit.Dp(180)), gtx.Dp(unit.Dp(560)))
	surfH := clampInt(canvas.Y*3/4, gtx.Dp(unit.Dp(120)), gtx.Dp(unit.Dp(420)))
	if surfW > canvas.X {
		surfW = canvas.X
	}
	if surfH > canvas.Y {
		surfH = canvas.Y
	}
	surfPos := image.Pt((canvas.X-surfW)/2, (canvas.Y-surfH)/2)

	// Surface — rounded rectangle, registered as a pointer absorber so
	// presses on its area do not reach the scrim and dismiss the modal.
	off := op.Offset(surfPos).Push(gtx.Ops)
	surfRRect := clip.RRect{Rect: image.Rectangle{Max: image.Pt(surfW, surfH)}, SE: r, SW: r, NE: r, NW: r}
	paint.FillShape(gtx.Ops, tok.color.Surface, surfRRect.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, tok.color.Outline, clip.Stroke{
		Path:  surfRRect.Path(gtx.Ops),
		Width: float32(gtx.Dp(unit.Dp(1))),
	}.Op())

	// Absorb pointer events on the surface.
	if live {
		absorbClip := clip.Rect{Max: image.Pt(surfW, surfH)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &st.surfaceTag)
		absorbClip.Pop()
	}

	// Surface inset content (header / body / footer).
	contentGtx := gtx
	contentGtx.Constraints = layout.Exact(image.Pt(surfW, surfH))
	contentGtx.Constraints.Min = image.Point{}
	layout.UniformInset(unit.Dp(tok.spacing.S5)).Layout(contentGtx, func(gtx layout.Context) layout.Dimensions {
		return drawSurfaceContents(gtx, shaper, props, tok, st, live, gap)
	})
	off.Pop()

	if live {
		processInput(gtx, props, st)
	}

	return layout.Dimensions{Size: canvas}
}

// drawSurfaceContents lays out the header row, the body, and the footer
// action row vertically inside the already-inset surface gtx.
func drawSurfaceContents(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	st *modalState,
	live bool,
	gap int,
) layout.Dimensions {
	header := headerWidget(shaper, props, tok, st, live)
	footer := footerWidget(props, tok, st, live)

	children := []layout.FlexChild{
		layout.Rigid(header),
		layout.Rigid(spacerV(gap)),
	}
	if props.Body != nil {
		children = append(children, layout.Flexed(1, props.Body))
	} else {
		children = append(children, layout.Flexed(1, emptyFlex()))
	}
	if footer != nil {
		children = append(children, layout.Rigid(spacerV(gap)))
		children = append(children, layout.Rigid(footer))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// headerWidget renders the title (drawn only when non-empty) and the
// close affordance on the right.
func headerWidget(shaper *text.Shaper, props Props, tok resolvedTokens, st *modalState, live bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		closeSize := gtx.Dp(unit.Dp(24))
		titleFlex := layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if props.Title == "" {
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, closeSize)}
			}
			mColor := op.Record(gtx.Ops)
			paint.ColorOp{Color: tok.color.OnSurface}.Add(gtx.Ops)
			material := mColor.Stop()
			wl := widget.Label{MaxLines: 1}
			return wl.Layout(gtx, shaper, font.Font{Weight: font.SemiBold}, unit.Sp(tok.typ.TitleMedium), props.Title, material)
		})
		closeFlex := layout.Rigid(closeButtonWidget(closeSize, tok, st, live))
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, titleFlex, closeFlex)
	}
}

// closeButtonWidget draws an "×" glyph in a square hit target and routes
// click/Enter/Space through st.closeClick. Focus participation is opt-in:
// only when live is true does the modal register the focus tag.
func closeButtonWidget(sizePx int, tok resolvedTokens, st *modalState, live bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		sz := image.Pt(sizePx, sizePx)
		gtx.Constraints = layout.Exact(sz)

		return st.closeClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			rect := image.Rectangle{Max: sz}
			defer clip.Rect(rect).Push(gtx.Ops).Pop()

			if live {
				event.Op(gtx.Ops, &st.closeTag)
				if gtx.Focused(&st.closeTag) {
					paint.FillShape(gtx.Ops, tok.color.Outline, clip.Stroke{
						Path:  clip.Rect(rect).Path(),
						Width: float32(gtx.Dp(unit.Dp(2))),
					}.Op())
				}
			}

			drawCross(gtx, sz, tok.color.OnSurfaceVariant)
			return layout.Dimensions{Size: sz}
		})
	}
}

// footerWidget renders a right-aligned row of action widgets, each wrapped
// in a clip area registered for focus participation when live. Returns nil
// when there are no non-nil actions.
func footerWidget(props Props, tok resolvedTokens, st *modalState, live bool) layout.Widget {
	if len(props.Actions) == 0 {
		return nil
	}
	any := false
	for _, a := range props.Actions {
		if a != nil {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	gap := tok.spacing.S2

	return func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{layout.Flexed(1, emptyFlex())}
		ai := 0
		first := true
		for i, a := range props.Actions {
			i, a := i, a
			if a == nil {
				ai++
				continue
			}
			if !first {
				children = append(children, layout.Rigid(pllayout.HSpacer(gap)))
			}
			first = false
			tagIdx := i
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				dims := a(gtx)
				if live {
					stk := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
					event.Op(gtx.Ops, &st.actionTags[tagIdx])
					if gtx.Focused(&st.actionTags[tagIdx]) {
						paint.FillShape(gtx.Ops, tok.color.Outline, clip.Stroke{
							Path:  clip.Rect{Max: dims.Size}.Path(),
							Width: float32(gtx.Dp(unit.Dp(2))),
						}.Op())
					}
					stk.Pop()
				}
				return dims
			}))
			ai++
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	}
}

// processInput drains the scrim, surface, close-button, escape, and tab
// events generated this frame and dispatches them: scrim/close/escape →
// OnClose, tab/shift-tab → cycle focus among the registered modal tags.
func processInput(gtx layout.Context, props Props, st *modalState) {
	// Drain FocusFilter events for each focus tag so the router retains focus
	// when set, mirroring prism/layout.FocusGroup.Update.
	tags := focusTags(props, st)
	for _, tag := range tags {
		for {
			if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
				break
			}
		}
	}

	// Set initial focus to the close button on the first frame after Open
	// transitions to true. Subsequent transitions are tracked by the rx
	// pipeline; here we just consume the flag.
	if st.wantInitialFocus {
		gtx.Execute(key.FocusCmd{Tag: tags[0]})
		st.wantInitialFocus = false
	}

	// Backdrop click → OnClose.
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &st.scrimTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			fire(gtx, props.OnClose)
		}
	}

	// Drain surface presses so they are not re-dispatched anywhere else.
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &st.surfaceTag, Kinds: pointer.Press}); !ok {
			break
		}
	}

	// Close button click (mouse or Space/Enter when focused).
	if st.closeClick.Clicked(gtx) {
		fire(gtx, props.OnClose)
	}

	// Escape → OnClose. Register the filter against every modal focus tag
	// so the event fires whenever any modal element has focus.
	for _, tag := range tags {
		for {
			e, ok := gtx.Event(key.Filter{Focus: tag, Name: key.NameEscape})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				fire(gtx, props.OnClose)
			}
		}
	}

	// Tab / Shift+Tab focus cycling among modal tags. Registering the
	// filter with Focus: tag traps Tab before the router's default
	// MoveFocus advances focus to background content.
	curIdx := currentFocusIdx(gtx, tags)
	for i, tag := range tags {
		for {
			e, ok := gtx.Event(key.Filter{Focus: tag, Name: key.NameTab, Optional: key.ModShift})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				dir := 1
				if ke.Modifiers.Contain(key.ModShift) {
					dir = -1
				}
				n := len(tags)
				if n == 0 {
					continue
				}
				base := curIdx
				if base < 0 {
					base = i
				}
				nextIdx := (base + dir + n) % n
				gtx.Execute(key.FocusCmd{Tag: tags[nextIdx]})
				curIdx = nextIdx
			}
		}
	}
}

// focusTags returns the ordered slice of focus tags belonging to this
// modal: the close button first, then one per non-nil action.
func focusTags(props Props, st *modalState) []event.Tag {
	tags := make([]event.Tag, 0, focusCount(props))
	tags = append(tags, &st.closeTag)
	for i, a := range props.Actions {
		if a == nil {
			continue
		}
		tags = append(tags, &st.actionTags[i])
	}
	return tags
}

// currentFocusIdx returns the index of the currently-focused modal tag,
// or -1 if no modal element holds focus.
func currentFocusIdx(gtx layout.Context, tags []event.Tag) int {
	for i, tag := range tags {
		if gtx.Focused(tag) {
			return i
		}
	}
	return -1
}

// drawCross paints an "×" shape — two diagonal strokes — centered inside
// the size-sz square, in the given colour. Used as the close affordance.
func drawCross(gtx layout.Context, sz image.Point, col color.NRGBA) {
	w := float32(sz.X)
	h := float32(sz.Y)
	pad := float32(gtx.Dp(unit.Dp(6)))
	stroke := float32(gtx.Dp(unit.Dp(2)))
	if stroke < 1 {
		stroke = 1
	}
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(pad, pad))
	p.LineTo(f32.Pt(w-pad, h-pad))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: stroke}.Op())

	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(w-pad, pad))
	p.LineTo(f32.Pt(pad, h-pad))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: stroke}.Op())
}

// scrimColor returns a translucent dim laid over the scene background.
// Light themes get a black scrim; dark themes also use black for
// consistency with material-style scrims that dim by reducing luminance.
func scrimColor(_ tokens.ColorTokens) color.NRGBA {
	return color.NRGBA{R: 0, G: 0, B: 0, A: 0x80}
}

// spacerV returns a vertical-spacer widget that consumes hPx pixels in
// the Y axis and zero pixels in X. Used inside the vertical Flex stack.
func spacerV(hPx int) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(0, hPx)}
	}
}

// emptyFlex returns a widget that consumes its available constraints and
// paints nothing. Used as a Flexed spacer in the footer row to right-
// align the action group and as a fallback Body when Props.Body is nil.
func emptyFlex() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
}

// fire invokes cb when cb is non-nil. Centralised so OnClose is never
// called against a nil pointer.
func fire(gtx layout.Context, cb func(gtx layout.Context)) {
	if cb != nil {
		cb(gtx)
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
