// Command gallery shows the Cadence Modal in action so the GX.4 close
// affordance — now a prism/button icon-only variant — can be exercised live:
// hover the × for the Primary hover overlay, Tab to it for the focus ring,
// then click it, press Enter/Space while focused, press Escape, or click the
// dimmed backdrop to dismiss. Click "Open dialog" to bring it back.
//
// Run from the repo root: go run ./cadence/modal/gallery
package main

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"sync"

	"gioui.org/app"
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
	"github.com/vibrantgio/cadence/modal"
	"github.com/vibrantgio/prism/button"
	"github.com/vibrantgio/prism/coordination"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

func main() {
	go func() {
		w := new(app.Window)
		w.Option(
			app.Title("Cadence — Modal (GX.4 close button)"),
			app.Size(unit.Dp(560), unit.Dp(440)),
		)
		if err := run(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

type demo struct {
	win    *app.Window
	shaper *text.Shaper

	openObserver rx.Observer[bool]
	openBtn      layout.Widget

	mu          sync.Mutex
	modalWidget layout.Widget
	closes      int
}

func run(w *app.Window) error {
	shaper := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	d := &demo{win: w, shaper: shaper}

	// Static theme — emits once synchronously, so .First() returns immediately
	// and the modal's CombineLatest fires as soon as Open emits.
	th := rx.Of(theme.Default())

	var openObs rx.Observable[bool]
	d.openObserver, openObs = coordination.Subject[bool](coordination.BufCapSignal)

	// The trigger is itself a prism/button — dogfooding the same component the
	// modal's close affordance now uses.
	var err error
	d.openBtn, err = button.Button(th, button.Props{
		Label:   "Open dialog",
		Shaper:  shaper,
		OnClick: func(_ layout.Context) { d.openObserver.Next(true); w.Invalidate() },
	}).First()
	if err != nil {
		return err
	}

	// Live modal. OnClose flows back through the Open subject so the dialog
	// actually hides, and bumps a visible counter so each dismissal is obvious.
	light := tokens.DefaultLight
	modalObs := modal.Modal(th, modal.Props{
		Open:  openObs,
		Title: "Confirm action",
		Body:  d.body,
		Actions: []layout.Widget{
			d.actionChip("Cancel", light.SurfaceVariant, light.OnSurfaceVariant),
			d.actionChip("OK", light.Primary, light.OnPrimary),
		},
		Shaper: shaper,
		OnClose: func(_ layout.Context) {
			d.mu.Lock()
			d.closes++
			d.mu.Unlock()
			d.openObserver.Next(false)
			w.Invalidate()
		},
	})
	sub := modalObs.Subscribe(func(mw layout.Widget, _ error, done bool) {
		if !done && mw != nil {
			d.mu.Lock()
			d.modalWidget = mw
			d.mu.Unlock()
			w.Invalidate()
		}
	}, rx.Goroutine)
	defer sub.Unsubscribe()

	// Open the dialog on launch.
	d.openObserver.Next(true)

	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			d.frame(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// body is the modal's content: a short instructional paragraph.
func (d *demo) body(gtx layout.Context) layout.Dimensions {
	m := op.Record(gtx.Ops)
	paint.ColorOp{Color: tokens.DefaultLight.OnSurfaceVariant}.Add(gtx.Ops)
	mat := m.Stop()
	lbl := widget.Label{}
	return lbl.Layout(gtx, d.shaper, font.Font{}, unit.Sp(15),
		"The × in the header is a prism/button icon variant. It takes focus when "+
			"the dialog opens — press Tab to cycle the focus ring × → Cancel → OK → ×. "+
			"Hover the × for the Primary overlay; click it, press Enter/Space while it "+
			"is focused, press Escape, or click the dimmed backdrop to close.",
		mat)
}

// actionChip is a static footer control: a filled rounded rect with a centered
// label. It is not independently interactive — the modal wraps each action as a
// Tab focus stop and draws the focus ring around it — so two chips are enough to
// make Tab focus-cycling visible (× → Cancel → OK → ×).
func (d *demo) actionChip(label string, fill, fg color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		sz := image.Pt(gtx.Dp(unit.Dp(96)), gtx.Dp(unit.Dp(40)))
		rad := gtx.Dp(unit.Dp(8))
		rr := clip.RRect{Rect: image.Rectangle{Max: sz}, SE: rad, SW: rad, NE: rad, NW: rad}
		paint.FillShape(gtx.Ops, fill, rr.Op(gtx.Ops))

		m := op.Record(gtx.Ops)
		paint.ColorOp{Color: fg}.Add(gtx.Ops)
		mat := m.Stop()
		lblGtx := gtx
		lblGtx.Constraints.Min = image.Point{}
		rec := op.Record(gtx.Ops)
		lbl := widget.Label{MaxLines: 1, Alignment: text.Middle}
		ld := lbl.Layout(lblGtx, d.shaper, font.Font{}, unit.Sp(14), label, mat)
		call := rec.Stop()
		off := op.Offset(image.Pt((sz.X-ld.Size.X)/2, (sz.Y-ld.Size.Y)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		off.Pop()
		return layout.Dimensions{Size: sz}
	}
}

func (d *demo) frame(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, tokens.DefaultLight.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Trigger button + dismissal counter, near the top. Visible whenever the
	// modal is closed; when open, the scrim is painted over them and absorbs
	// the clicks (so the backdrop dismisses instead).
	layout.Inset{Top: unit.Dp(28), Left: unit.Dp(28), Right: unit.Dp(28)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = gtx.Dp(unit.Dp(180))
				return d.openBtn(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(0, gtx.Dp(unit.Dp(12)))}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				d.mu.Lock()
				n := d.closes
				d.mu.Unlock()
				m := op.Record(gtx.Ops)
				paint.ColorOp{Color: tokens.DefaultLight.OnBackground}.Add(gtx.Ops)
				mat := m.Stop()
				lbl := widget.Label{MaxLines: 1}
				return lbl.Layout(gtx, d.shaper, font.Font{}, unit.Sp(14),
					fmt.Sprintf("Dismissed %d time(s)", n), mat)
			}),
		)
	})

	// Live modal on top: paints scrim + surface when open, nothing when closed.
	d.mu.Lock()
	mw := d.modalWidget
	d.mu.Unlock()
	if mw != nil {
		mw(gtx)
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}
