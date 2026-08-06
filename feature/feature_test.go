package feature_test

import (
	"flag"
	"fmt"
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
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/cadence/feature"
	"github.com/vibrantgio/spectrum/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

const (
	canvasW, canvasH = 720, 320
	// scene leaves a small margin around the grid so the outer cells
	// retain breathing room from the canvas edge.
	marginPx = 16
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

// featureBody is the description every cell carries. It is long enough to
// wrap onto more than one line in a three-up cell, which is the whole point:
// the BodyMedium role's line height only shows in the gap between baselines,
// so a body that fits on one line would pin everything about the role except
// that (F4.4). ASCII only — both embedded faces carry every rune, so no
// machine-dependent fallback face can reach a stored image.
const featureBody = "Every token flows from one theme value, so a change lands everywhere at once."

// item returns an Item with the deterministic icon fill and real text. The
// labels were blank until F4.4, on the theory that font rasterisation was
// non-deterministic; F4.2 pinned the faces by configuration, and Latin text in
// Roboto rasterises identically on every machine.
func item(title string) feature.Item {
	return feature.Item{
		Icon:  iconFill(color.NRGBA{R: 60, G: 110, B: 200, A: 255}),
		Title: title,
		Body:  featureBody,
	}
}

// featureTitles names the cells in document order, so a six-item grid is
// legible as six distinct cells rather than six copies.
var featureTitles = []string{"Tokens", "Density", "Elevation", "Motion", "Contrast", "Focus"}

// items returns the first n cells.
func items(n int) []feature.Item {
	out := make([]feature.Item, n)
	for i := range out {
		out[i] = item(featureTitles[i])
	}
	return out
}

// TestFeatureGolden records or diffs the four Measurable goldens: the icon
// fills and grid geometry carry the structural differences between cases, and
// the titles and bodies carry the typography.
func TestFeatureGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	darkBG := color.NRGBA{R: 20, G: 20, B: 20, A: 255}

	three := items(3)
	two := items(2)
	six := items(6)

	cases := []struct {
		name    string
		colors  tokens.ColorTokens
		bg      color.NRGBA
		columns int
		items   []feature.Item
		size    image.Point
	}{
		{"light-3-up", tokens.DefaultLight, lightBG, 3, three, canvasSize},
		{"dark-3-up", tokens.DefaultDark, darkBG, 3, three, canvasSize},
		{"light-2-up", tokens.DefaultLight, lightBG, 2, two, canvasSize},
		// Two rows of real text do not fit the one-row canvas: with blank
		// labels a cell was an icon fill and nothing else, and 320 px was
		// generous. The taller canvas is what keeps the second row's bodies
		// on screen rather than cut off at the edge.
		{"light-6-items-3-up", tokens.DefaultLight, lightBG, 3, six, image.Pt(canvasW, 2*canvasH)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := feature.Props{Columns: tc.columns, Items: tc.items}
			w := feature.Render(shaper, props, tc.colors, tokens.Spacing, tokens.DefaultTypography)
			renderGolden(t, tc.name, tc.size, scene(w, tc.bg))
		})
	}
}

// TestFeatureColumnsDefaultsToThree confirms Columns=0 renders the same
// as Columns=3.
func TestFeatureColumnsDefaultsToThree(t *testing.T) {
	shaper := defaultShaper(t)
	bg := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	cells := items(3)

	zero := feature.Render(shaper, feature.Props{Columns: 0, Items: cells}, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography)
	three := feature.Render(shaper, feature.Props{Columns: 3, Items: cells}, tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography)

	a := capture(t, canvasSize, scene(zero, bg))
	b := capture(t, canvasSize, scene(three, bg))
	if a == nil || b == nil {
		return
	}
	if n := pixelDiff(a, b); n != 0 {
		t.Errorf("Columns=0 default-to-3 contract broken: %d pixel(s) differ from Columns=3", n)
	}
}

// ---- typography (F4.4) ----

// withBodyLineHeight returns a copy of the default typography whose BodyMedium
// role — the role the cell bodies draw in — is on a taller line box, and
// nothing else changed.
func withBodyLineHeight(lh float32) tokens.Typography {
	typo := tokens.DefaultTypography
	typo.BodyMedium.LineHeight = lh
	return typo
}

// featureLineHeightWidget renders the three-up grid with BodyMedium on the
// given line height, on the light theme.
func featureLineHeightWidget(t *testing.T, lh float32) layout.Widget {
	t.Helper()
	w := feature.Render(defaultShaper(t), feature.Props{Columns: 3, Items: items(3)},
		tokens.DefaultLight, tokens.Spacing, withBodyLineHeight(lh))
	return scene(w, color.NRGBA{R: 240, G: 240, B: 240, A: 255})
}

// TestFeatureLineHeightGolden is the org's only golden that pins a role's line
// height, and it lives here because this is where the property is observable.
//
// gioui.org/text spends the line height on the gap between baselines and
// nowhere else — calculateYOffsets baselines the first line at that line's own
// ascent — and widget.Label reports the glyph ink bounds as its size. So a
// MaxLines:1 label renders identically at any LineHeight, which is every
// control in prism and most of the ones here. feature's cell body wraps to
// three lines, so the role's line height is the distance between them, and a
// regression in it moves these pixels.
func TestFeatureLineHeightGolden(t *testing.T) {
	renderGolden(t, "light-3-up-tall-body-lines", canvasSize,
		featureLineHeightWidget(t, tokens.DefaultTypography.BodyMedium.LineHeight+12))
}

// TestFeatureLineHeightIsDetectable proves the golden above is an instrument
// and not decoration: raising only the BodyMedium role's line height has to
// change pixels. If the property were dropped between tokens.TextStyle and
// widget.Label the two renders would be identical, and this test — not a stale
// image — says so.
func TestFeatureLineHeightIsDetectable(t *testing.T) {
	base := capture(t, canvasSize, featureLineHeightWidget(t, tokens.DefaultTypography.BodyMedium.LineHeight))
	tall := capture(t, canvasSize, featureLineHeightWidget(t, tokens.DefaultTypography.BodyMedium.LineHeight+12))
	if base == nil || tall == nil {
		return // headless unavailable; capture called t.Skip
	}
	if n := pixelDiff(base, tall); n == 0 {
		t.Error("raising BodyMedium's line height changed no pixels; the role's line height never reaches the shaper")
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
	// A size change is a failure in its own right, and it has to be caught
	// here: once the bounds differ there is no pixel count to compare, and
	// pixelDiff refuses to invent one.
	if sb, ib := stored.Bounds(), img.Bounds(); sb != ib {
		actualPath := strings.TrimSuffix(path, ".png") + ".actual.png"
		_ = saveImage(actualPath, img)
		t.Fatalf("%q: size changed: golden is %dx%d, render is %dx%d (actual saved to %s)",
			name, sb.Dx(), sb.Dy(), ib.Dx(), ib.Dy(), actualPath)
	}
	if n := pixelDiff(stored, img); n > 0 {
		actualPath := strings.TrimSuffix(path, ".png") + ".actual.png"
		_ = saveImage(actualPath, img)
		t.Fatalf("%q: %d pixel(s) differ (actual saved to %s)", name, n, actualPath)
	}
}

// pixelDiff counts the pixels that differ between a and b, which must have equal
// bounds. It panics if they do not.
//
// The panic replaces a returned -1. There is no pixel count to report for two
// images of different shapes, and -1 read as "no difference" to every `n > 0`
// test — which is how a golden whose size had moved compared as a pass, here
// and across the whole organization. A caller for which a size change is a
// real outcome rather than a defect — the stored-golden comparison, and only
// it — must compare Bounds itself before calling.
func pixelDiff(a, b *image.RGBA) int {
	if a.Bounds() != b.Bounds() {
		panic(fmt.Sprintf("pixelDiff: images must have equal bounds, got %v and %v",
			a.Bounds(), b.Bounds()))
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
