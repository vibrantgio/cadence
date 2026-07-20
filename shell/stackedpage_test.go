package shell_test

import (
	"image"
	"image/color"
	"sync/atomic"
	"testing"
	"time"

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
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

const (
	stackedW, stackedH = 480, 256
)

var stackedSize = image.Pt(stackedW, stackedH)

// band is a fixed-height full-width filler used as a StackedPage
// section: sections receive an unbounded height from the shell's
// scroll list, so unlike fillRect they must size their own height.
func band(c color.NRGBA, h int) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(gtx.Constraints.Max.X, h)
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// TestShellStackedPageGolden records or diffs the StackedPage goldens.
// The short case fits within the viewport, so the footer is visible
// with the Background ground below it — the footer scrolls with the
// content instead of pinning to the viewport bottom. The overflow
// cases exceed the viewport and must clip at its edge.
func TestShellStackedPageGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}
	s1Fill := color.NRGBA{R: 0x33, G: 0x99, B: 0x66, A: 0xff}
	s2Fill := color.NRGBA{R: 0x88, G: 0x55, B: 0x22, A: 0xff}
	s3Fill := color.NRGBA{R: 0x22, G: 0x55, B: 0x88, A: 0xff}
	footFill := color.NRGBA{R: 0x66, G: 0x33, B: 0x99, A: 0xff}

	props := func(footer layout.Widget) shell.Props {
		return shell.Props{
			Layout: shell.StackedPage,
			Navbar: navbar.Props{Links: []navbar.Link{{Label: ""}, {Label: ""}}, Shaper: shaper},
			Footer: footer,
		}
	}
	short := []layout.Widget{band(s1Fill, 60), band(s2Fill, 60)}
	overflow := []layout.Widget{band(s1Fill, 90), band(s2Fill, 90), band(s3Fill, 90)}
	maxWProps := props(band(footFill, 40))
	maxWProps.ContentMaxWidth = 240

	cases := []struct {
		name     string
		sections []layout.Widget
		props    shell.Props
		colors   tokens.ColorTokens
		bg       color.NRGBA
	}{
		{"light-stacked-page-short", short, props(band(footFill, 40)), tokens.DefaultLight, lightBG},
		{"light-stacked-page-overflow", overflow, props(band(footFill, 40)), tokens.DefaultLight, lightBG},
		{"dark-stacked-page-overflow", overflow, props(band(footFill, 40)), tokens.DefaultDark, darkBG},
		{"light-stacked-page-maxwidth", short, maxWProps, tokens.DefaultLight, lightBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := shell.RenderStackedPage(shaper, tc.props, tc.sections, tc.colors, tokens.Spacing, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, stackedSize, scene(w, tc.bg))
		})
	}
}

// TestShellStackedPageContentMaxWidth verifies that a positive
// ContentMaxWidth hands sections exactly the clamped width and centers
// the column on the page with the Background showing in the margins,
// and that a clamp at or above the page width is a no-op.
func TestShellStackedPageContentMaxWidth(t *testing.T) {
	shaper := defaultShaper(t)
	bandFill := color.NRGBA{R: 0x33, G: 0x99, B: 0x66, A: 0xff}

	cases := []struct {
		name  string
		maxW  unit.Dp
		wantW int
	}{
		{"clamped", 240, 240},
		{"wider-than-page", 1000, stackedW},
		{"zero", 0, stackedW},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotMin, gotMax int
			section := func(gtx layout.Context) layout.Dimensions {
				gotMin = gtx.Constraints.Min.X
				gotMax = gtx.Constraints.Max.X
				return band(bandFill, 60)(gtx)
			}
			props := shell.Props{
				Layout:          shell.StackedPage,
				Navbar:          navbar.Props{Shaper: shaper},
				ContentMaxWidth: tc.maxW,
			}
			w := shell.RenderStackedPage(shaper, props, []layout.Widget{section},
				tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypeScale)
			img := capture(t, stackedSize, w)
			if img == nil {
				return
			}
			if gotMin != tc.wantW || gotMax != tc.wantW {
				t.Fatalf("section width constraints: min %d max %d; want exactly %d", gotMin, gotMax, tc.wantW)
			}

			eq := func(c color.RGBA, n color.NRGBA) bool {
				return c.R == n.R && c.G == n.G && c.B == n.B && c.A == n.A
			}
			margin := (stackedW - tc.wantW) / 2
			y := 64 + 30 // mid-band: navbar height + half the 60 px section
			bg := tokens.DefaultLight.Background
			if margin > 0 {
				for _, x := range []int{0, margin - 1, margin + tc.wantW, stackedW - 1} {
					if got := img.RGBAAt(x, y); !eq(got, bg) {
						t.Errorf("margin pixel (%d,%d) = %v; want Background %v", x, y, got, bg)
					}
				}
			}
			for _, x := range []int{margin, margin + tc.wantW/2, margin + tc.wantW - 1} {
				if got := img.RGBAAt(x, y); !eq(got, bandFill) {
					t.Errorf("content pixel (%d,%d) = %v; want band %v", x, y, got, bandFill)
				}
			}
		})
	}
}

// TestShellStackedPageScrolls verifies that the shell-owned scroll
// region both virtualizes (offscreen sections are never laid out) and
// responds to pointer scroll events. Canvas 480×256 leaves a 192 px
// body under the 64 px navbar; five 120 px sections total 600 px. At
// rest only sections 0 and 1 fit the viewport; after scrolling 300 px
// the window covers sections 2–4 and section 0 must not be laid out.
func TestShellStackedPageScrolls(t *testing.T) {
	laid := make([]int, 5)
	sections := make([]rx.Observable[layout.Widget], 5)
	for i := range sections {
		i := i
		inner := band(color.NRGBA{R: uint8(40 * (i + 1)), A: 255}, 120)
		sections[i] = rx.Of[layout.Widget](func(gtx layout.Context) layout.Dimensions {
			laid[i]++
			return inner(gtx)
		})
	}
	props := shell.Props{
		Layout:   shell.StackedPage,
		Sections: sections,
	}
	w := liveWidget(t, shell.Shell(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	driveFrame(w, ops, r, stackedSize)
	driveFrame(w, ops, r, stackedSize)

	reset := func() {
		for i := range laid {
			laid[i] = 0
		}
	}
	reset()
	driveFrame(w, ops, r, stackedSize)
	if laid[0] == 0 || laid[1] == 0 {
		t.Fatalf("sections 0 and 1 should be laid out at rest; got %v", laid)
	}
	if laid[3] != 0 || laid[4] != 0 {
		t.Fatalf("offscreen sections 3 and 4 laid out at rest (no virtualization); got %v", laid)
	}

	r.Queue(pointer.Event{
		Kind:     pointer.Scroll,
		Position: f32.Pt(240, 150),
		Scroll:   f32.Pt(0, 300),
		Source:   pointer.Mouse,
	})
	// The frame that absorbs the scroll still walks children from the
	// old first index while consuming the offset; the advanced window
	// only shows in the next, settled frame.
	driveFrame(w, ops, r, stackedSize)
	reset()
	driveFrame(w, ops, r, stackedSize)
	if laid[0] != 0 {
		t.Errorf("section 0 still laid out after scrolling 300 px down; got %v", laid)
	}
	if laid[3] == 0 {
		t.Errorf("section 3 not laid out after scrolling 300 px down; got %v", laid)
	}
}

// TestShellStackedPageSectionReEmission verifies that a section stream
// re-emitting (the shape of a theme change) re-emits the shell widget
// itself. This is the property that lets observable-driven apps repaint
// on section changes without a layer-boundary adapter: the shell
// emission is what drives the window's Invalidate.
func TestShellStackedPageSectionReEmission(t *testing.T) {
	secIn, secObs := rx.Subject[layout.Widget](0, 1, 16)
	props := shell.Props{
		Layout:   shell.StackedPage,
		Sections: []rx.Observable[layout.Widget]{secObs},
	}
	sh := shell.Shell(rx.Of(theme.Default()), props)

	var emissions atomic.Int32
	sub := sh.Subscribe(rx.GoroutineContext(), func(next layout.Widget, err error, done bool) {
		if !done && next != nil {
			emissions.Add(1)
		}
	})
	defer sub.Unsubscribe()

	waitAbove := func(n int32) bool {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if emissions.Load() > n {
				return true
			}
			secIn.Next(band(color.NRGBA{R: 200, A: 255}, 60))
			time.Sleep(time.Millisecond)
		}
		return false
	}
	if !waitAbove(0) {
		t.Fatal("Shell did not emit for the initial section widget")
	}
	seen := emissions.Load()
	if !waitAbove(seen) {
		t.Fatalf("section re-emission did not re-emit the shell (stuck at %d emissions)", seen)
	}
	secIn.Done(nil)
}

// TestShellStackedPageTabTraversal verifies that Tab focus traverses
// the shell in op-stream order navbar → sections top to bottom →
// footer. All stops except the navbar link are externally observable
// clickables; the link stop is observable as "nothing focused".
func TestShellStackedPageTabTraversal(t *testing.T) {
	shaper := defaultShaper(t)
	var seedClick, s0Click, s1Click, brandClick, footerClick widget.Clickable

	clickBand := func(c *widget.Clickable, col color.NRGBA, h int) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return c.Layout(gtx, band(col, h))
		}
	}
	brandWidget := func(gtx layout.Context) layout.Dimensions {
		return brandClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(40, 20)
			paint.FillShape(gtx.Ops, color.NRGBA{R: 80, G: 80, B: 200, A: 255}, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		})
	}

	props := shell.Props{
		Layout: shell.StackedPage,
		Navbar: navbar.Props{
			Brand: brandWidget,
			Links: []navbar.Link{
				{Label: "", OnClick: func(_ layout.Context) {}},
			},
			Shaper: shaper,
		},
		Sections: []rx.Observable[layout.Widget]{
			rx.Of[layout.Widget](clickBand(&s0Click, color.NRGBA{R: 0, G: 200, B: 0, A: 255}, 60)),
			rx.Of[layout.Widget](clickBand(&s1Click, color.NRGBA{R: 200, G: 120, B: 0, A: 255}, 60)),
		},
		Footer: clickBand(&footerClick, color.NRGBA{R: 120, G: 0, B: 200, A: 255}, 40),
	}
	bodyW := liveWidget(t, shell.Shell(rx.Of(theme.Default()), props))

	// Compose: a seed clickable (zero-size visual) then the shell, so
	// MoveFocus(Forward) from the seed enters the shell at its first
	// focusable.
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

	driveFrame(composed, ops, r, tabSize)
	driveFrame(composed, ops, r, tabSize)

	tags := []struct {
		name string
		c    *widget.Clickable
	}{
		{"seed", &seedClick},
		{"brand", &brandClick},
		{"section0", &s0Click},
		{"section1", &s1Click},
		{"footer", &footerClick},
	}

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
		{"Tab #3 (→ section 0)", &s0Click},
		{"Tab #4 (→ section 1)", &s1Click},
		{"Tab #5 (→ footer)", &footerClick},
	}
	for _, s := range steps {
		r.MoveFocus(key.FocusForward)
		driveFrame(composed, ops, r, tabSize)
		check(s.stage, s.want)
	}
}
