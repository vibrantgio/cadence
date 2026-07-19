package toast_test

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/font/gofont"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/cadence/toast"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 320, 240
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius. Anti-aliased rounded corners vary slightly
	// between GPU contexts, breaking determinism.
	sharpRadius = tokens.RadiusScale{}
)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

// scene renders w over a flat background sized to the constraints.
func scene(w layout.Widget, bg color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// TestStackGolden records or diffs the three Measurable goldens. Empty
// text avoids non-deterministic font rasterisation; variant tint and
// stack ordering are the load-bearing visual signal. The scenes
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
				{ID: 1, Level: toast.Info},
				{ID: 2, Level: toast.Success},
				{ID: 3, Level: toast.Warning},
			},
			colors: tokens.DefaultLight,
			bg:     lightBG,
		},
		{
			name:   "dark-warning-toast",
			props:  toast.Props{Position: toast.BottomRight, Shaper: shaper},
			items:  []toast.Toast{{ID: 1, Level: toast.Warning}},
			colors: tokens.DefaultDark,
			bg:     darkBG,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := toast.Render(shaper, tc.props, tc.items, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestStackEmptyAndPopulatedDiffer catches regressions where the
// populated branch silently no-ops.
func TestStackEmptyAndPopulatedDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	props := toast.Props{Position: toast.TopRight, Shaper: shaper}

	empty := toast.Render(shaper, props, nil, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
	full := toast.Render(shaper, props, []toast.Toast{{ID: 1, Level: toast.Info}}, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)

	imgE := capture(t, canvasSize, scene(empty, bg))
	imgF := capture(t, canvasSize, scene(full, bg))
	if imgE == nil || imgF == nil {
		return
	}
	if n := pixelDiff(imgE, imgF); n == 0 {
		t.Error("empty and populated stacks render identically; expected the surface to appear when populated")
	}
}

// TestStackPositionAnchoring confirms that swapping Position relocates
// the rendered toast. A TopRight stack must differ pixel-wise from a
// BottomLeft stack with the same items.
func TestStackPositionAnchoring(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	items := []toast.Toast{{ID: 1, Level: toast.Info}}

	tr := toast.Render(shaper, toast.Props{Position: toast.TopRight, Shaper: shaper}, items, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
	bl := toast.Render(shaper, toast.Props{Position: toast.BottomLeft, Shaper: shaper}, items, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)

	imgTR := capture(t, canvasSize, scene(tr, bg))
	imgBL := capture(t, canvasSize, scene(bl, bg))
	if imgTR == nil || imgBL == nil {
		return
	}
	if n := pixelDiff(imgTR, imgBL); n == 0 {
		t.Error("TopRight and BottomLeft stacks render identically; expected corner anchoring")
	}
}

// ---- golden harness (inlined; prism/internal/golden is not importable
// from outside the prism module tree) ----

func capture(t *testing.T, size image.Point, draw layout.Widget) *image.RGBA {
	t.Helper()
	w, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skipf("headless rendering not supported: %v", err)
		return nil
	}
	defer w.Release()

	var ops op.Ops
	gtx := layout.Context{
		Constraints: layout.Exact(size),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Ops:         &ops,
	}
	draw(gtx)
	if err := w.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := w.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
}

func renderGolden(t *testing.T, name string, size image.Point, draw layout.Widget) {
	t.Helper()
	img := capture(t, size, draw)
	if img == nil {
		return
	}
	path := filepath.Join("testdata", "golden", name+".png")

	if *goldenUpdate {
		if err := saveImage(path, img); err != nil {
			t.Fatalf("save %s: %v", path, err)
		}
		return
	}

	stored, err := loadImage(path)
	if os.IsNotExist(err) {
		t.Fatalf("%s not found; run go test -golden.update to create", path)
		return
	}
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
		return
	}
	if n := pixelDiff(stored, img); n > 0 {
		actualPath := strings.TrimSuffix(path, ".png") + ".actual.png"
		_ = saveImage(actualPath, img)
		t.Fatalf("%q: %d pixel(s) differ (actual saved to %s)", name, n, actualPath)
	}
}

func pixelDiff(a, b *image.RGBA) int {
	if a.Bounds() != b.Bounds() {
		return -1
	}
	bounds := a.Bounds()
	n := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			off := (y-bounds.Min.Y)*a.Stride + (x-bounds.Min.X)*4
			if a.Pix[off] != b.Pix[off] ||
				a.Pix[off+1] != b.Pix[off+1] ||
				a.Pix[off+2] != b.Pix[off+2] ||
				a.Pix[off+3] != b.Pix[off+3] {
				n++
			}
		}
	}
	return n
}

func saveImage(path string, img *image.RGBA) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	nrgba := &image.NRGBA{Pix: img.Pix, Stride: img.Stride, Rect: img.Rect}
	return png.Encode(f, nrgba)
}

func loadImage(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	decoded, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	switch v := decoded.(type) {
	case *image.RGBA:
		return v, nil
	case *image.NRGBA:
		return &image.RGBA{Pix: v.Pix, Stride: v.Stride, Rect: v.Rect}, nil
	default:
		bounds := decoded.Bounds()
		rgba := image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				rgba.Set(x, y, decoded.At(x, y))
			}
		}
		return rgba, nil
	}
}
