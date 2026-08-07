package table_test

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"testing"

	"gioui.org/f32"
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
	"github.com/vibrantgio/prism/golden"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

// Body height fits ~9 rows of 36 px, so the visible-row bound used by
// the benchmark is well below the smallest dataset size.
const (
	viewW = 480
	viewH = 360
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

// rowNames is the text the middle column draws, one entry per row index. It
// and the headers were empty until F4.4b, on the theory that font
// rasterisation was non-deterministic; F4.2 pinned the faces by configuration
// and F4.3 moved every golden onto DeterministicShaper, so Latin text in
// Roboto rasterises identically on every machine. ASCII only, per F4.2 — no
// symbol reaches a stored image, and the sort chevron is a clip path the
// package draws itself.
var rowNames = []string{"Tokens", "Density", "Elevation", "Motion"}

// textCell renders column text through the package's own RenderTextCell, the
// helper a caller is meant to reach for — so the goldens now exercise the
// cell's own padding and clamping rather than a flat colour block.
func textCell(shaper *text.Shaper, f func(int) string) func(int) layout.Widget {
	return func(item int) layout.Widget {
		return table.RenderTextCell(shaper, tokens.DefaultLight, tokens.DefaultTypography.BodyMedium, f(item))
	}
}

// TestTableGolden records or diffs one golden per density through the
// LIVE pipeline (the static Render path is frozen at tokens.Comfortable).
// The sort chevron on the active column is a deterministic clip path; the
// headers draw in the LabelLarge role and the cells in BodyMedium. The two
// goldens differ only in the density snapshot: header and rows land at 36 dp
// pitch Comfortable, 28 dp Compact.
func TestTableGolden(t *testing.T) {
	shaper := defaultShaper(t)
	lightBG := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	size := image.Pt(360, 200)

	cols := []table.Column[int]{
		{Header: "ID", Width: unit.Dp(80), Sortable: true, Cell: textCell(shaper, func(i int) string { return strconv.Itoa(i + 1) })},
		{Header: "Name", Sortable: true, Cell: textCell(shaper, func(i int) string { return rowNames[i] })},
		{Header: "Steps", Width: unit.Dp(96), Cell: textCell(shaper, func(i int) string { return strconv.Itoa(4 * (i + 1)) })},
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
			golden.Render(t, tc.name, size, scene(w, lightBG))
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
