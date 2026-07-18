package shell_test

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/f32"
	gioinput "gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/navbar"
	"github.com/vibrantgio/cadence/shell"
	"github.com/vibrantgio/cadence/sidebar"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

const (
	threeColW, threeColH = 640, 256
)

var threeColSize = image.Pt(threeColW, threeColH)

// TestShellThreeColumnGolden records or diffs the ThreeColumn goldens:
// the full five-slot composition in both schemes, and the degenerate
// no-aside/no-footer form, whose navbar must span the full width.
func TestShellThreeColumnGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}
	mainFill := color.NRGBA{R: 0x33, G: 0x99, B: 0x66, A: 0xff}
	asideFill := color.NRGBA{R: 0x88, G: 0x55, B: 0x22, A: 0xff}
	footerFill := color.NRGBA{R: 0x22, G: 0x55, B: 0x88, A: 0xff}

	sbProps := sidebar.Props{
		Items: []sidebar.Item{
			{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) {}},
			{Icon: testIcon(), Label: "", OnClick: func(_ layout.Context) {}},
		},
		Shaper: shaper,
	}

	cases := []struct {
		name   string
		colors tokens.ColorTokens
		bg     color.NRGBA
		aside  layout.Widget
		footer layout.Widget
		width  unit.Dp
	}{
		{"light-three-column", tokens.DefaultLight, lightBG, fillRect(asideFill), fillRect(footerFill), 160},
		{"dark-three-column", tokens.DefaultDark, darkBG, fillRect(asideFill), fillRect(footerFill), 160},
		{"light-three-column-no-aside", tokens.DefaultLight, lightBG, nil, nil, 160},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sbW := sidebar.Render(shaper, sbProps, false, tc.colors, tokens.Spacing, tokens.DefaultTypeScale)
			props := shell.Props{
				Layout: shell.ThreeColumn,
				Navbar: navbar.Props{Links: []navbar.Link{{Label: ""}, {Label: ""}}, Shaper: shaper},
				Main:   fillRect(mainFill),
				Footer: tc.footer,
			}
			w := shell.RenderThreeColumn(shaper, props, sbW, tc.aside, tc.colors, tokens.Spacing, tokens.DefaultTypeScale, tc.width)
			renderGolden(t, tc.name, threeColSize, scene(w, tc.bg))
		})
	}
}

// TestShellThreeColumnAsideResize verifies that pressing on the aside
// divider and dragging horizontally emits absolute-width updates via
// OnAsideResize. With PxPerDp=1, a 480-wide canvas, an empty sidebar
// (width 0) and an initial aside width of 200, the divider (6 px wide)
// sits at x ∈ [274, 280). A press at (277, 100) followed by a drag to
// (227, 100) moves the divider 50 px toward leading, growing the aside
// to 250 dp.
func TestShellThreeColumnAsideResize(t *testing.T) {
	var got []unit.Dp
	props := shell.Props{
		Layout:     shell.ThreeColumn,
		Aside:      rx.Of[layout.Widget](fillRect(color.NRGBA{R: 200, A: 255})),
		AsideWidth: rx.Of(unit.Dp(200)),
		OnAsideResize: func(_ layout.Context, w unit.Dp) {
			got = append(got, w)
		},
	}
	w := liveWidget(t, shell.Shell(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	size := image.Pt(480, 256)
	// Warm-up frames so the divider's clip area is registered with the
	// router before pointer events are queued.
	driveFrame(w, ops, r, size)
	driveFrame(w, ops, r, size)

	press := f32.Pt(277, 100)
	drag := f32.Pt(227, 100)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: press, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Move, Position: drag, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: drag, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, size)

	if len(got) == 0 {
		t.Fatalf("OnAsideResize not invoked; want at least one update")
	}
	last := got[len(got)-1]
	if last != 250 {
		t.Errorf("final width = %v; want 250", last)
	}
}

// TestShellThreeColumnTabTraversal verifies that Tab focus traverses
// the shell in op-stream order navbar → sidebar → main → aside →
// footer, matching the visual reading order (top bar first, then the
// columns leading-to-trailing, then the bottom strip). Every region
// except the navbar link contributes an externally observable
// clickable; the link stop is observable as "nothing focused".
func TestShellThreeColumnTabTraversal(t *testing.T) {
	shaper := defaultShaper(t)
	var seedClick, sbClick, brandClick, mainClick, asideClick, footerClick widget.Clickable

	clickFill := func(c *widget.Clickable, col color.NRGBA) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return c.Layout(gtx, fillRect(col))
		}
	}
	sidebarWidget := func(gtx layout.Context) layout.Dimensions {
		return sbClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(40, gtx.Constraints.Max.Y)
			paint.FillShape(gtx.Ops, color.NRGBA{R: 60, G: 60, B: 60, A: 255}, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		})
	}
	brandWidget := func(gtx layout.Context) layout.Dimensions {
		return brandClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(40, 20)
			paint.FillShape(gtx.Ops, color.NRGBA{R: 80, G: 80, B: 200, A: 255}, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		})
	}

	props := shell.Props{
		Layout:  shell.ThreeColumn,
		Sidebar: rx.Of[layout.Widget](sidebarWidget),
		Navbar: navbar.Props{
			Brand: brandWidget,
			Links: []navbar.Link{
				{Label: "", OnClick: func(_ layout.Context) {}},
			},
			Shaper: shaper,
		},
		Main:   clickFill(&mainClick, color.NRGBA{R: 0, G: 200, B: 0, A: 255}),
		Aside:  rx.Of[layout.Widget](clickFill(&asideClick, color.NRGBA{R: 200, G: 120, B: 0, A: 255})),
		Footer: clickFill(&footerClick, color.NRGBA{R: 120, G: 0, B: 200, A: 255}),
	}
	bodyW := liveWidget(t, shell.Shell(rx.Of(theme.Default()), props))

	// Compose: a seed clickable (zero-size visual) then the shell. The
	// seed is a focus anchor whose position in the op-stream is before
	// the shell, so MoveFocus(Forward) from the seed enters the shell
	// at its first focusable.
	composed := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return seedClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(1, 1)}
				})
			}),
			layout.Flexed(1, bodyW),
		)
	}

	r := new(gioinput.Router)
	ops := new(op.Ops)

	// Two warm-up frames for stable hit-test data.
	driveFrame(composed, ops, r, tabSize)
	driveFrame(composed, ops, r, tabSize)

	tags := []struct {
		name string
		c    *widget.Clickable
	}{
		{"seed", &seedClick},
		{"sidebar", &sbClick},
		{"brand", &brandClick},
		{"main", &mainClick},
		{"aside", &asideClick},
		{"footer", &footerClick},
	}

	// Drain any synthetic focus events so the router retains focus when
	// explicitly set, matching the FocusGroup idiom used elsewhere.
	drainFocus := func() {
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(tabSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		for _, tg := range tags {
			for {
				if _, ok := gtx.Event(key.FocusFilter{Target: tg.c}); !ok {
					break
				}
			}
		}
	}
	drainFocus()

	// Anchor focus at the seed.
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(tabSize),
		Ops:         ops,
		Source:      r.Source(),
	}
	gtx.Execute(key.FocusCmd{Tag: &seedClick})
	driveFrame(composed, ops, r, tabSize)

	check := func(stage string, want *widget.Clickable) {
		t.Helper()
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(tabSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		for _, tg := range tags {
			got := gtx.Focused(tg.c)
			if got != (tg.c == want) {
				t.Errorf("%s: focused(%s)=%v; want %v", stage, tg.name, got, tg.c == want)
			}
		}
	}

	check("after Focus(seed)", &seedClick)

	steps := []struct {
		stage string
		want  *widget.Clickable
	}{
		{"Tab #1 (→ navbar brand)", &brandClick},
		{"Tab #2 (→ navbar link)", nil},
		{"Tab #3 (→ sidebar)", &sbClick},
		{"Tab #4 (→ main)", &mainClick},
		{"Tab #5 (→ aside)", &asideClick},
		{"Tab #6 (→ footer)", &footerClick},
	}
	for _, s := range steps {
		r.MoveFocus(key.FocusForward)
		driveFrame(composed, ops, r, tabSize)
		check(s.stage, s.want)
	}
}
