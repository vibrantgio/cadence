package hero_test

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

	"github.com/vibrantgio/cadence/hero"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 480, 240
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius keeps the goldens deterministic — anti-aliased
	// rounded corners and the eyebrow pill's Full radius both vary slightly
	// between GPU contexts, breaking pixel-exact diffs.
	sharpRadius = tokens.RadiusScale{}
)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

// fillRect is a sharp-edged solid widget used as a Visual stand-in. We
// avoid text in the Title/Subtitle/CTA labels so GPU font rasterisation
// drift across platforms does not break the goldens; structural variations
// (Visual presence, eyebrow pill, CTA backgrounds) still produce visibly
// distinct images.
func fillRect(c color.NRGBA, heightDp float32) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(heightDp))
		size := image.Pt(gtx.Constraints.Max.X, h)
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// scene renders w into a canvas-sized constraint over a flat background.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

// TestHeroGolden records or diffs the four Measurable goldens. Text labels
// are intentionally empty (or, for the eyebrow, a single space) so the
// goldens do not depend on GPU font rasterisation; structural variations —
// Visual slot presence, eyebrow pill, dual CTA backgrounds — are what
// distinguishes the cases.
func TestHeroGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}
	visual := fillRect(color.NRGBA{R: 60, G: 110, B: 200, A: 255}, 120)

	cases := []struct {
		name   string
		colors tokens.ColorTokens
		bg     color.NRGBA
		props  hero.Props
	}{
		{
			name:   "light-text-only",
			colors: tokens.DefaultLight,
			bg:     lightBG,
			props:  hero.Props{Shaper: shaper},
		},
		{
			name:   "dark-text-only",
			colors: tokens.DefaultDark,
			bg:     darkBG,
			props:  hero.Props{Shaper: shaper},
		},
		{
			name:   "light-with-visual",
			colors: tokens.DefaultLight,
			bg:     lightBG,
			props:  hero.Props{Visual: visual, Shaper: shaper},
		},
		{
			name:   "light-eyebrow-and-dual-cta",
			colors: tokens.DefaultLight,
			bg:     lightBG,
			props: hero.Props{
				Eyebrow:      " ",
				PrimaryCTA:   &hero.CTA{Label: ""},
				SecondaryCTA: &hero.CTA{Label: ""},
				Shaper:       shaper,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := hero.Render(shaper, tc.props, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestHeroVisualSlotShiftsLayout confirms that supplying a Visual moves the
// hero from a single-column layout into a two-column split — without a
// Visual, the right half of the canvas is empty; with a Visual the right
// half carries the Visual's pixels.
func TestHeroVisualSlotShiftsLayout(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	visual := fillRect(color.NRGBA{R: 60, G: 110, B: 200, A: 255}, 120)

	textOnly := hero.Render(shaper, hero.Props{Shaper: shaper}, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
	withVisual := hero.Render(shaper, hero.Props{Visual: visual, Shaper: shaper}, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)

	imgA := capture(t, canvasSize, scene(textOnly, bg))
	imgB := capture(t, canvasSize, scene(withVisual, bg))
	if imgA == nil || imgB == nil {
		return
	}
	if n := pixelDiff(imgA, imgB); n == 0 {
		t.Error("text-only and with-visual hero render identically; expected the Visual slot to introduce a two-column split")
	}
}

// TestHeroLightDarkDiffer confirms that swapping the colour token set
// changes the rendered output.
func TestHeroLightDarkDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	props := hero.Props{
		Eyebrow:      " ",
		PrimaryCTA:   &hero.CTA{Label: ""},
		SecondaryCTA: &hero.CTA{Label: ""},
		Shaper:       shaper,
	}
	light := hero.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
	dark := hero.Render(shaper, props, tokens.DefaultDark, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)

	imgLight := capture(t, canvasSize, scene(light, bg))
	imgDark := capture(t, canvasSize, scene(dark, bg))
	if imgLight == nil || imgDark == nil {
		return
	}
	if n := pixelDiff(imgLight, imgDark); n == 0 {
		t.Error("light and dark hero render identically; expected colour differences")
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
