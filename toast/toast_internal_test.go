package toast

import (
	"image"
	"testing"
	"time"

	"gioui.org/font/gofont"
	gioinput "gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

const intCanvasW, intCanvasH = 320, 240

var intCanvas = image.Pt(intCanvasW, intCanvasH)

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

// TestNotifyAddsAndLifetimeExpires discharges the Measurable interaction:
// Notify adds a toast to the stack and the stack length returns to its
// prior value after Lifetime elapses. White-box test driving synthetic
// gtx.Now against an isolated stackState — avoids both real-time flakes
// and the cross-test pollution that would arise from sharing the
// package-scoped Subject across tests.
func TestNotifyAddsAndLifetimeExpires(t *testing.T) {
	const lifetime = 200 * time.Millisecond
	shaper := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	props := Props{Position: TopRight, Lifetime: lifetime, Shaper: shaper}
	st := newStackState()

	w := func(gtx layout.Context) layout.Dimensions {
		return drawStackLive(gtx, shaper, props, lifetime, intTok(), st)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)
	t0 := time.Unix(1700000000, 0)

	// Frame 1: empty stack baseline.
	driveFrameAt(w, ops, r, intCanvas, t0)
	st.mu.Lock()
	pre := len(st.items)
	st.mu.Unlock()
	if pre != 0 {
		t.Fatalf("prior stack length = %d; want 0", pre)
	}

	// Simulate Notify by enqueuing directly on the isolated state. This
	// stands in for the package-scoped Subject side-effect without
	// involving the global Notifications observable (which would leak
	// state across tests).
	st.enqueue(Toast{ID: 1, Level: Info})

	// Frame 2 at t0: the toast is observed for the first time; addedAt
	// is stamped to t0, expiry to t0+lifetime. Stack length = 1.
	driveFrameAt(w, ops, r, intCanvas, t0)
	st.mu.Lock()
	mid := len(st.items)
	st.mu.Unlock()
	if mid != pre+1 {
		t.Fatalf("after Notify, stack length = %d; want %d", mid, pre+1)
	}

	// Frame 3 at t0+lifetime+1ms: the toast has expired. snapshot
	// prunes; stack length returns to the prior value.
	driveFrameAt(w, ops, r, intCanvas, t0.Add(lifetime).Add(time.Millisecond))
	st.mu.Lock()
	post := len(st.items)
	st.mu.Unlock()
	if post != pre {
		t.Fatalf("after lifetime, stack length = %d; want %d (returned-to-prior)", post, pre)
	}
}

// TestNotifyReachesStackSubscription confirms that the package-scoped
// Notify entry point is wired to Stack via the coordination Subject:
// calling Notify drives a re-emission of layout.Widget. Uses a fresh
// goroutine scheduler so the Subject's spinlock receiver runs alongside
// the test.
func TestNotifyReachesStackSubscription(t *testing.T) {
	props := Props{Position: TopRight, Lifetime: time.Second}
	obs := Stack(rx.Of(theme.Default()), props)

	emissions := make(chan layout.Widget, 4)
	sub := obs.Subscribe(rx.GoroutineContext(), func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			select {
			case emissions <- next:
			default:
			}
		}
	})
	defer sub.Unsubscribe()

	// Drain the initial seed emission (the StartWith-injected ping).
	select {
	case <-emissions:
	case <-time.After(2 * time.Second):
		t.Fatal("Stack did not emit an initial widget within 2s")
	}

	Notify(Info, "")

	select {
	case <-emissions:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify did not drive a Stack re-emission within 2s")
	}
}
