package navbar_test

import (
	"context"
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
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/navbar"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 480, 64
)

var canvasSize = image.Pt(canvasW, canvasH)

// defaultShaper returns the shaper every golden here draws with: the default
// typography's faces pinned, system fonts off, so the stored images are the
// same on every machine. A golden test pins its faces with
// DeterministicShaper; application code takes the fallback Shaper. See
// AGENTS.md.
func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return tokens.DefaultTypography.DeterministicShaper()
}

// scene renders w into a canvas-sized constraint over a flat background.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// linkLabels names the two links in document order. They were blank until
// F4.4b, on the theory that font rasterisation was non-deterministic; F4.2
// pinned the faces by configuration and F4.3 moved every golden onto
// DeterministicShaper, so Latin text in Roboto rasterises identically on every
// machine. ASCII only, per F4.2 — no symbol reaches a stored image.
var linkLabels = [2]string{"Docs", "Components"}

// links returns the two-link fixture, optionally marking one Active.
// activeIdx < 0 means no link is active.
func links(activeIdx int) []navbar.Link {
	out := make([]navbar.Link, len(linkLabels))
	for i, l := range linkLabels {
		out[i] = navbar.Link{Label: l, Active: i == activeIdx}
	}
	return out
}

// TestNavbarGolden records or diffs the three Measurable goldens. Each link
// cell is its label plus (S3, S2) padding, so the Active link's Primary
// underline runs the width of the label rather than the width of the bare
// padding it spanned while the labels were blank.
func TestNavbarGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	defaultLinks := links(-1)
	activeSecond := links(1)

	cases := []struct {
		name   string
		links  []navbar.Link
		colors tokens.ColorTokens
		bg     color.NRGBA
	}{
		{"light-default", defaultLinks, tokens.DefaultLight, lightBG},
		{"dark-default", defaultLinks, tokens.DefaultDark, darkBG},
		{"light-active-second-link", activeSecond, tokens.DefaultLight, lightBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := navbar.Props{Links: tc.links, Shaper: shaper}
			w := navbar.Render(shaper, props, tc.colors, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestNavbarActiveVsDefaultDiffer guards the visual contract that an
// Active link adds Primary-coloured pixels in the link row.
func TestNavbarActiveVsDefaultDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	render := func(links []navbar.Link) *image.RGBA {
		props := navbar.Props{Links: links, Shaper: shaper}
		w := navbar.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
		return golden.Capture(t, canvasSize, scene(w, bg))
	}

	def := render(links(-1))
	act := render(links(1))
	if n := golden.PixelDiff(def, act); n == 0 {
		t.Errorf("active and default render identically; expected Primary underline pixels")
	}
}

// ---- Interaction tests ----

// fillRect is a sharp-edged solid widget with a fixed size.
func fillRect(c color.NRGBA, w, h int) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(w, h)
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// liveWidget subscribes to nb, drains the trampoline scheduler, and
// returns the latest emitted layout.Widget. State referenced by the
// widget closure remains valid for the test's lifetime because it is
// captured by the rx.Defer scope.
func liveWidget(t *testing.T, nb rx.Observable[layout.Widget]) layout.Widget {
	t.Helper()
	var w layout.Widget
	if err := nb.Subscribe(context.Background(), func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}).Wait(); err != nil {
		t.Fatalf("Navbar subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Navbar did not emit an initial widget")
	}
	return w
}

// driveFrame lays out w against ops + router and returns the dims.
func driveFrame(w layout.Widget, ops *op.Ops, r *gioinput.Router, size image.Point) layout.Dimensions {
	ops.Reset()
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(size),
		Ops:         ops,
		Source:      r.Source(),
	}
	dims := w(gtx)
	r.Frame(ops)
	return dims
}

// TestNavbarTabTraversal verifies Tab cycles focus through
// brand → links → actions in document order, and Shift+Tab reverses.
// Brand and action contribute focus stops via outer-test Clickables;
// the two link stops are owned by the navbar.
func TestNavbarTabTraversal(t *testing.T) {
	var brandClick, actionClick widget.Clickable
	brand := func(gtx layout.Context) layout.Dimensions {
		return brandClick.Layout(gtx, fillRect(color.NRGBA{R: 80, G: 80, B: 200, A: 255}, 40, 20))
	}
	action := func(gtx layout.Context) layout.Dimensions {
		return actionClick.Layout(gtx, fillRect(color.NRGBA{R: 200, G: 80, B: 80, A: 255}, 40, 20))
	}

	props := navbar.Props{
		Brand: brand,
		Links: []navbar.Link{
			{Label: linkLabels[0], OnClick: func(_ layout.Context) {}},
			{Label: linkLabels[1], OnClick: func(_ layout.Context) {}},
		},
		Actions: []layout.Widget{action},
		Shaper:  defaultShaper(t),
	}
	w := liveWidget(t, navbar.Navbar(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)

	// Frame 0: register tags.
	driveFrame(w, ops, r, canvasSize)

	// Drain any synthetic focus events for the externally-owned tags so
	// the router retains focus when set, matching the FocusGroup idiom.
	drainFocus := func() {
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(canvasSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		for _, tag := range []any{&brandClick, &actionClick} {
			for {
				if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
					break
				}
			}
		}
	}
	drainFocus()

	// Focus the brand explicitly.
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(canvasSize),
		Ops:         ops,
		Source:      r.Source(),
	}
	gtx.Execute(key.FocusCmd{Tag: &brandClick})
	driveFrame(w, ops, r, canvasSize)

	check := func(stage string, wantBrand, wantAction bool) {
		t.Helper()
		gtx := layout.Context{
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(canvasSize),
			Ops:         ops,
			Source:      r.Source(),
		}
		gotBrand := gtx.Focused(&brandClick)
		gotAction := gtx.Focused(&actionClick)
		if gotBrand != wantBrand || gotAction != wantAction {
			t.Errorf("%s: focused(brand)=%v action=%v; want brand=%v action=%v",
				stage, gotBrand, gotAction, wantBrand, wantAction)
		}
	}

	check("after Focus(brand)", true, false)

	// Tab → expected stop is link 0 (neither brand nor action).
	r.MoveFocus(key.FocusForward)
	driveFrame(w, ops, r, canvasSize)
	check("Tab #1 (→ link 0)", false, false)

	// Tab → expected stop is link 1.
	r.MoveFocus(key.FocusForward)
	driveFrame(w, ops, r, canvasSize)
	check("Tab #2 (→ link 1)", false, false)

	// Tab → expected stop is action.
	r.MoveFocus(key.FocusForward)
	driveFrame(w, ops, r, canvasSize)
	check("Tab #3 (→ action)", false, true)

	// Now reverse the traversal. Shift+Tab from action: back to link 1.
	r.MoveFocus(key.FocusBackward)
	driveFrame(w, ops, r, canvasSize)
	check("Shift+Tab #1 (→ link 1)", false, false)

	// Shift+Tab → link 0.
	r.MoveFocus(key.FocusBackward)
	driveFrame(w, ops, r, canvasSize)
	check("Shift+Tab #2 (→ link 0)", false, false)

	// Shift+Tab → brand.
	r.MoveFocus(key.FocusBackward)
	driveFrame(w, ops, r, canvasSize)
	check("Shift+Tab #3 (→ brand)", true, false)
}

// TestNavbarLinkClickFiresOnClick verifies clicking a link invokes its
// OnClick callback. With PxPerDp=1, canvas 480×64, no brand, no actions, two
// links: each cell is its label plus (S3, Density.PaddingY) padding and an
// underline, separated by an S2 spacer, and the row is centred at canvas-mid.
// "Docs" and "Components" measure 57 and 105 px, so the row is 57+8+105 = 170
// wide and starts at x = 155; link 0 occupies x in [155, 212], y in [15, 49].
// A press/release at (180, 32) lands squarely inside it, clear of link 1.
//
// Before F4.4b the labels were blank, every cell collapsed to its 24 px of
// padding, and the coordinate was (224, 32) — which is now inside link 1.
func TestNavbarLinkClickFiresOnClick(t *testing.T) {
	var fired0, fired1 int
	props := navbar.Props{
		Links: []navbar.Link{
			{Label: linkLabels[0], OnClick: func(_ layout.Context) { fired0++ }},
			{Label: linkLabels[1], OnClick: func(_ layout.Context) { fired1++ }},
		},
		Shaper: defaultShaper(t),
	}
	w := liveWidget(t, navbar.Navbar(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)

	// Two warm-up frames so the router has stable hit-test data for the
	// link clip areas before pointer events are queued.
	driveFrame(w, ops, r, canvasSize)
	driveFrame(w, ops, r, canvasSize)

	hit := f32.Pt(180, 32)
	r.Queue(
		pointer.Event{Kind: pointer.Press, Position: hit, Source: pointer.Touch},
		pointer.Event{Kind: pointer.Release, Position: hit, Source: pointer.Touch},
	)
	driveFrame(w, ops, r, canvasSize)

	if fired0 != 1 {
		t.Errorf("link 0 OnClick call count = %d, want 1", fired0)
	}
	if fired1 != 0 {
		t.Errorf("link 1 OnClick spuriously fired %d time(s)", fired1)
	}
}

// densityTheme returns a theme whose density is d, with sharp corners
// for golden determinism — the E1.4 injection idiom, mirroring prism's
// density tests.
func densityTheme(d tokens.Density) theme.Theme {
	th := theme.Default()
	th.Density = rx.Of(d)
	th.Radius = rx.Of(tokens.RadiusScale{})
	return th
}

// barHeight is the height the navbar actually draws at density d: a link
// cell is its label's line box plus the density's vertical padding above
// and below plus the Active underline, and the bar insets that row by the
// same PaddingY again.
//
// It is computed rather than taken from Density.ControlHeight on purpose.
// The compact golden used to be captured at the shell's navbar pin,
// ControlHeight + 2·PaddingY = 40 dp, on the theory that a bar wrapping
// ControlHeight controls is exactly that tall. It never was — a link cell
// carries a 2 dp underline the pin does not budget for — and F4.4d made the
// gap visible by giving the label its line box (20 dp) instead of its glyph
// ink (17 dp): the compact bar became exactly 40 dp of content in a 40 dp
// window, its bottom padding squeezed to nothing and the underline flush
// against the last pixel row. One more dp of line height and the golden
// would have pinned a clipped underline. Compute the row the way the
// component computes it, and the window cannot go stale that way again.
func barHeight(d tokens.Density, style tokens.TextStyle) int {
	cell := int(style.LineHeight) + 2*int(d.PaddingY) + navbarUnderlineDp
	return cell + 2*int(d.PaddingY)
}

// navbarUnderlineDp mirrors the unexported underlineDp in the navbar
// package: the thickness of the Active link's Primary indicator.
const navbarUnderlineDp = 2

// TestNavbarCompactGolden records or diffs the compact-density golden
// through the LIVE pipeline (the static Render path is frozen at
// tokens.Comfortable): the bar's vertical inset and the link padding
// drop to the Compact PaddingY (6 dp). The canvas is [barHeight] at
// Compact — 46 dp, not the shell's 40 dp pin; see barHeight for why.
func TestNavbarCompactGolden(t *testing.T) {
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	props := navbar.Props{
		Links: []navbar.Link{
			{Label: linkLabels[0], Active: true, OnClick: func(_ layout.Context) {}},
			{Label: linkLabels[1], OnClick: func(_ layout.Context) {}},
		},
		Shaper: defaultShaper(t),
	}
	w := liveWidget(t, navbar.Navbar(rx.Of(densityTheme(tokens.Compact)), props))
	h := barHeight(tokens.Compact, tokens.DefaultTypography.LabelLarge)
	golden.Render(t, "light-compact", image.Pt(canvasW, h), scene(w, lightBG))
}

// TestNavbarKeepsItsBottomPadding is the assertion the compact golden's
// window used to make by accident, and it is why the window is computed
// rather than assumed. In a canvas of [barHeight] the Active underline —
// the lowest thing the bar draws — must clear the bottom PaddingY, so the
// bar keeps the breathing room its own inset asks for and is nowhere near
// the edge it would be clipped at.
//
// The navbar fills its constraints, so its reported Dimensions can never
// report an overflow; only the pixels can. This reads them.
func TestNavbarKeepsItsBottomPadding(t *testing.T) {
	style := tokens.DefaultTypography.LabelLarge
	primary := tokens.DefaultLight.Primary

	for _, d := range []tokens.Density{tokens.Comfortable, tokens.Compact} {
		props := navbar.Props{
			Links:  []navbar.Link{{Label: linkLabels[0], Active: true, OnClick: func(_ layout.Context) {}}},
			Shaper: defaultShaper(t),
		}
		w := liveWidget(t, navbar.Navbar(rx.Of(densityTheme(d)), props))
		h := barHeight(d, style)
		img := golden.Capture(t, image.Pt(canvasW, h), scene(w, color.NRGBA{R: 240, G: 240, B: 240, A: 255}))

		lowest := -1
		for y := 0; y < h; y++ {
			for x := 0; x < canvasW; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if uint8(r>>8) == primary.R && uint8(g>>8) == primary.G && uint8(b>>8) == primary.B {
					lowest = y
					break
				}
			}
		}
		if lowest < 0 {
			t.Fatalf("density %+v: no Primary pixel in the bar; the Active underline did not draw, so this proves nothing", d)
		}
		if want := h - int(d.PaddingY); lowest >= want {
			t.Errorf("density %+v: the underline reaches row %d of a %d px bar, inside the %d dp bottom padding",
				d, lowest, h, int(d.PaddingY))
		}
	}
}
