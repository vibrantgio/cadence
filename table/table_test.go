package table_test

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	gioinput "gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/cadence/table"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

var goldenUpdate = flag.Bool("golden.update", false, "overwrite golden images with current output")

// Body height fits ~9 rows of 36 px, so the visible-row bound used by
// the benchmark is well below the smallest dataset size.
const (
	viewW = 480
	viewH = 360
)

func defaultShaper(t *testing.T) *text.Shaper {
	t.Helper()
	return tokens.DefaultTypography.Shaper()
}

func liveWidget(t *testing.T, obs rx.Observable[layout.Widget]) layout.Widget {
	t.Helper()
	var w layout.Widget
	if err := obs.Subscribe(context.Background(), func(next layout.Widget, _ error, done bool) {
		if !done && next != nil {
			w = next
		}
	}).Wait(); err != nil {
		t.Fatalf("Table subscribe: %v", err)
	}
	if w == nil {
		t.Fatal("Table did not emit an initial widget")
	}
	return w
}

func driveFrame(w layout.Widget, ops *op.Ops, r *gioinput.Router, size image.Point) {
	ops.Reset()
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(size),
		Ops:         ops,
		Source:      r.Source(),
	}
	w(gtx)
	r.Frame(ops)
}

// TestRowFnCalledOnlyForVisibleItems is the direct counter-based proof
// that the table delegates body iteration to prism/list and therefore
// only invokes each Column.Cell for viewport-visible rows. With a 360 px
// body height and 36 dp row height we expect ~9 visible rows; the safe
// upper bound for a 10 000-row dataset is well under 50 — anything
// approaching N would indicate the table is iterating rows itself.
func TestRowFnCalledOnlyForVisibleItems(t *testing.T) {
	shaper := defaultShaper(t)
	const n = 10000
	items := make([]int, n)
	for i := range items {
		items[i] = i
	}

	var calls int
	cols := []table.Column[int]{
		{
			Header: "ID",
			Cell: func(item int) layout.Widget {
				calls++
				return table.RenderTextCell(shaper, tokens.DefaultLight, tokens.DefaultTypography.BodyMedium, strconv.Itoa(item))
			},
		},
	}

	w := table.Render(shaper, cols, items, table.Sort{Column: -1},
		tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypography.LabelLarge, tokens.Comfortable)
	var ops op.Ops
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(viewW, viewH)),
		Ops:         &ops,
	}
	w(gtx)

	const maxVisible = 50
	if calls > maxVisible {
		t.Errorf("Cell called %d times for N=%d (body %dpx); want ≤ %d (O(visible))",
			calls, n, viewH, maxVisible)
	}
	if calls == 0 {
		t.Error("Cell never called; table should render at least one row")
	}
}

// TestHeaderClickFiresOnSort drives a pointer Press+Release against the
// Sortable header (column 0) and confirms OnSort fires with column index
// 0. With PxPerDp=1 and viewW=480, the table partitions [0, 480] into
// three columns: ID (Width=80), Name (flexed, width = 480-80-120 = 280),
// Value (Width=120). Header row occupies y∈[0, 36] (the Comfortable
// control height, E1.4).
//
// A click at (40, 22) lands on the Sortable ID header.
// A click at (220, 22) lands on the Sortable Name header (column 1).
// A click at (420, 22) lands on the non-Sortable Value header — should
// not fire OnSort.
func TestHeaderClickFiresOnSort(t *testing.T) {
	shaper := defaultShaper(t)
	var calls []int
	cols := []table.Column[int]{
		{Header: "ID", Width: unit.Dp(80), Sortable: true, Cell: cellAs(shaper)},
		{Header: "Name", Sortable: true, Cell: cellAs(shaper)},
		{Header: "Value", Width: unit.Dp(120), Sortable: false, Cell: cellAs(shaper)},
	}
	props := table.Props[int]{
		Columns: cols,
		Items:   rx.Of([]int{1, 2, 3}),
		Shaper:  shaper,
		OnSort:  func(_ layout.Context, col int) { calls = append(calls, col) },
	}
	w := liveWidget(t, table.Table(rx.Of(theme.Default()), props))

	r := new(gioinput.Router)
	ops := new(op.Ops)
	driveFrame(w, ops, r, image.Pt(viewW, viewH))
	driveFrame(w, ops, r, image.Pt(viewW, viewH))

	clickAt := func(x, y float32) {
		hit := f32.Pt(x, y)
		r.Queue(
			pointer.Event{Kind: pointer.Press, Position: hit, Source: pointer.Touch},
			pointer.Event{Kind: pointer.Release, Position: hit, Source: pointer.Touch},
		)
		driveFrame(w, ops, r, image.Pt(viewW, viewH))
	}

	clickAt(40, 22)  // ID header (sortable, col 0)
	clickAt(220, 22) // Name header (sortable, col 1)
	clickAt(420, 22) // Value header (NOT sortable)

	want := []int{0, 1}
	if !equalInts(calls, want) {
		t.Fatalf("OnSort call sequence:\n got  %v\n want %v", calls, want)
	}
}

// TestNilItemsObservableRenders confirms a nil Items prop is rendered as
// an empty table rather than panicking. Guards the rx.Of[[]T](nil)
// fallback in Table.
func TestNilItemsObservableRenders(t *testing.T) {
	shaper := defaultShaper(t)
	cols := []table.Column[int]{{Header: "ID", Cell: cellAs(shaper)}}
	props := table.Props[int]{Columns: cols, Shaper: shaper}
	w := liveWidget(t, table.Table(rx.Of(theme.Default()), props))
	var ops op.Ops
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(viewW, viewH)),
		Ops:         &ops,
	}
	w(gtx)
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

// fillCell returns a Column.Cell that fills its cell box with a flat
// colour. Deterministic (no fonts), so density goldens diff on geometry
// alone: row pitch and header height are exactly ControlHeight.
func fillCell(c color.NRGBA) func(int) layout.Widget {
	return func(int) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, c, clip.Rect{Max: gtx.Constraints.Max}.Op())
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
	}
}

// TestTableGolden records or diffs one golden per density through the
// LIVE pipeline (the static Render path is frozen at tokens.Comfortable).
// Headers are empty to avoid GPU font rasterisation differences; the
// sort chevron on the active column is a deterministic clip path. The
// two goldens differ only in the density snapshot: header and rows land
// at 36 dp pitch Comfortable, 28 dp Compact.
func TestTableGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	size := image.Pt(360, 200)

	cols := []table.Column[int]{
		{Header: "", Width: unit.Dp(80), Sortable: true, Cell: fillCell(color.NRGBA{R: 0x33, G: 0x99, B: 0x66, A: 0xff})},
		{Header: "", Sortable: true, Cell: fillCell(color.NRGBA{R: 0x88, G: 0x55, B: 0x22, A: 0xff})},
		{Header: "", Width: unit.Dp(96), Cell: fillCell(color.NRGBA{R: 0x22, G: 0x55, B: 0x88, A: 0xff})},
	}

	cases := []struct {
		name    string
		density tokens.Density
	}{
		{"light-comfortable", tokens.Comfortable},
		{"light-compact", tokens.Compact},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := table.Props[int]{
				Columns: cols,
				Items:   rx.Of([]int{0, 1, 2, 3}),
				Sort:    rx.Of(table.Sort{Column: 0, Asc: true}),
				Shaper:  shaper,
			}
			w := liveWidget(t, table.Table(rx.Of(densityTheme(tc.density)), props))
			renderGolden(t, tc.name, size, scene(w, lightBG))
		})
	}
}

// scene renders w into a canvas-sized constraint over a flat background.
func scene(w layout.Widget, bgColor color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return w(gtx)
	}
}

func cellAs(shaper *text.Shaper) func(int) layout.Widget {
	return func(v int) layout.Widget {
		return table.RenderTextCell(shaper, tokens.DefaultLight, tokens.DefaultTypography.BodyMedium, strconv.Itoa(v))
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
