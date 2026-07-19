package card_test

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/vibrantgio/cadence/card"
	"github.com/vibrantgio/prism/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 280, 200
	// The card draws into its full constraints. For the elevated variant
	// we leave a 16-px margin so the shadow strip has room to extend
	// outside the card's perimeter and remain visible in the golden.
	marginPx = 16
)

var (
	canvasSize = image.Pt(canvasW, canvasH)
	// Sharp corner radius. Anti-aliased rounded corners vary slightly
	// between GPU contexts, breaking determinism. Sharp edges still
	// exercise the fill colour, outline stroke, and shadow presence.
	sharpRadius = tokens.RadiusScale{}
)

// fillRect is a simple sharp-edged solid widget used as a slot stand-in.
// We avoid text in goldens because GPU font rasterisation is non-deterministic
// across platforms.
func fillRect(c color.NRGBA, heightDp float32) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(heightDp))
		size := image.Pt(gtx.Constraints.Max.X, h)
		paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	}
}

// scene renders w into a canvas-sized constraint. The optional margin
// leaves room around the widget for ornamental output (e.g., shadows
// extending outside the widget's nominal bounds).
func scene(w layout.Widget, margin int, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.UniformInset(unit.Dp(float32(margin))).Layout(gtx, w)
	}
}

// TestCardGolden records or diffs the four canonical card variants.
func TestCardGolden(t *testing.T) {
	header := fillRect(color.NRGBA{R: 60, G: 110, B: 200, A: 255}, 24)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 48)
	footer := fillRect(color.NRGBA{R: 120, G: 180, B: 120, A: 255}, 20)

	cases := []struct {
		name   string
		colors tokens.ColorTokens
		props  card.Props
		bg     color.NRGBA
		margin int
	}{
		{
			name:   "light-normal",
			colors: tokens.DefaultLight,
			props:  card.Props{Header: header, Body: body, Footer: footer},
			bg:     color.NRGBA{R: 240, G: 240, B: 240, A: 255},
			margin: 0,
		},
		{
			name:   "dark-normal",
			colors: tokens.DefaultDark,
			props:  card.Props{Header: header, Body: body, Footer: footer},
			bg:     color.NRGBA{R: 20, G: 20, B: 20, A: 255},
			margin: 0,
		},
		{
			name:   "light-header-only",
			colors: tokens.DefaultLight,
			props:  card.Props{Header: header},
			bg:     color.NRGBA{R: 240, G: 240, B: 240, A: 255},
			margin: 0,
		},
		{
			name:   "light-elevated",
			colors: tokens.DefaultLight,
			props:  card.Props{Header: header, Body: body, Footer: footer, Elevated: true},
			bg:     color.NRGBA{R: 240, G: 240, B: 240, A: 255},
			margin: marginPx,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := card.Render(tc.props, tc.colors, tokens.Spacing, sharpRadius)
			renderGolden(t, tc.name, canvasSize, scene(w, tc.margin, tc.bg))
		})
	}
}

// TestCardElevatedDiffersFromOutlined confirms the elevated variant
// produces visibly different pixels from the outlined variant. Catches
// regressions where the Elevated flag silently no-ops.
func TestCardElevatedDiffersFromOutlined(t *testing.T) {
	header := fillRect(color.NRGBA{R: 60, G: 110, B: 200, A: 255}, 24)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}

	outlined := card.Render(card.Props{Header: header}, tokens.DefaultLight, tokens.Spacing, sharpRadius)
	elevated := card.Render(card.Props{Header: header, Elevated: true}, tokens.DefaultLight, tokens.Spacing, sharpRadius)

	imgOut := capture(t, canvasSize, scene(outlined, marginPx, bg))
	imgElev := capture(t, canvasSize, scene(elevated, marginPx, bg))
	if imgOut == nil || imgElev == nil {
		return
	}
	if n := pixelDiff(imgOut, imgElev); n == 0 {
		t.Error("elevated and outlined cards render identically; expected shadow/outline difference")
	}
}

// TestCardLightDarkDiffer confirms that swapping the colour token set
// changes the rendered output.
func TestCardLightDarkDiffer(t *testing.T) {
	header := fillRect(color.NRGBA{R: 60, G: 110, B: 200, A: 255}, 24)
	body := fillRect(color.NRGBA{R: 200, G: 200, B: 200, A: 255}, 48)
	bg := color.NRGBA{R: 128, G: 128, B: 128, A: 255}

	light := card.Render(card.Props{Header: header, Body: body}, tokens.DefaultLight, tokens.Spacing, sharpRadius)
	dark := card.Render(card.Props{Header: header, Body: body}, tokens.DefaultDark, tokens.Spacing, sharpRadius)

	imgLight := capture(t, canvasSize, scene(light, 0, bg))
	imgDark := capture(t, canvasSize, scene(dark, 0, bg))
	if imgLight == nil || imgDark == nil {
		return
	}
	if n := pixelDiff(imgLight, imgDark); n == 0 {
		t.Error("light and dark cards render identically; expected colour differences")
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
