package shell_test

import (
	"image"
	"sync/atomic"
	"testing"
	"time"

	gioinput "gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/shell"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

// These tests exercise the shells' external-value reconciliation under
// concurrent emissions: a Subject feeds SplitRatio/AsideWidth values
// from the rx Goroutine scheduler while the test goroutine lays out
// frames, the shape of any model-driven app (drag → message → model →
// re-emission). Drag state must only ever be touched on the frame
// goroutine, so `go test -race` fails if an emission projector writes
// it. The dims assertions are secondary; the real check is -race.

// raceHarness subscribes sh on the Goroutine scheduler and returns a
// getter for the most recent widget, waiting for the first emission
// (kick is called each poll to (re-)prod cold Subjects whose first
// value may precede the subscription).
func raceHarness(t *testing.T, sh rx.Observable[layout.Widget], kick func()) (latest func() layout.Widget, stop func()) {
	t.Helper()
	var cur atomic.Value
	sub := sh.Subscribe(func(next layout.Widget, err error, done bool) {
		if !done && next != nil {
			cur.Store(next)
		}
	}, rx.Goroutine)

	deadline := time.Now().Add(5 * time.Second)
	for cur.Load() == nil && time.Now().Before(deadline) {
		kick()
		time.Sleep(time.Millisecond)
	}
	if cur.Load() == nil {
		sub.Unsubscribe()
		t.Fatal("Shell did not emit a widget")
	}
	latest = func() layout.Widget { return cur.Load().(layout.Widget) }
	return latest, sub.Unsubscribe
}

func TestShellSplitPaneConcurrentRatioEmissions(t *testing.T) {
	ratioIn, ratioObs := rx.Subject[float32](0, 1, 128)
	props := shell.Props{
		Layout:     shell.SplitPane,
		SplitRatio: ratioObs,
	}
	sh := shell.Shell(rx.Of(theme.Default()), props)
	latest, stop := raceHarness(t, sh, func() { ratioIn.Next(0.5) })
	defer stop()

	r := new(gioinput.Router)
	ops := new(op.Ops)
	for i := 0; i < 50; i++ {
		dims := driveFrame(latest(), ops, r, dragSize)
		if dims.Size != dragSize {
			t.Fatalf("frame %d: dims = %v; want %v", i, dims.Size, dragSize)
		}
		ratioIn.Next(float32(i%9+1) / 10)
	}
	ratioIn.Done(nil)
}

func TestShellThreeColumnConcurrentWidthEmissions(t *testing.T) {
	widthIn, widthObs := rx.Subject[unit.Dp](0, 1, 128)
	props := shell.Props{
		Layout:     shell.ThreeColumn,
		Aside:      rx.Of[layout.Widget](fillRect(tokens.DefaultLight.Primary)),
		AsideWidth: widthObs,
	}
	sh := shell.Shell(rx.Of(theme.Default()), props)
	latest, stop := raceHarness(t, sh, func() { widthIn.Next(200) })
	defer stop()

	size := image.Pt(480, 256)
	r := new(gioinput.Router)
	ops := new(op.Ops)
	for i := 0; i < 50; i++ {
		dims := driveFrame(latest(), ops, r, size)
		if dims.Size != size {
			t.Fatalf("frame %d: dims = %v; want %v", i, dims.Size, size)
		}
		widthIn.Next(unit.Dp(180 + i))
	}
	widthIn.Done(nil)
}
