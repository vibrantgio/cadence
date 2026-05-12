package pricing_test

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

	"github.com/vibrantgio/cadence/pricing"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 720, 280
	// scene leaves an S5-equivalent margin around the pricing row so
	// the row's outer cards retain breathing room from the canvas edge.
	marginPx = 20
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius keeps the goldens deterministic — anti-aliased
	// rounded corners and the "Popular" chip's Full radius both vary
	// slightly between GPU contexts, breaking pixel-exact diffs.
	sharpRadius = tokens.RadiusScale{}
)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
}

// scene renders w into a canvas-sized constraint over a flat background
// with a uniform margin so the outer cards do not touch the canvas edge.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.UniformInset(unit.Dp(float32(marginPx))).Layout(gtx, w)
	}
}

// emptyTier returns a Tier whose text fields are blank but whose
// structural slots (features, CTA) are populated. Text labels are
// intentionally empty so the goldens do not depend on GPU font
// rasterisation; the checkmark glyphs, CTA fill, and card chrome carry
// the structural differences instead.
func emptyTier(highlighted bool) pricing.Tier {
	return pricing.Tier{
		Features:    []string{"", "", ""},
		CTA:         &pricing.CTA{Label: ""},
		Highlighted: highlighted,
	}
}

// TestPricingGolden records or diffs the four Measurable goldens.
func TestPricingGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	three := []pricing.Tier{emptyTier(false), emptyTier(false), emptyTier(false)}
	threeHighlighted := []pricing.Tier{emptyTier(false), emptyTier(true), emptyTier(false)}
	single := []pricing.Tier{emptyTier(false)}

	cases := []struct {
		name   string
		colors tokens.ColorTokens
		bg     color.NRGBA
		tiers  []pricing.Tier
	}{
		{"light-three-tier", tokens.DefaultLight, lightBG, three},
		{"dark-three-tier", tokens.DefaultDark, darkBG, three},
		{"light-three-tier-highlighted", tokens.DefaultLight, lightBG, threeHighlighted},
		{"light-single-tier", tokens.DefaultLight, lightBG, single},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := pricing.Props{Tiers: tc.tiers, Shaper: shaper}
			w := pricing.Render(shaper, props, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestPricingHighlightDiffers confirms the Highlighted flag changes the
// rendered output. Two three-tier rows are rendered with the middle tier
// flipped; the resulting images must differ.
func TestPricingHighlightDiffers(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}

	plain := pricing.Props{Tiers: []pricing.Tier{emptyTier(false), emptyTier(false), emptyTier(false)}, Shaper: shaper}
	highlighted := pricing.Props{Tiers: []pricing.Tier{emptyTier(false), emptyTier(true), emptyTier(false)}, Shaper: shaper}

	a := capture(t, canvasSize, scene(pricing.Render(shaper, plain, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale), bg))
	b := capture(t, canvasSize, scene(pricing.Render(shaper, highlighted, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale), bg))
	if a == nil || b == nil {
		return
	}
	if n := pixelDiff(a, b); n == 0 {
		t.Error("plain and highlighted pricing render identically; expected the 2 dp Primary border and Popular chip to introduce differences")
	}
}

// TestPricingLightDarkDiffer confirms that swapping the colour token set
// changes the rendered output.
func TestPricingLightDarkDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	tiers := []pricing.Tier{emptyTier(false), emptyTier(true), emptyTier(false)}

	light := pricing.Render(shaper, pricing.Props{Tiers: tiers, Shaper: shaper}, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
	dark := pricing.Render(shaper, pricing.Props{Tiers: tiers, Shaper: shaper}, tokens.DefaultDark, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)

	imgLight := capture(t, canvasSize, scene(light, bg))
	imgDark := capture(t, canvasSize, scene(dark, bg))
	if imgLight == nil || imgDark == nil {
		return
	}
	if n := pixelDiff(imgLight, imgDark); n == 0 {
		t.Error("light and dark pricing render identically; expected colour differences")
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
