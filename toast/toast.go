// Package toast provides the Cadence Toast pattern: a position-anchored
// column of transient notifications. Application code calls the
// package-scoped Notify entry point to emit a toast; one or more active
// Stack subscriptions render the queued toasts in their chosen corner.
// Each toast auto-dismisses after a configurable Lifetime, fading out
// over the last fadeWindow of that lifetime via pulse/tween.
//
// The package follows the Phase 4 Composition contract: Stack is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. The source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// The Subject behind Notify is process-global: every active Stack
// receives every Toast. Per-stack routing (channels, topics) is out of
// scope for this package; callers that want it can wrap Notify and
// Stack in their own filter.
package toast

import (
	"image"
	"image/color"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/prism/coordination"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
	"github.com/vibrantgio/pulse/depth"
	"github.com/vibrantgio/pulse/tween"
)

// Level selects the toast's semantic palette.
type Level int

const (
	Info Level = iota
	Success
	Warning
	Error
)

// Position is the screen corner where the stack anchors. Newest toast
// renders nearest the anchored edge; older toasts sit further from it.
type Position int

const (
	TopRight Position = iota
	BottomRight
	TopLeft
	BottomLeft
)

// DefaultLifetime is the auto-dismiss duration applied when Props.Lifetime
// is zero or negative.
const DefaultLifetime = 4 * time.Second

// fadeWindow is the trailing slice of Lifetime during which a toast tweens
// its alpha from 1.0 to 0.0. Picked short enough that the dismiss feels
// snappy but long enough that the fade is perceptible at 60 fps.
const fadeWindow = 400 * time.Millisecond

// Toast is a single notification value. Notify constructs one and pushes
// it onto the package-scoped Subject; every active Stack receives it.
type Toast struct {
	ID    int64
	Level Level
	Text  string
}

// Props configures a Stack.
type Props struct {
	Position Position
	Lifetime time.Duration

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The
	// default shaper is created once per subscription inside the
	// rx.Defer scope, so it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

// Package-scoped Subject for notifications. Notify is a free function so
// any code with the package imported can emit toasts; Stack subscriptions
// fan-in via the Subject's Observable side.
var (
	publish       rx.Observer[Toast]
	Notifications rx.Observable[Toast]
	nextID        atomic.Int64
)

func init() {
	publish, Notifications = coordination.Subject[Toast](coordination.BufCapSignal)
}

// Notify emits a Toast onto the package-scoped Subject. Every active
// Stack subscription receives it on the next frame.
func Notify(level Level, textValue string) {
	publish.Next(Toast{ID: nextID.Add(1), Level: level, Text: textValue})
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	typ     tokens.TypeScale
}

// Stack returns an rx.Observable[layout.Widget] that renders a positioned
// column of the toasts queued via Notify. The widget closure prunes
// expired toasts on each frame, scheduling the next invalidation at the
// earliest interesting time (fade-start or expiry).
func Stack(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	lifetime := props.Lifetime
	if lifetime <= 0 {
		lifetime = DefaultLifetime
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
		st := newStackState()
		// Each Notify emission mutates st (queuing the new toast) and
		// surfaces as a struct{} ping. StartWith seeds CombineLatest so
		// the first layout.Widget emits before any Notify.
		pings := rx.Map(Notifications, func(t Toast) struct{} {
			st.enqueue(t)
			return struct{}{}
		}).StartWith(struct{}{})
		return rx.Map(rx.CombineLatest2(resolved, pings), func(n rx.Tuple2[resolvedTokens, struct{}]) layout.Widget {
			tok := n.First
			return func(gtx layout.Context) layout.Dimensions {
				return drawStackLive(gtx, shaper, props, lifetime, tok, st)
			}
		})
	})
}

// Render produces a layout.Widget for a fixed []Toast snapshot with
// pre-resolved tokens. Intended for golden-image testing and static
// demonstrations; production code should use Stack. The returned widget
// performs no input handling, no fading, and does not consume the
// package-scoped Subject.
func Render(
	shaper *text.Shaper,
	props Props,
	toasts []Toast,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
	ts tokens.TypeScale,
) layout.Widget {
	tok := resolvedTokens{color: colors, spacing: sp, radius: rad, typ: ts}
	return func(gtx layout.Context) layout.Dimensions {
		return drawStackStatic(gtx, shaper, props, toasts, tok)
	}
}

// stackState holds the per-subscription FIFO queue. items[0] is the
// oldest toast; items[len-1] is the newest. enqueue is callable from any
// goroutine (the rx Map running off the Subject's scheduler); the widget
// closure mutates items only at frame time.
type stackState struct {
	mu    sync.Mutex
	items []activeToast
}

type activeToast struct {
	toast   Toast
	addedAt time.Time // zero until the first frame that observes the toast
}

func newStackState() *stackState { return &stackState{} }

// enqueue appends t to the queue with a zero addedAt. The widget closure
// stamps addedAt on the frame it first sees the toast, so expiry math
// runs against gtx.Now instead of wall-clock time — keeping the lifetime
// assertion deterministic under synthetic clocks.
func (s *stackState) enqueue(t Toast) {
	s.mu.Lock()
	s.items = append(s.items, activeToast{toast: t})
	s.mu.Unlock()
}

// snapshot returns a copy of the queue trimmed to non-expired entries.
// addedAt is stamped on the first observation (zero → now). The earliest
// expiry instant is returned so the caller can schedule InvalidateCmd.
// If no toasts remain the returned time is zero.
func (s *stackState) snapshot(now time.Time, lifetime time.Duration) (items []activeToast, nextWake time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.items[:0]
	for _, it := range s.items {
		if it.addedAt.IsZero() {
			it.addedAt = now
		}
		expiresAt := it.addedAt.Add(lifetime)
		if !now.Before(expiresAt) {
			continue
		}
		out = append(out, it)
		// Wake at the start of fade or at expiry, whichever is sooner.
		wake := expiresAt.Add(-fadeWindow)
		if wake.Before(now) {
			wake = expiresAt
		}
		if nextWake.IsZero() || wake.Before(nextWake) {
			nextWake = wake
		}
	}
	for i := len(out); i < len(s.items); i++ {
		s.items[i] = activeToast{}
	}
	s.items = out
	return append([]activeToast(nil), out...), nextWake
}

// drawStackLive prunes expired toasts, schedules the next invalidation,
// and paints the surviving toasts with per-toast fade alpha.
func drawStackLive(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	lifetime time.Duration,
	tok resolvedTokens,
	st *stackState,
) layout.Dimensions {
	now := gtx.Now
	items, nextWake := st.snapshot(now, lifetime)
	if len(items) > 0 {
		// Always re-invalidate at the next wake; during the fade we
		// also redraw every frame so the alpha animates smoothly.
		if !nextWake.IsZero() {
			gtx.Execute(op.InvalidateCmd{At: nextWake})
		}
		for _, it := range items {
			if now.Sub(it.addedAt) >= lifetime-fadeWindow {
				gtx.Execute(op.InvalidateCmd{})
				break
			}
		}
	}
	return paintStack(gtx, shaper, props, tok, items, lifetime, now)
}

// drawStackStatic paints the supplied toasts at full opacity with no
// scheduling. Used by Render for goldens.
func drawStackStatic(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	toasts []Toast,
	tok resolvedTokens,
) layout.Dimensions {
	items := make([]activeToast, len(toasts))
	for i, t := range toasts {
		items[i] = activeToast{toast: t}
	}
	// addedAt remains zero → fadeAlpha returns 1.0 → full opacity.
	return paintStack(gtx, shaper, props, tok, items, 0, time.Time{})
}

const (
	toastWidthDp = 240
	toastMinHDp  = 36
)

// paintStack lays out the column of toasts at the anchored corner of the
// canvas. lifetime and now drive the fade alpha for live frames; both
// zero means "fully opaque" (the Render path).
func paintStack(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	items []activeToast,
	lifetime time.Duration,
	now time.Time,
) layout.Dimensions {
	canvas := gtx.Constraints.Max
	edgePad := gtx.Dp(unit.Dp(tok.spacing.S4))
	gap := gtx.Dp(unit.Dp(tok.spacing.S2))
	width := gtx.Dp(unit.Dp(toastWidthDp))
	if width > canvas.X-2*edgePad {
		width = canvas.X - 2*edgePad
		if width < 0 {
			width = 0
		}
	}

	leftAnchored := props.Position == TopLeft || props.Position == BottomLeft
	topAnchored := props.Position == TopLeft || props.Position == TopRight

	var x int
	if leftAnchored {
		x = edgePad
	} else {
		x = canvas.X - edgePad - width
	}

	// Render order: newest nearest the anchored edge. items[len-1] is
	// the newest. For top-anchored stacks we walk newest-first downward;
	// for bottom-anchored stacks we walk newest-first upward.
	order := make([]int, len(items))
	if topAnchored {
		for i := range items {
			order[i] = len(items) - 1 - i
		}
	} else {
		for i := range items {
			order[i] = i
		}
	}

	// First measure all toasts so bottom-anchored stacks can position
	// from the bottom up.
	heights := make([]int, len(items))
	macros := make([]op.CallOp, len(items))
	for vis, idx := range order {
		macro := op.Record(gtx.Ops)
		toastGtx := gtx
		toastGtx.Constraints = layout.Constraints{
			Min: image.Pt(width, gtx.Dp(unit.Dp(toastMinHDp))),
			Max: image.Pt(width, canvas.Y),
		}
		dims := paintToast(toastGtx, shaper, tok, items[idx], lifetime, now)
		macros[vis] = macro.Stop()
		heights[vis] = dims.Size.Y
	}

	var y int
	if topAnchored {
		y = edgePad
	} else {
		total := 0
		for i, h := range heights {
			total += h
			if i > 0 {
				total += gap
			}
		}
		y = canvas.Y - edgePad - total
	}

	for vis := range order {
		off := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		macros[vis].Add(gtx.Ops)
		off.Pop()
		y += heights[vis] + gap
	}

	return layout.Dimensions{Size: canvas}
}

// paintToast paints one elevated, tinted row sized to its content: a
// Level3 cast shadow under a SurfaceVariant fill tinted 20% with the
// level accent, ringed by a 1dp accent outline. The Surface-based 12%
// tint this replaces sat at ~1.2:1 against Surface-painted panes (and
// 1.01:1 against SurfaceVariant ones in dark themes) — the toast only
// read as a shape because of its outline. The fade alpha is applied to
// the shadow (via PushOpacity), the fill, and the text colour.
func paintToast(
	gtx layout.Context,
	shaper *text.Shaper,
	tok resolvedTokens,
	it activeToast,
	lifetime time.Duration,
	now time.Time,
) layout.Dimensions {
	padH := gtx.Dp(unit.Dp(tok.spacing.S3))
	padV := gtx.Dp(unit.Dp(tok.spacing.S2))
	r := gtx.Dp(unit.Dp(tok.radius.Md))
	alpha := fadeAlpha(it, lifetime, now)

	accent := accentColor(it.toast.Level, tok.color)
	fill := withAlpha(tintSurface(tok.color.SurfaceVariant, accent), alpha)
	outline := withAlpha(accent, alpha)
	fg := withAlpha(tok.color.OnSurface, alpha)

	// Pre-record the label so we can size the surface around its dims.
	mColor := op.Record(gtx.Ops)
	paint.ColorOp{Color: fg}.Add(gtx.Ops)
	material := mColor.Stop()
	mLabel := op.Record(gtx.Ops)
	labelGtx := gtx
	labelGtx.Constraints = layout.Constraints{
		Max: image.Pt(gtx.Constraints.Max.X-2*padH, gtx.Constraints.Max.Y),
	}
	wl := widget.Label{MaxLines: 1}
	labelDims := wl.Layout(labelGtx, shaper, font.Font{}, unit.Sp(tok.typ.LabelMedium), it.toast.Text, material)
	labelCall := mLabel.Stop()

	w := gtx.Constraints.Max.X
	h := labelDims.Size.Y + 2*padV
	if h < gtx.Constraints.Min.Y {
		h = gtx.Constraints.Min.Y
	}

	// The shadow, not the fill, separates the toast on dark themes; it
	// shares the toast's fade so it never outlives the surface.
	opacity := paint.PushOpacity(gtx.Ops, float32(alpha))
	depth.Shadow(gtx, image.Rectangle{Max: image.Pt(w, h)}, tokens.Level3)
	opacity.Pop()

	rect := clip.RRect{Rect: image.Rectangle{Max: image.Pt(w, h)}, SE: r, SW: r, NE: r, NW: r}
	paint.FillShape(gtx.Ops, fill, rect.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, outline, clip.Stroke{
		Path:  rect.Path(gtx.Ops),
		Width: float32(gtx.Dp(unit.Dp(1))),
	}.Op())

	labelY := padV
	if labelDims.Size.Y < h-2*padV {
		labelY = (h - labelDims.Size.Y) / 2
	}
	labelOff := op.Offset(image.Pt(padH, labelY)).Push(gtx.Ops)
	labelCall.Add(gtx.Ops)
	labelOff.Pop()

	return layout.Dimensions{Size: image.Pt(w, h)}
}

// fadeAlpha returns the toast's current alpha in [0,1]. lifetime==0 (the
// Render path) or addedAt zero means "fully opaque". Inside the live
// path, the alpha tweens from 1.0 to 0.0 across the final fadeWindow of
// the lifetime via pulse/tween.LerpFloat64.
func fadeAlpha(it activeToast, lifetime time.Duration, now time.Time) float64 {
	if lifetime <= 0 || it.addedAt.IsZero() {
		return 1
	}
	age := now.Sub(it.addedAt)
	if age >= lifetime {
		return 0
	}
	if age < lifetime-fadeWindow {
		return 1
	}
	tw := tween.Tween[float64]{
		From:   1,
		To:     0,
		Frames: int(fadeWindow / time.Millisecond),
		Lerp:   tween.LerpFloat64,
	}
	frame := int((age - (lifetime - fadeWindow)) / time.Millisecond)
	return tw.At(frame)
}

// accentColor maps Level to its accent colour. Info and Error read
// directly from token roles so they flip automatically with light/dark;
// Success and Warning fall back to locally-defined Tailwind palettes (no
// token role exists for those semantics yet) — mirroring cadence/alert.
func accentColor(l Level, c tokens.ColorTokens) color.NRGBA {
	switch l {
	case Info:
		return c.Primary
	case Error:
		return c.Error
	case Success:
		return localAccent(c, green700, green400)
	case Warning:
		return localAccent(c, amber700, amber400)
	default:
		return c.Primary
	}
}

var (
	green700 = color.NRGBA{0x15, 0x80, 0x3d, 0xff}
	green400 = color.NRGBA{0x4a, 0xde, 0x80, 0xff}
	amber700 = color.NRGBA{0xb4, 0x54, 0x09, 0xff}
	amber400 = color.NRGBA{0xfb, 0xbf, 0x24, 0xff}
)

func localAccent(c tokens.ColorTokens, lightShade, darkShade color.NRGBA) color.NRGBA {
	if luminance(c.Surface) > luminance(c.OnSurface) {
		return lightShade
	}
	return darkShade
}

func luminance(c color.NRGBA) int { return int(c.R) + int(c.G) + int(c.B) }

// tintSurface blends 20% of the accent over the given base. Strong
// enough that the fill itself separates from Surface-painted panes;
// paired with the SurfaceVariant base in paintToast.
func tintSurface(base, accent color.NRGBA) color.NRGBA {
	return blend(base, accent, 0x33)
}

func blend(base, over color.NRGBA, alpha uint8) color.NRGBA {
	a := float32(alpha) / 255
	return color.NRGBA{
		R: uint8(float32(over.R)*a + float32(base.R)*(1-a)),
		G: uint8(float32(over.G)*a + float32(base.G)*(1-a)),
		B: uint8(float32(over.B)*a + float32(base.B)*(1-a)),
		A: 0xff,
	}
}

func withAlpha(c color.NRGBA, a float64) color.NRGBA {
	if a >= 1 {
		return c
	}
	if a <= 0 {
		return color.NRGBA{}
	}
	out := c
	out.A = uint8(float64(c.A)*a + 0.5)
	return out
}
