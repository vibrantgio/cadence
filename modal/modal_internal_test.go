package modal

import (
	"image"
	"testing"

	"gioui.org/font/gofont"
	gioinput "gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/prism/tokens"
)

// TestTabCyclesFocusAmongModalTags strengthens Measurable (b) — Tab "cycles
// focus within the modal" — by asserting at least two distinct modal focus
// tags are visited across a sequence of Tab presses. The companion external
// trap tests cover the "does not escape" clause; this in-package test
// covers the "cycles" clause by reading the unexported focus-tag slice.
func TestTabCyclesFocusAmongModalTags(t *testing.T) {
	shaper := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))

	stubSize := image.Pt(60, 28)
	action := func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: stubSize} }
	body := func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(100, 40)} }

	props := Props{
		Body:    body,
		Actions: []layout.Widget{action, action},
		OnClose: func() {},
	}
	st := newState(len(props.Actions))
	st.pushed = true
	st.wantInitialFocus = true
	stackPush(st.id)
	t.Cleanup(func() { stackPop(st.id) })

	tok := resolvedTokens{
		color:   tokens.DefaultLight,
		spacing: tokens.Spacing,
		radius:  tokens.RadiusScale{},
		typ:     tokens.DefaultTypeScale,
	}

	w := func(gtx layout.Context) layout.Dimensions {
		return drawModal(gtx, shaper, props, tok, st, true)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)
	canvas := image.Pt(320, 240)

	drive := func() {
		ops.Reset()
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(canvas),
			Ops:         ops,
			Source:      r.Source(),
		}
		w(gtx)
		r.Frame(ops)
	}
	drive() // frame 1: register tags
	drive() // frame 2: initial focus applied

	tags := focusTags(props, st)
	focusedIdx := func() int {
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(canvas),
			Ops:         new(op.Ops),
			Source:      r.Source(),
		}
		for i, tag := range tags {
			if gtx.Focused(tag) {
				return i
			}
		}
		return -1
	}

	initial := focusedIdx()
	if initial < 0 {
		t.Fatal("no modal tag focused after initial setup")
	}
	visited := map[int]bool{initial: true}
	for i := 0; i < 6; i++ {
		r.Queue(key.Event{Name: key.NameTab, State: key.Press})
		drive()
		idx := focusedIdx()
		if idx < 0 {
			t.Fatalf("Tab press #%d: no modal tag focused", i+1)
		}
		visited[idx] = true
	}

	if len(visited) < 2 {
		t.Errorf("Tab did not cycle focus among modal tags: visited %v (n=%d), want ≥ 2 distinct tags", visited, len(visited))
	}
}
