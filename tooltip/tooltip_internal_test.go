package tooltip

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/font/gofont"
	gioinput "gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/prism/tokens"
)

// These interaction tests are white-box because tooltip exposes no
// callbacks: visibility is the internal `shown` flag. The popover pattern
// asserts arbitration through OnDismiss; tooltip cannot, so the
// equivalent state-flag inspection happens here.

const intCanvasW, intCanvasH = 320, 240

var intCanvas = image.Pt(intCanvasW, intCanvasH)

func intTrigger() layout.Widget {
	c := color.NRGBA{R: 80, G: 160, B: 220, A: 255}
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(gtx.Dp(unit.Dp(60)), gtx.Dp(unit.Dp(28)))
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

func intTok() resolvedTokens {
	return resolvedTokens{
		color:   tokens.DefaultLight,
		spacing: tokens.Spacing,
		radius:  tokens.RadiusScale{},
		typ:     tokens.DefaultTypeScale,
	}
}

func driveFrameAt(w layout.Widget, ops *op.Ops, r *gioinput.Router, size image.Point, now time.Time) {
	ops.Reset()
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(size),
		Now:         now,
		Ops:         ops,
		Source:      r.Source(),
	}
	w(gtx)
	r.Frame(ops)
}

// TestHoverEntryAfterDelayShows verifies Measurable (a): hover entry
// followed by the delay elapsing flips st.shown to true. Before the
// delay, st.shown must remain false.
func TestHoverEntryAfterDelayShows(t *testing.T) {
	const delay = 50 * time.Millisecond
	shaper := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	props := Props{Text: "Save", Trigger: intTrigger(), Placement: Top, Shaper: shaper}
	st := newState()
	t.Cleanup(func() { clearTop(st.id) })

	w := func(gtx layout.Context) layout.Dimensions {
		return drawTooltip(gtx, shaper, props, delay, intTok(), st, true)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)
	t0 := time.Unix(1700000000, 0)

	// Frame 1: register hover and focus tags. Nothing in the queue yet.
	driveFrameAt(w, ops, r, intCanvas, t0)
	if st.shown {
		t.Fatalf("st.shown=true before any hover event; want false")
	}

	// Queue a pointer.Move at the canvas centre (inside the trigger). The
	// router synthesizes pointer.Enter into the hover gesture next frame.
	r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(intCanvasW/2, intCanvasH/2), Source: pointer.Mouse})

	// Frame 2 at t0: hover Enter consumed, st.entryAt = t0, delay not
	// elapsed → shown stays false.
	driveFrameAt(w, ops, r, intCanvas, t0)
	if st.entryAt.IsZero() {
		t.Fatalf("hover Enter did not record entry time")
	}
	if st.shown {
		t.Fatalf("st.shown=true before delay elapsed; want false")
	}

	// Frame 3 at t0+delay+1ms: delay elapsed → shown flips to true.
	driveFrameAt(w, ops, r, intCanvas, t0.Add(delay).Add(time.Millisecond))
	if !st.shown {
		t.Fatalf("st.shown=false after delay elapsed; want true")
	}
	if !isTop(st.id) {
		t.Fatalf("arbitration top != tooltip id after show")
	}
}

// TestHoverExitHides verifies Measurable (b): once the tooltip is shown,
// hover Leave hides it (st.shown flips back to false).
func TestHoverExitHides(t *testing.T) {
	const delay = 50 * time.Millisecond
	shaper := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	props := Props{Text: "Save", Trigger: intTrigger(), Placement: Top, Shaper: shaper}
	st := newState()
	t.Cleanup(func() { clearTop(st.id) })

	w := func(gtx layout.Context) layout.Dimensions {
		return drawTooltip(gtx, shaper, props, delay, intTok(), st, true)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)
	t0 := time.Unix(1700000000, 0)
	tShown := t0.Add(delay).Add(time.Millisecond)

	// Bring up the tooltip via the same sequence as (a).
	driveFrameAt(w, ops, r, intCanvas, t0)
	r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(intCanvasW/2, intCanvasH/2), Source: pointer.Mouse})
	driveFrameAt(w, ops, r, intCanvas, t0)
	driveFrameAt(w, ops, r, intCanvas, tShown)
	if !st.shown {
		t.Fatalf("precondition failed: tooltip not shown after entry+delay")
	}

	// Move the pointer outside the trigger. The router emits Leave; the
	// gesture flips to !hovered → active goes false → shown clears.
	r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(4, 4), Source: pointer.Mouse})
	driveFrameAt(w, ops, r, intCanvas, tShown.Add(time.Millisecond))
	if st.shown {
		t.Fatalf("st.shown=true after hover exit; want false")
	}
	if isTop(st.id) {
		t.Fatalf("arbitration top still equals tooltip id after exit")
	}
}

// TestSecondTooltipDismissesFirst verifies Measurable (c): once tooltip
// A is shown, another tooltip claiming arbitration top causes A's next
// frame to drop its shown flag without releasing top (because it no
// longer holds it).
func TestSecondTooltipDismissesFirst(t *testing.T) {
	const delay = 50 * time.Millisecond
	shaper := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	props := Props{Text: "Save", Trigger: intTrigger(), Placement: Top, Shaper: shaper}
	st := newState()
	t.Cleanup(func() { clearTop(st.id) })

	w := func(gtx layout.Context) layout.Dimensions {
		return drawTooltip(gtx, shaper, props, delay, intTok(), st, true)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)
	t0 := time.Unix(1700000000, 0)
	tShown := t0.Add(delay).Add(time.Millisecond)

	// Bring A up the same way.
	driveFrameAt(w, ops, r, intCanvas, t0)
	r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(intCanvasW/2, intCanvasH/2), Source: pointer.Mouse})
	driveFrameAt(w, ops, r, intCanvas, t0)
	driveFrameAt(w, ops, r, intCanvas, tShown)
	if !st.shown {
		t.Fatalf("precondition failed: tooltip A not shown after entry+delay")
	}

	// A second tooltip claims arbitration top. We simulate this directly
	// because no second Tooltip instance is needed to prove the contract.
	otherID := allocID()
	setTop(otherID)
	t.Cleanup(func() { clearTop(otherID) })

	// A's next frame observes that it no longer holds top; shown drops.
	// Pointer is still hovering, so st.entryAt remains set.
	driveFrameAt(w, ops, r, intCanvas, tShown.Add(time.Millisecond))
	if st.shown {
		t.Fatalf("st.shown=true after another tooltip took arbitration top; want false")
	}
	if isTop(st.id) {
		t.Fatalf("A still reports arbitration top after another id claimed it")
	}
}
