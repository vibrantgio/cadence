package table_test

import (
	"fmt"
	"image"
	"strconv"
	"testing"

	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/vibrantgio/cadence/table"
	"github.com/vibrantgio/prism/tokens"
)

// BenchmarkTableLayout is the Measurable for G4.4: it proves the table's
// per-frame layout cost is O(visible-rows), not O(len(items)). The body
// height is fixed at 360 px and the row height at 36 dp/px, so the
// viewport fits ~9 rows. ns/op should stay roughly flat as N grows from
// 10 to 10000; if the table accidentally iterates rows during header or
// width computation, this benchmark immediately surfaces O(N) growth.
func BenchmarkTableLayout(b *testing.B) {
	shaper := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	cols := []table.Column[row]{
		{Header: "ID", Width: unit.Dp(80), Sortable: true, Cell: idCell(shaper)},
		{Header: "Name", Sortable: true, Cell: nameCell(shaper)},
		{Header: "Value", Width: unit.Dp(120), Cell: valueCell(shaper)},
	}
	canvas := image.Pt(viewW, viewH)

	for _, n := range []int{10, 100, 1000, 10000} {
		items := makeRows(n)
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			w := table.Render(shaper, cols, items, table.Sort{Column: 1, Asc: true},
				tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypeScale)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var ops op.Ops
				gtx := layout.Context{
					Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
					Constraints: layout.Exact(canvas),
					Ops:         &ops,
				}
				w(gtx)
			}
		})
	}
}

// TestBenchmarkConfirmsConstantCost is a coarse runtime guard for the
// O(visible-rows) claim: it measures ns/op at N=100 and N=10000 directly
// and fails if the 10000-row layout exceeds 4× the 100-row layout. The
// 4× tolerance is generous enough that GC and timer jitter don't
// produce false positives, but tight enough that a real O(N) regression
// (which would show ~100× growth) trips it.
func TestBenchmarkConfirmsConstantCost(t *testing.T) {
	shaper := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	cols := []table.Column[row]{
		{Header: "ID", Width: unit.Dp(80), Sortable: true, Cell: idCell(shaper)},
		{Header: "Name", Sortable: true, Cell: nameCell(shaper)},
		{Header: "Value", Width: unit.Dp(120), Cell: valueCell(shaper)},
	}

	measure := func(n int) int64 {
		items := makeRows(n)
		w := table.Render(shaper, cols, items, table.Sort{Column: 1, Asc: true},
			tokens.DefaultLight, tokens.Spacing, tokens.DefaultTypeScale)
		result := testing.Benchmark(func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var ops op.Ops
				gtx := layout.Context{
					Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
					Constraints: layout.Exact(image.Pt(viewW, viewH)),
					Ops:         &ops,
				}
				w(gtx)
			}
		})
		return result.NsPerOp()
	}

	small := measure(100)
	large := measure(10000)
	if small <= 0 {
		t.Skip("benchmark produced no samples")
	}
	const maxRatio = 4
	if large > small*maxRatio {
		t.Errorf("layout cost not O(visible): N=100 → %d ns/op, N=10000 → %d ns/op (%.1f×); want ≤ %d×",
			small, large, float64(large)/float64(small), maxRatio)
	}
}

// row is a minimal row type carrying three columns of varying types.
type row struct {
	ID    int
	Name  string
	Value float64
}

func makeRows(n int) []row {
	out := make([]row, n)
	for i := range out {
		out[i] = row{ID: i, Name: "row-" + strconv.Itoa(i), Value: float64(i) * 1.5}
	}
	return out
}

func idCell(shaper *text.Shaper) func(row) layout.Widget {
	return func(r row) layout.Widget {
		return table.RenderTextCell(shaper, tokens.DefaultLight, tokens.DefaultTypeScale,strconv.Itoa(r.ID))
	}
}

func nameCell(shaper *text.Shaper) func(row) layout.Widget {
	return func(r row) layout.Widget {
		return table.RenderTextCell(shaper, tokens.DefaultLight, tokens.DefaultTypeScale,r.Name)
	}
}

func valueCell(shaper *text.Shaper) func(row) layout.Widget {
	return func(r row) layout.Widget {
		return table.RenderTextCell(shaper, tokens.DefaultLight, tokens.DefaultTypeScale,strconv.FormatFloat(r.Value, 'f', 2, 64))
	}
}
