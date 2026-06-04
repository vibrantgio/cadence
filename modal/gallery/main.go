// Command gallery shows the Cadence Modal in action. The header close
// affordance (GX.4) and the footer Cancel/OK actions (GX.5) are all
// prism/button instances that own their own focus ring; the modal sequences
// them into one Tab cycle (× → Cancel → OK → ×) via Props.ActionFocusTags.
//
// Exercise it: hover the × for the Primary overlay; press Tab to move the focus
// ring across the controls; activate any with Enter/Space; press Escape or
// click the dimmed backdrop to dismiss; click "Open dialog" to bring it back.
//
// Run from the repo root: go run ./cadence/modal/gallery
package main

import (
	"fmt"
	"image"
	"log"
	"os"
	"sync"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/event"
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

	// Footer action clickables — caller-owned so they double as the actions'
	// focus tags (passed to the modal via Props.ActionFocusTags).
	cancelClk widget.Clickable
	okClk     widget.Clickable

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

	// Dismiss the dialog: bump a visible counter and drive Open false so the
	// modal actually hides. Shared by the close button, Escape/backdrop
	// (Props.OnClose), and the footer actions' own OnClick.
	closeDialog := func() {
		d.mu.Lock()
		d.closes++
		d.mu.Unlock()
		d.openObserver.Next(false)
		w.Invalidate()
	}

	// Footer actions are prism/buttons keyed to caller-owned clickables. Each
	// draws its own focus ring; passing &clickable in ActionFocusTags adds it
	// to the modal's Tab cycle with no doubled outer ring (GX.5).
	cancelBtn, err := button.Button(th, button.Props{
		Label:     "Cancel",
		Shaper:    shaper,
		Clickable: &d.cancelClk,
		OnClick:   func(_ layout.Context) { closeDialog() },
	}).First()
	if err != nil {
		return err
	}
	okBtn, err := button.Button(th, button.Props{
		Label:     "OK",
		Shaper:    shaper,
		Clickable: &d.okClk,
		OnClick:   func(_ layout.Context) { closeDialog() },
	}).First()
	if err != nil {
		return err
	}

	// Live modal. OnClose (close button, Escape, backdrop) also dismisses.
	modalObs := modal.Modal(th, modal.Props{
		Open:            openObs,
		Title:           "Confirm action",
		Body:            d.body,
		Actions:         []layout.Widget{footerSlot(cancelBtn), footerSlot(okBtn)},
		ActionFocusTags: []event.Tag{&d.cancelClk, &d.okClk},
		Shaper:          shaper,
		OnClose:         func(_ layout.Context) { closeDialog() },
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

// footerSlot constrains an otherwise fill-width button to a compact fixed width
// so the footer shows two right-aligned buttons rather than one stretched bar.
// It only sets the width budget; the wrapped button still owns its focus tag.
func footerSlot(w layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = 0
		gtx.Constraints.Max.X = gtx.Dp(unit.Dp(110))
		return w(gtx)
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
