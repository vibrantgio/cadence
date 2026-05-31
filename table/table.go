// Package table provides the Cadence Table pattern: a sortable, filterable,
// virtualised data table. Body rows are laid out only for the current
// viewport (O(visible) per frame) via prism/list, independent of how many
// rows the Items observable carries.
//
// Sort and filter are external transforms. The Items observable emits
// already-sorted, already-filtered slices; the table renders whatever it
// receives and surfaces header-click intent via OnSort. Filter UI is the
// consumer's responsibility (typically a textfield above the table whose
// changes re-emit a filtered Items slice). This keeps the table dumb and
// matches the Phase 4 Composition contract: no opaque runtime configuration,
// source is the spec, copy and modify as needed.
//
// Per-row widget state (editors, checkboxes, expanders) is preserved across
// sort/filter by wiring prism/keyed.Defer into a Column's Cell closure: the
// consumer captures a *keyed.Deferred[K, *WidgetState] in the rx.Defer scope
// holding the Items observable, and returns the same widget pointer for the
// same row key on every emission. The table itself stores no per-row state
// — every Column.Cell call is fresh.
package table

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/pointer"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/prism/list"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

// Column declares one column of a Table. Cell is invoked once per visible
// row per frame; the returned Widget is constrained to the column's
// computed width and a fixed row height. Width is a hint: a non-zero
// value pins the column to that pixel width; zero flexes the column
// equally with other flexed columns. Sortable=true makes the header
// clickable and draws a sort chevron when this column is the active sort.
type Column[T any] struct {
	Header   string
	Cell     func(item T) layout.Widget
	Width    unit.Dp
	Sortable bool
}

// Sort describes the table's current display sort. Column is the
// zero-based column index, or -1 to indicate no active sort. Asc=true
// renders the chevron pointing up.
type Sort struct {
	Column int
	Asc    bool
}

// Props configures a Table[T]. Columns is read once per emission (cheap
// for typical column counts); Items is the data slice, already filtered
// and sorted by the consumer.
type Props[T any] struct {
	Columns []Column[T]

	// Items is the row data, already filtered and sorted. The table
	// renders only what it receives — sort/filter state is the
	// consumer's responsibility. Required.
	Items rx.Observable[[]T]

	// Sort drives the header chevron's display state. A nil Sort is
	// treated as a constant Sort{Column: -1} (no active sort).
	Sort rx.Observable[Sort]

	// OnSort is invoked when the user clicks a Sortable header. May be
	// nil. The consumer typically cycles None → Asc → Desc → None for
	// the clicked column and re-emits Sort and a re-sorted Items slice.
	OnSort func(gtx layout.Context, col int)

	// Shaper, if nil, defaults to a shaper backed by Go fonts. Created
	// once per subscription inside the rx.Defer scope so it survives
	// theme emissions for the lifetime of the Table instance.
	Shaper *text.Shaper
}

// Layout-affecting constants. Row and header heights are fixed so the
// body's vertical extent is deterministic and the prism/list viewport
// can serve constant-time look-aheads.
const (
	headerHDp     = 44
	rowHDp        = 36
	cellPadDp     = 12
	chevronSizeDp = 10
	dividerDp     = 1
	minColumnDp   = 64
)

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	typ     tokens.TypeScale
}

// Table returns an rx.Observable[layout.Widget] that emits a new widget
// whenever a consumed theme token, Items, or Sort changes. Header clicks
// invoke OnSort; the body is laid out via prism/list.Layout so per-frame
// cost is O(visible-rows), not O(len(items)).
func Table[T any](th rx.Observable[theme.Theme], props Props[T]) rx.Observable[layout.Widget] {
	items := props.Items
	if items == nil {
		items = rx.Of[[]T](nil)
	}
	sort := props.Sort
	if sort == nil {
		sort = rx.Of(Sort{Column: -1})
	}
	tokensObs := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest3(t.Color, t.Spacing, t.Type),
			func(n rx.Tuple3[tokens.ColorTokens, tokens.SpacingScale, tokens.TypeScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, typ: n.Third}
			},
		)
	})
	inputs := rx.CombineLatest3(tokensObs, items, sort)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}
		state := list.NewState()
		clicks := make([]widget.Clickable, len(props.Columns))
		return rx.Map(inputs, func(n rx.Tuple3[resolvedTokens, []T, Sort]) layout.Widget {
			tok, rows, sk := n.First, n.Second, n.Third
			return func(gtx layout.Context) layout.Dimensions {
				processHeaderClicks(gtx, props.Columns, clicks, props.OnSort)
				return drawTable(gtx, shaper, props.Columns, rows, sk, state, clicks, tok)
			}
		})
	})
}

// Render produces a layout.Widget for a table with a fixed dataset and
// pre-resolved tokens. Intended for golden-image testing and static
// demonstrations; production code should use Table.
func Render[T any](
	shaper *text.Shaper,
	columns []Column[T],
	items []T,
	sk Sort,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	tok := resolvedTokens{color: colors, spacing: sp, typ: ts}
	state := list.NewState()
	return func(gtx layout.Context) layout.Dimensions {
		return drawTable(gtx, shaper, columns, items, sk, state, nil, tok)
	}
}

// processHeaderClicks drains pending click events for each header. Only
// columns marked Sortable participate; non-sortable columns have a nil
// click in the slice so any pointer event is ignored.
func processHeaderClicks[T any](
	gtx layout.Context,
	columns []Column[T],
	clicks []widget.Clickable,
	onSort func(gtx layout.Context, col int),
) {
	for i := range columns {
		if !columns[i].Sortable {
			continue
		}
		if clicks[i].Clicked(gtx) && onSort != nil {
			onSort(gtx, i)
		}
	}
}

// drawTable renders the full table: header row + virtualised body. Width
// is partitioned across columns once per frame (O(cols), independent of
// row count); the body is laid out via prism/list so only viewport-
// visible rows incur per-row cost.
func drawTable[T any](
	gtx layout.Context,
	shaper *text.Shaper,
	columns []Column[T],
	items []T,
	sk Sort,
	state *list.State,
	clicks []widget.Clickable,
	tok resolvedTokens,
) layout.Dimensions {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, tok.color.Surface, clip.Rect{Max: size}.Op())

	widths := columnWidths(gtx, columns, size.X)
	headerH := gtx.Dp(unit.Dp(headerHDp))
	if headerH > size.Y {
		headerH = size.Y
	}

	// Header row.
	hSize := image.Pt(size.X, headerH)
	hStack := op.Offset(image.Point{}).Push(gtx.Ops)
	hGtx := gtx
	hGtx.Constraints = layout.Exact(hSize)
	drawHeaderRow(hGtx, shaper, columns, widths, sk, clicks, tok)
	hStack.Pop()

	// Body.
	bodyY := headerH
	bodyH := size.Y - bodyY
	if bodyH <= 0 {
		return layout.Dimensions{Size: size}
	}
	bStack := op.Offset(image.Pt(0, bodyY)).Push(gtx.Ops)
	bGtx := gtx
	bGtx.Constraints = layout.Exact(image.Pt(size.X, bodyH))
	list.Layout(bGtx, state, items, func(rGtx layout.Context, item T) layout.Dimensions {
		return drawRow(rGtx, columns, widths, item, tok)
	})
	bStack.Pop()

	return layout.Dimensions{Size: size}
}

// columnWidths resolves the per-column pixel widths. Explicit Width
// values are honoured; zero-width columns share the remainder equally.
// Returns a slice of length len(columns); element widths sum to totalW
// (modulo integer rounding).
func columnWidths[T any](gtx layout.Context, columns []Column[T], totalW int) []int {
	n := len(columns)
	out := make([]int, n)
	if n == 0 || totalW <= 0 {
		return out
	}
	minW := gtx.Dp(unit.Dp(minColumnDp))
	used := 0
	flexed := 0
	for i := range columns {
		if columns[i].Width > 0 {
			w := gtx.Dp(columns[i].Width)
			if w < minW {
				w = minW
			}
			out[i] = w
			used += w
		} else {
			flexed++
		}
	}
	remaining := totalW - used
	if flexed > 0 {
		if remaining < flexed*minW {
			remaining = flexed * minW
		}
		share := remaining / flexed
		extra := remaining - share*flexed
		for i := range columns {
			if columns[i].Width == 0 {
				w := share
				if extra > 0 {
					w++
					extra--
				}
				out[i] = w
			}
		}
	}
	return out
}

// drawHeaderRow renders the bold-weight header labels with optional sort
// chevrons and clickable hit areas for sortable columns. The trailing
// divider line marks the boundary between the header and the body.
func drawHeaderRow[T any](
	gtx layout.Context,
	shaper *text.Shaper,
	columns []Column[T],
	widths []int,
	sk Sort,
	clicks []widget.Clickable,
	tok resolvedTokens,
) layout.Dimensions {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, tok.color.SurfaceVariant, clip.Rect{Max: size}.Op())

	x := 0
	for i, col := range columns {
		w := widths[i]
		if w <= 0 {
			continue
		}
		st := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		cellGtx := gtx
		cellGtx.Constraints = layout.Exact(image.Pt(w, size.Y))
		drawHeaderCell(cellGtx, shaper, col, i == sk.Column, sk.Asc, clickFor(clicks, i, col.Sortable), tok)
		st.Pop()
		x += w
	}

	divH := gtx.Dp(unit.Dp(dividerDp))
	if divH < 1 {
		divH = 1
	}
	divRect := image.Rect(0, size.Y-divH, size.X, size.Y)
	paint.FillShape(gtx.Ops, tok.color.Outline, clip.Rect(divRect).Op())

	return layout.Dimensions{Size: size}
}

// drawHeaderCell renders one header label + optional sort chevron inside
// a fixed-size column box, wiring a Clickable if the column is sortable.
func drawHeaderCell[T any](
	gtx layout.Context,
	shaper *text.Shaper,
	col Column[T],
	active bool,
	asc bool,
	click *widget.Clickable,
	tok resolvedTokens,
) layout.Dimensions {
	size := gtx.Constraints.Max
	padH := gtx.Dp(unit.Dp(cellPadDp))

	inner := func(gtx layout.Context) layout.Dimensions {
		// Label.
		labelMaxW := size.X - 2*padH
		if active && col.Sortable {
			labelMaxW -= gtx.Dp(unit.Dp(chevronSizeDp)) + padH/2
		}
		if labelMaxW > 0 {
			labelGtx := gtx
			labelGtx.Constraints.Min = image.Point{}
			labelGtx.Constraints.Max.X = labelMaxW
			labelGtx.Constraints.Max.Y = size.Y

			mColor := op.Record(gtx.Ops)
			paint.ColorOp{Color: tok.color.OnSurfaceVariant}.Add(gtx.Ops)
			material := mColor.Stop()

			mLabel := op.Record(gtx.Ops)
			wl := widget.Label{MaxLines: 1}
			labelDims := wl.Layout(
				labelGtx,
				shaper,
				font.Font{Weight: font.Bold},
				unit.Sp(tok.typ.LabelLarge),
				col.Header,
				material,
			)
			labelCall := mLabel.Stop()

			offY := (size.Y - labelDims.Size.Y) / 2
			if offY < 0 {
				offY = 0
			}
			st := op.Offset(image.Pt(padH, offY)).Push(gtx.Ops)
			labelCall.Add(gtx.Ops)
			st.Pop()
		}

		if active && col.Sortable {
			chev := gtx.Dp(unit.Dp(chevronSizeDp))
			cx := size.X - padH - chev/2
			cy := size.Y / 2
			drawSortChevron(gtx, cx, cy, chev, tok.color.OnSurfaceVariant, asc)
		}

		return layout.Dimensions{Size: size}
	}

	gtx.Constraints = layout.Exact(size)
	if click == nil {
		return inner(gtx)
	}
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.LabelOp(col.Header).Add(gtx.Ops)
		semantic.EnabledOp(true).Add(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
		return inner(gtx)
	})
}

// drawRow renders one body row by invoking each column's Cell closure
// inside a fixed-size cell box, then painting the bottom divider line.
// rowH is computed from rowHDp here, not from the cell's intrinsic size,
// so per-row layout cost stays bounded regardless of cell content.
func drawRow[T any](
	gtx layout.Context,
	columns []Column[T],
	widths []int,
	item T,
	tok resolvedTokens,
) layout.Dimensions {
	rowH := gtx.Dp(unit.Dp(rowHDp))
	totalW := gtx.Constraints.Max.X
	rowSize := image.Pt(totalW, rowH)

	x := 0
	for i, col := range columns {
		w := widths[i]
		if w <= 0 {
			continue
		}
		st := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		cellGtx := gtx
		cellGtx.Constraints = layout.Exact(image.Pt(w, rowH))
		if col.Cell != nil {
			cw := col.Cell(item)
			if cw != nil {
				cw(cellGtx)
			}
		}
		st.Pop()
		x += w
	}

	divH := gtx.Dp(unit.Dp(dividerDp))
	if divH < 1 {
		divH = 1
	}
	divRect := image.Rect(0, rowH-divH, totalW, rowH)
	paint.FillShape(gtx.Ops, tok.color.Outline, clip.Rect(divRect).Op())

	return layout.Dimensions{Size: rowSize}
}

// RenderTextCell renders a single line of OnSurface-coloured text within
// the cell's allocated rectangle, with horizontal padding equal to
// cellPadDp. Exported so consumers building their own Cell closures can
// match the table's stock text style.
func RenderTextCell(
	shaper *text.Shaper,
	colors tokens.ColorTokens,
	ts tokens.TypeScale,
	s string,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		padH := gtx.Dp(unit.Dp(cellPadDp))
		labelMaxW := size.X - 2*padH
		if labelMaxW <= 0 {
			return layout.Dimensions{Size: size}
		}
		labelGtx := gtx
		labelGtx.Constraints.Min = image.Point{}
		labelGtx.Constraints.Max.X = labelMaxW
		labelGtx.Constraints.Max.Y = size.Y

		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: colors.OnSurface}.Add(gtx.Ops)
		material := mColor.Stop()

		mLabel := op.Record(gtx.Ops)
		wl := widget.Label{MaxLines: 1}
		labelDims := wl.Layout(labelGtx, shaper, font.Font{}, unit.Sp(ts.BodyMedium), s, material)
		labelCall := mLabel.Stop()

		offY := (size.Y - labelDims.Size.Y) / 2
		if offY < 0 {
			offY = 0
		}
		st := op.Offset(image.Pt(padH, offY)).Push(gtx.Ops)
		labelCall.Add(gtx.Ops)
		st.Pop()
		return layout.Dimensions{Size: size}
	}
}

// drawSortChevron paints a small filled triangle centred at (cx, cy)
// pointing up (asc) or down (desc).
func drawSortChevron(gtx layout.Context, cx, cy, sz int, c color.NRGBA, asc bool) {
	half := float32(sz) / 2
	fcx := float32(cx)
	fcy := float32(cy)
	var p clip.Path
	p.Begin(gtx.Ops)
	if asc {
		p.MoveTo(f32.Pt(fcx-half, fcy+half/2))
		p.LineTo(f32.Pt(fcx, fcy-half/2))
		p.LineTo(f32.Pt(fcx+half, fcy+half/2))
	} else {
		p.MoveTo(f32.Pt(fcx-half, fcy-half/2))
		p.LineTo(f32.Pt(fcx, fcy+half/2))
		p.LineTo(f32.Pt(fcx+half, fcy-half/2))
	}
	p.Close()
	paint.FillShape(gtx.Ops, c, clip.Outline{Path: p.End()}.Op())
}

func clickFor(clicks []widget.Clickable, i int, sortable bool) *widget.Clickable {
	if !sortable || i < 0 || i >= len(clicks) {
		return nil
	}
	return &clicks[i]
}
