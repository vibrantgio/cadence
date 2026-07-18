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
// Focus ownership: the close affordance and each footer action own their own
// focus tag and focus ring (the close button is a prism/button; actions
// likewise register their own tags, e.g. a prism/button's caller-owned
// *widget.Clickable). The modal does not wrap an action or draw a ring around
// it — it only adds the caller-declared Props.ActionFocusTags to its Tab cycle
// (route (a)), so a focused action shows exactly one ring: its own.
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
	"github.com/vibrantgio/prism/button"
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

	// HideClose, when true, omits the top-right close button. Use it when
	// the footer Actions already provide explicit dismissal (e.g. a Cancel
	// button) — Escape and a scrim click still trigger OnClose.
	HideClose bool

	// DynamicFocusTags, if non-nil, is called every frame and its tags join
	// the Tab cycle after the close button and BEFORE ActionFocusTags. Use
	// it for focusables whose tags change across the modal's lifetime —
	// e.g. a prism TextField rebuilt per open (its editor tag, exposed via
	// TextFieldProps.FocusTag, is new each rebuild). The first tag in the
	// cycle receives initial focus when the modal opens.
	DynamicFocusTags func() []event.Tag

	// ActionFocusTags lists the focus tags of the focusable Actions, in the
	// order they should join the modal's Tab cycle (after the close button).
	//
	// Footer actions own their own focus tags and focus ring — the modal does
	// not wrap an action or draw a ring around it. A prism/button action, for
	// example, is built with a caller-owned *widget.Clickable; passing that
	// &clickable here adds it to the Tab cycle (and the Escape trap) with no
	// doubled outer ring. A non-focusable action (plain widget) simply omits
	// its tag. nil entries are skipped. See ActionFocusTags vs Actions: the
	// two slices are independent — list a tag here only for actions that
	// participate in keyboard focus.
	ActionFocusTags []event.Tag

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

	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}

		st := newState()

		// The close affordance is a prism/button icon-only variant. The modal
		// owns its clickable (&st.closeClick) so the focus trap stays keyed to
		// a single tag and no doubled focus ring is drawn; OnClose is routed
		// through the button's OnClick. Build once here in the rx.Defer scope
		// and fold the latest emitted widget into the input pipeline — never
		// subscribe inside the per-frame widget closure.
		closeBtn := rx.Of[layout.Widget](nil)
		if !props.HideClose {
			closeBtn = button.Button(th, button.Props{
				Icon:        crossIcon,
				Description: "Close",
				Clickable:   &st.closeClick,
				OnClick:     props.OnClose,
				Shaper:      shaper,
			})
		}

		inputs := rx.CombineLatest3(resolved, open, closeBtn)

		return rx.Map(inputs, func(next rx.Tuple3[resolvedTokens, bool, layout.Widget]) layout.Widget {
			tok, openNow, closeW := next.First, next.Second, next.Third

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
				return drawModal(gtx, shaper, props, tok, st, live, closeW)
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
	st := newState()
	// Static, inert close affordance: the same icon painter the live path
	// uses, rendered through button.RenderIcon so goldens stay text-free and
	// deterministic. Radius is threaded straight through (callers pass a sharp
	// radius for golden determinism).
	var closeW layout.Widget
	if !props.HideClose {
		closeW = button.RenderIcon(crossIcon, colors, sp, rad, ts, button.RenderState{})
	}
	return func(gtx layout.Context) layout.Dimensions {
		if !open {
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
		return drawModal(gtx, shaper, props, tok, st, false, closeW)
	}
}

// modalState holds per-subscription stable tags and the open-flag tracker.
// One instance is owned by each Modal subscription (and by static Render
// invocations, where focus and input handling are inert).
type modalState struct {
	id               int64
	pushed           bool
	wantInitialFocus bool

	// Stable tags so the router can route events across frames. The close
	// button's clickable doubles as its focus tag (driven by prism/button).
	// Footer actions own their own focus tags (Props.ActionFocusTags); the
	// modal holds none on their behalf.
	scrimTag   int
	surfaceTag int
	closeClick widget.Clickable
}

func newState() *modalState {
	return &modalState{id: allocStackID()}
}

// focusCount returns the number of focusable elements: the close button
// (unless hidden) plus one per non-nil caller-declared action focus tag.
func focusCount(props Props) int {
	n := 0
	if !props.HideClose {
		n = 1
	}
	for _, t := range props.ActionFocusTags {
		if t != nil {
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
	closeWidget layout.Widget,
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

	// Surface width — 75% of canvas clamped to a sensible min/max in dp.
	// Height HUGS THE CONTENT: the content is laid out once into a macro
	// (stateful widgets process their events exactly once), the surface is
	// sized to the recorded dims, and the macro is replayed inside the
	// positioned surface. maxH caps the surface at the old 75%-of-canvas
	// bound; overflowing content is clipped to the surface.
	surfW := clampInt(canvas.X*3/4, gtx.Dp(unit.Dp(180)), gtx.Dp(unit.Dp(560)))
	if surfW > canvas.X {
		surfW = canvas.X
	}
	// 560dp (not the historical 420) so tall forms — e.g. an alert plus
	// four fields plus actions — fit before the overflow clip engages.
	maxH := clampInt(canvas.Y*3/4, gtx.Dp(unit.Dp(120)), gtx.Dp(unit.Dp(560)))
	if maxH > canvas.Y {
		maxH = canvas.Y
	}
	inset := gtx.Dp(unit.Dp(tok.spacing.S5))

	contentGtx := gtx
	contentGtx.Constraints = layout.Constraints{
		Min: image.Pt(surfW-2*inset, 0),
		Max: image.Pt(surfW-2*inset, maxH-2*inset),
	}
	contentMacro := op.Record(gtx.Ops)
	contentDims := drawSurfaceContents(contentGtx, shaper, props, tok, gap, closeWidget)
	content := contentMacro.Stop()

	surfH := clampInt(contentDims.Size.Y+2*inset, gtx.Dp(unit.Dp(120)), maxH)
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

	// Surface inset content (header / body / footer) — the macro recorded
	// above, replayed at the inset origin and clipped to the surface.
	contentClip := clip.Rect{Max: image.Pt(surfW, surfH)}.Push(gtx.Ops)
	contentOff := op.Offset(image.Pt(inset, inset)).Push(gtx.Ops)
	content.Add(gtx.Ops)
	contentOff.Pop()
	contentClip.Pop()
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
	gap int,
	closeWidget layout.Widget,
) layout.Dimensions {
	header := headerWidget(shaper, props, tok, closeWidget)
	footer := footerWidget(props, tok)

	children := []layout.FlexChild{
		layout.Rigid(header),
		layout.Rigid(spacerV(gap)),
	}
	if props.Body != nil {
		children = append(children, layout.Rigid(props.Body))
	}
	if footer != nil {
		children = append(children, layout.Rigid(spacerV(gap)))
		children = append(children, layout.Rigid(footer))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// headerWidget renders the title (drawn only when non-empty) on the left and
// the close affordance — a prism/button icon variant, built upstream and
// threaded in as closeWidget — on the right. The button owns its own focus
// ring and click handling; the header only positions it.
func headerWidget(shaper *text.Shaper, props Props, tok resolvedTokens, closeWidget layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		titleFlex := layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if props.Title == "" {
				// Empty title contributes no height; the Rigid close button
				// drives the header row height via Middle alignment.
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
			}
			mColor := op.Record(gtx.Ops)
			paint.ColorOp{Color: tok.color.OnSurface}.Add(gtx.Ops)
			material := mColor.Stop()
			wl := widget.Label{MaxLines: 1}
			return wl.Layout(gtx, shaper, font.Font{Weight: font.SemiBold}, unit.Sp(tok.typ.TitleMedium), props.Title, material)
		})
		if closeWidget == nil {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, titleFlex)
		}
		closeFlex := layout.Rigid(closeWidget)
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, titleFlex, closeFlex)
	}
}

// footerWidget renders a right-aligned row of action widgets. Each action is
// laid out bare: it owns its own focus tag and focus ring (the modal neither
// wraps it nor decorates it), and joins the Tab cycle via Props.ActionFocusTags.
// Returns nil when there are no non-nil actions.
func footerWidget(props Props, tok resolvedTokens) layout.Widget {
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
		// The right-alignment filler must claim NO cross-axis height: with a
		// content-sized surface the row's Max.Y is all remaining space, and
		// a Constraints.Max-sized filler would inflate the footer to fill it
		// (the actions would float mid-surface over a sea of empty space).
		filler := func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
		}
		children := []layout.FlexChild{layout.Flexed(1, filler)}
		first := true
		for _, a := range props.Actions {
			a := a
			if a == nil {
				continue
			}
			if !first {
				children = append(children, layout.Rigid(pllayout.HSpacer(gap)))
			}
			first = false
			children = append(children, layout.Rigid(a))
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
		// tags can be empty (HideClose with no action tags) — nothing to
		// focus then, but never panic.
		if len(tags) > 0 {
			gtx.Execute(key.FocusCmd{Tag: tags[0]})
		}
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

	// The close button is a prism/button instance: it drains its own
	// Clicked() and invokes props.OnClose via Props.OnClick. The modal must
	// NOT also check st.closeClick.Clicked here — the button has already
	// consumed the event, so this check would always be false.

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

// focusTags returns the ordered slice of focus tags belonging to this modal:
// the close button first, then the caller-declared action focus tags. Action
// tags are owned by the action widgets themselves (Props.ActionFocusTags); the
// modal only sequences them for Tab cycling and the Escape trap.
func focusTags(props Props, st *modalState) []event.Tag {
	tags := make([]event.Tag, 0, focusCount(props))
	if !props.HideClose {
		tags = append(tags, &st.closeClick)
	}
	if props.DynamicFocusTags != nil {
		for _, t := range props.DynamicFocusTags() {
			if t != nil {
				tags = append(tags, t)
			}
		}
	}
	for _, t := range props.ActionFocusTags {
		if t != nil {
			tags = append(tags, t)
		}
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

// crossIcon paints an "×" shape — two diagonal strokes — into a
// sizePx×sizePx box at the current origin in colour col. It is the modal
// close button's glyph, satisfying the button.Props.Icon painter contract
// (clip.Path / clip.Stroke only — no font or SVG rasterisation) so goldens
// stay deterministic across GPU contexts.
func crossIcon(gtx layout.Context, sizePx int, col color.NRGBA) {
	w := float32(sizePx)
	h := float32(sizePx)
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
