package feature_test

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

	"github.com/vibrantgio/cadence/feature"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 720, 320
	// scene leaves a small margin around the grid so the outer cells
	// retain breathing room from the canvas edge.
	marginPx = 16
)

var canvasSize = image.Pt(canvasW, canvasH)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

// scene renders w into a canvas-sized constraint over a flat background
// with a uniform margin so the outer cells do not touch the canvas edge.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.UniformInset(unit.Dp(float32(marginPx))).Layout(gtx, w)
	}
}

// iconFill returns a solid-colour widget that fills its (icon-cell-sized)
// constraints. Used as an Icon stand-in so the goldens carry a
// deterministic structural marker for the icon slot without depending on
// any vector asset.
func iconFill(c color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// emptyItem returns an Item whose icon is the deterministic fill widget
// but whose Title/Body labels are blank. Blank labels keep the goldens
// independent of GPU font rasterisation while the icon block still
// distinguishes column layouts.
func emptyItem() feature.Item {
	return feature.Item{
		Icon: iconFill(color.NRGBA{R: 60, G: 110, B: 200, A: 255}),
	}
}

// TestFeatureGolden records or diffs the four Measurable goldens. Text
// labels are intentionally empty so the goldens do not depend on GPU font
// rasterisation; the icon fills and the grid geometry carry the structural
// differences between cases.
func TestFeatureGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	three := []feature.Item{emptyItem(), emptyItem(), emptyItem()}
	two := []feature.Item{emptyItem(), emptyItem()}
	six := []feature.Item{
		emptyItem(), emptyItem(), emptyItem(),
		emptyItem(), emptyItem(), emptyItem(),
	}

	cases := []struct {
		name    string
		colors  tokens.ColorTokens
		bg      color.NRGBA
		columns int
		items   []feature.Item
	}{
		{"light-3-up", tokens.DefaultLight, lightBG, 3, three},
		{"dark-3-up", tokens.DefaultDark, darkBG, 3, three},
		{"light-2-up", tokens.DefaultLight, lightBG, 2, two},
		{"light-6-items-3-up", tokens.DefaultLight, lightBG, 3, six},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := feature.Props{Columns: tc.columns, Items: tc.items}
			w := feature.Render(shaper, props, tc.colors, tokens.Spacing, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestFeatureColumnsDefaultsToThree confirms Columns=0 renders the same
// as Columns=3.
func TestFeatureColumnsDefaultsToThree(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	items := []feature.Item{emptyItem(), emptyItem(), emptyItem()}

	zero := feature.Render(shaper, feature.Props{Columns: 0, Items: items}, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypeScale)
	three := feature.Render(shaper, feature.Props{Columns: 3, Items: items}, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypeScale)

	a := capture(t, canvasSize, scene(zero, bg))
	b := capture(t, canvasSize, scene(three, bg))
	if a == nil || b == nil {
		return
	}
	if n := pixelDiff(a, b); n != 0 {
		t.Errorf("Columns=0 default-to-3 contract broken: %d pixel(s) differ from Columns=3", n)
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
