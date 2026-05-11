package alert_test

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

	"github.com/vibrantgio/cadence/alert"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 320, 96
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

// fillRect is a sharp-edged solid widget used as a Body stand-in. We avoid
// text in goldens because GPU font rasterisation is non-deterministic
// across platforms.
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

// TestAlertGolden records or diffs every variant × {light, dark} pair.
// The Measurable contract requires at minimum info-light, info-dark,
// warning-light, error-light; the full 4×2 matrix is recorded so cross-
// variant regressions surface immediately.
func TestAlertGolden(t *testing.T) {
	shaper := defaultShaper(t)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 32)

	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	cases := []struct {
		name    string
		variant alert.Variant
		colors  tokens.ColorTokens
		bg      color.NRGBA
	}{
		{"info-light", alert.Info, tokens.DefaultLight, lightBG},
		{"info-dark", alert.Info, tokens.DefaultDark, darkBG},
		{"success-light", alert.Success, tokens.DefaultLight, lightBG},
		{"success-dark", alert.Success, tokens.DefaultDark, darkBG},
		{"warning-light", alert.Warning, tokens.DefaultLight, lightBG},
		{"warning-dark", alert.Warning, tokens.DefaultDark, darkBG},
		{"error-light", alert.Error, tokens.DefaultLight, lightBG},
		{"error-dark", alert.Error, tokens.DefaultDark, darkBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := alert.Props{
				Variant: tc.variant,
				// Empty Title avoids non-deterministic font rasterisation.
				Body:   body,
				Shaper: shaper,
			}
			w := alert.Render(shaper, props, tc.colors, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.bg))
		})
	}
}

// TestAlertVariantsDiffer confirms each variant produces visibly distinct
// pixels in the same theme. Catches regressions where the Variant flag
// silently no-ops.
func TestAlertVariantsDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 32)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}

	render := func(v alert.Variant) *image.RGBA {
		props := alert.Props{Variant: v, Body: body, Shaper: shaper}
		w := alert.Render(shaper, props, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
		return capture(t, canvasSize, scene(w, bg))
	}

	variants := []struct {
		name string
		v    alert.Variant
	}{
		{"info", alert.Info},
		{"success", alert.Success},
		{"warning", alert.Warning},
		{"error", alert.Error},
	}
	imgs := make([]*image.RGBA, len(variants))
	for i, v := range variants {
		imgs[i] = render(v.v)
		if imgs[i] == nil {
			return
		}
	}
	for i := range variants {
		for j := i + 1; j < len(variants); j++ {
			if n := pixelDiff(imgs[i], imgs[j]); n == 0 {
				t.Errorf("%s and %s render identically; expected variant-specific accent", variants[i].name, variants[j].name)
			}
		}
	}
}

// TestAlertLightDarkDiffer confirms swapping the colour token set changes
// the rendered output.
func TestAlertLightDarkDiffer(t *testing.T) {
	shaper := defaultShaper(t)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 32)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	for _, v := range []alert.Variant{alert.Info, alert.Success, alert.Warning, alert.Error} {
		propsL := alert.Props{Variant: v, Body: body, Shaper: shaper}
		propsD := alert.Props{Variant: v, Body: body, Shaper: shaper}
		light := alert.Render(shaper, propsL, tokens.DefaultLight, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)
		dark := alert.Render(shaper, propsD, tokens.DefaultDark, tokens.Spacing, sharpRadius, tokens.DefaultTypeScale)

		imgLight := capture(t, canvasSize, scene(light, bg))
		imgDark := capture(t, canvasSize, scene(dark, bg))
		if imgLight == nil || imgDark == nil {
			return
		}
		if n := pixelDiff(imgLight, imgDark); n == 0 {
			t.Errorf("variant %v: light and dark render identically; expected colour differences", v)
		}
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
