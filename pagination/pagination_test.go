package pagination_test

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

	"github.com/vibrantgio/cadence/pagination"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 360, 48
)

var canvasSize = image.Pt(canvasW, canvasH)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

// scene renders w into a canvas-sized constraint over a flat background.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// TestPaginationGolden records or diffs the three Measurable goldens.
// Page labels are empty strings inside the buttons (set by the renderer
// internally as "1".."5") only via the digit glyph itself — GPU font
// rasterisation differs slightly across platforms. To keep the goldens
// deterministic we use a zero radius scale (sharp corners) and the
// distinguishing signal across goldens is which cell carries the Primary
// fill: page-1-of-5 highlights the first cell, page-3-of-5 highlights the
// third, and the light vs dark variants flip the colour palette.
func TestPaginationGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}
	sharpRadius := tokens.RadiusScale{}

	cases := []struct {
		name      string
		page      int
		pageCount int
		colors    tokens.ColorTokens
		bg        color.NRGBA
	}{
		{"light-page-1-of-5", 1, 5, tokens.DefaultLight, lightBG},
		{"light-page-3-of-5", 3, 5, tokens.DefaultLight, lightBG},
		{"dark-page-3-of-5", 3, 5, tokens.DefaultDark, darkBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := pagination.Props{Page: tc.page, PageCount: tc.pageCount, Shaper: shaper}
			w := pagination.Render(shaper, props, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestPaginationCurrentPagePositionDiffers confirms that moving the active
// page shifts the Primary-coloured cell to a different x position. Guards
// against a regression in which all cells render with identical styling.
func TestPaginationCurrentPagePositionDiffers(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	sharpRadius := tokens.RadiusScale{}

	render := func(page int) *image.RGBA {
		props := pagination.Props{Page: page, PageCount: 5, Shaper: shaper}
		w := pagination.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
		return capture(t, canvasSize, scene(w, bg))
	}

	one := render(1)
	three := render(3)
	if one == nil || three == nil {
		return
	}
	if n := pixelDiff(one, three); n == 0 {
		t.Error("page-1-of-5 and page-3-of-5 render identically; expected the Primary-coloured cell to move")
	}
}

// TestPaginationLightDarkDiffer confirms swapping the colour token set
// changes the rendered output.
func TestPaginationLightDarkDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	sharpRadius := tokens.RadiusScale{}

	props := pagination.Props{Page: 3, PageCount: 5, Shaper: shaper}
	light := pagination.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
	dark := pagination.Render(shaper, props, tokens.DefaultDark, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)

	imgLight := capture(t, canvasSize, scene(light, bg))
	imgDark := capture(t, canvasSize, scene(dark, bg))
	if imgLight == nil || imgDark == nil {
		return
	}
	if n := pixelDiff(imgLight, imgDark); n == 0 {
		t.Error("light and dark render identically; expected token-pair colour differences")
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
