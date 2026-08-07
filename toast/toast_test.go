package toast_test

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"

	"github.com/vibrantgio/cadence/toast"
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/tokens"
)

const (
	canvasW, canvasH = 320, 240
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius. Anti-aliased rounded corners vary slightly
	// between GPU contexts, breaking determinism.
	sharpRadius = tokens.RadiusScale{}
)

// defaultShaper returns the shaper every golden here draws with: the default
// typography's faces pinned, system fonts off, so the stored images are the
// same on every machine. A golden test pins its faces with
// DeterministicShaper; application code takes the fallback Shaper. See
// AGENTS.md.
func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return tokens.DefaultTypography.DeterministicShaper()
}

// scene renders w over a flat background sized to the constraints.
func scene(w layout.Widget, bg color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// toastText is the message each level carries. Toast.Text was left empty
// until F4.4b, on the theory that font rasterisation was non-deterministic;
// F4.2 pinned the faces by configuration and F4.3 moved every golden onto
// DeterministicShaper, so Latin text in Roboto rasterises identically on every
// machine. ASCII only, per F4.2 — no symbol reaches a stored image.
func toastText(l toast.Level) string {
	switch l {
	case toast.Success:
		return "Workspace saved"
	case toast.Warning:
		return "Connection is slow"
	case toast.Error:
		return "Upload failed"
	default:
		return "Syncing tokens"
	}
}

// item returns one toast of the given level, carrying that level's message.
func item(id int64, l toast.Level) toast.Toast {
	return toast.Toast{ID: id, Level: l, Text: toastText(l)}
}

// TestStackGolden records or diffs the three Measurable goldens. Variant tint
// and stack ordering are the load-bearing visual signal and the text carries
// the LabelMedium role. The scenes
// composite over the theme's own Surface — the colour app panes are
// painted with — so a toast fill that stops separating from real app
// backgrounds fails the diff instead of hiding behind an arbitrary
// grey (the regression that shipped the ~1.2:1 Surface-on-Surface
// toast).
func TestStackGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := tokens.DefaultLight.Surface
	darkBG := tokens.DefaultDark.Surface

	cases := []struct {
		name   string
		props  toast.Props
		items  []toast.Toast
		colors tokens.ColorTokens
		bg     color.NRGBA
	}{
		{
			name:   "light-empty",
			props:  toast.Props{Position: toast.TopRight, Shaper: shaper},
			items:  nil,
			colors: tokens.DefaultLight,
			bg:     lightBG,
		},
		{
			name:  "light-three-stacked",
			props: toast.Props{Position: toast.TopRight, Shaper: shaper},
			items: []toast.Toast{
				item(1, toast.Info),
				item(2, toast.Success),
				item(3, toast.Warning),
			},
			colors: tokens.DefaultLight,
			bg:     lightBG,
		},
		{
			name:   "dark-warning-toast",
			props:  toast.Props{Position: toast.BottomRight, Shaper: shaper},
			items:  []toast.Toast{item(1, toast.Warning)},
			colors: tokens.DefaultDark,
			bg:     darkBG,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := toast.Render(shaper, tc.props, tc.items, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelMedium)
			golden.Render(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestStackEmptyAndPopulatedDiffer catches regressions where the
// populated branch silently no-ops.
func TestStackEmptyAndPopulatedDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	props := toast.Props{Position: toast.TopRight, Shaper: shaper}

	empty := toast.Render(shaper, props, nil, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelMedium)
	full := toast.Render(shaper, props, []toast.Toast{item(1, toast.Info)}, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelMedium)

	imgE := golden.Capture(t, canvasSize, scene(empty, bg))
	imgF := golden.Capture(t, canvasSize, scene(full, bg))
	if n := golden.PixelDiff(imgE, imgF); n == 0 {
		t.Error("empty and populated stacks render identically; expected the surface to appear when populated")
	}
}

// TestStackPositionAnchoring confirms that swapping Position relocates
// the rendered toast. A TopRight stack must differ pixel-wise from a
// BottomLeft stack with the same items.
func TestStackPositionAnchoring(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	items := []toast.Toast{item(1, toast.Info)}

	tr := toast.Render(shaper, toast.Props{Position: toast.TopRight, Shaper: shaper}, items, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelMedium)
	bl := toast.Render(shaper, toast.Props{Position: toast.BottomLeft, Shaper: shaper}, items, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypography.LabelMedium)

	imgTR := golden.Capture(t, canvasSize, scene(tr, bg))
	imgBL := golden.Capture(t, canvasSize, scene(bl, bg))
	if n := golden.PixelDiff(imgTR, imgBL); n == 0 {
		t.Error("TopRight and BottomLeft stacks render identically; expected corner anchoring")
	}
}
