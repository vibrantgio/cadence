// Package tabs provides the Cadence Tabs pattern: a horizontal tab
// strip with a Primary-coloured underline on the selected tab, plus
// a content panel rendered below that shows the selected tab's content.
//
// The package follows the Phase 4 Composition contract: Tabs is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. Source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// Note on prism/button: the implementation plan suggested reusing
// prism/button for tab labels, but a Button renders with a Primary
// background fill, 6 dp corner radius, and 44 dp minimum height —
// none of which fit a tab strip. The labels here use the same
// widget.Clickable + custom label rendering pattern as cadence/navbar,
// which faced the same mismatch.
package tabs

import (
	"image"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/key"
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
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

// Tab is one entry in the tab strip. Content may be nil; a nil content
// renders as an empty content panel when this tab is selected.
type Tab struct {
	Label   string
	Content layout.Widget
}

// Props configures a Tabs instance.
type Props struct {
	Tabs []Tab

	// Selected drives which tab is rendered as selected. A nil Selected
	// defaults to a constant 0. Values outside [0, len(Tabs)) render as
	// "no tab selected" (no underline, empty content area).
	Selected rx.Observable[int]

	// OnSelect is invoked when the user changes the selection via click,
	// Arrow-Left/Right (wrapping), Home, or End. May be nil.
	OnSelect func(idx int)

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The
	// default shaper is created once per subscription inside the
	// rx.Defer scope, so it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

// Strip dimensions. The strip height fits a single-line label plus
// (S3, S2) padding plus the underline; a fixed value keeps the layout
// deterministic across goldens regardless of label content.
const (
	stripHDp    = 40
	underlineDp = 2
)

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	typ     tokens.TypeScale
}

// Tabs returns an rx.Observable[layout.Widget] that emits a new widget
// whenever a consumed theme token or the Selected observable changes.
// Per the WAI-ARIA tab pattern, focus follows selection: clicking a tab
// or pressing Arrow-Left/Right/Home/End moves both selection and focus.
func Tabs(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	selected := props.Selected
	if selected == nil {
		selected = rx.Of(0)
	}
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest3(t.Color, t.Spacing, t.Type),
			func(n rx.Tuple3[tokens.ColorTokens, tokens.SpacingScale, tokens.TypeScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, typ: n.Third}
			},
		)
	})
	inputs := rx.CombineLatest2(resolved, selected)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}
		clicks := make([]widget.Clickable, len(props.Tabs))
		return rx.Map(inputs, func(next rx.Tuple2[resolvedTokens, int]) layout.Widget {
			tok, sel := next.First, next.Second
			return func(gtx layout.Context) layout.Dimensions {
				processInput(gtx, props, clicks)
				return drawTabs(gtx, shaper, props, clicks, sel, tok.color, tok.spacing, tok.typ)
			}
		})
	})
}

// Render produces a layout.Widget for a tabs view with a fixed selected
// index and no event processing. Intended for golden-image testing and
// static demonstrations; production code should use Tabs.
func Render(
	shaper *text.Shaper,
	props Props,
	selected int,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return drawTabs(gtx, shaper, props, nil, selected, colors, sp, ts)
	}
}

// processInput drains click and arrow-key events for each tab. Per the
// WAI-ARIA tab pattern, every selection change also moves focus to the
// newly-selected tab so the next keyboard event is routed correctly.
func processInput(gtx layout.Context, props Props, clicks []widget.Clickable) {
	n := len(props.Tabs)
	if n == 0 {
		return
	}
	move := func(target int) {
		// Modular wrap: callers pass i-1 / i+1 / 0 / n-1; this normalises
		// negatives so wrapping at the ends needs no special case.
		target = ((target % n) + n) % n
		if props.OnSelect != nil {
			props.OnSelect(target)
		}
		gtx.Execute(key.FocusCmd{Tag: &clicks[target]})
	}
	for i := range props.Tabs {
		if clicks[i].Clicked(gtx) {
			move(i)
		}
		for {
			e, ok := gtx.Event(key.Filter{Focus: &clicks[i], Name: key.NameLeftArrow})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				move(i - 1)
			}
		}
		for {
			e, ok := gtx.Event(key.Filter{Focus: &clicks[i], Name: key.NameRightArrow})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				move(i + 1)
			}
		}
		for {
			e, ok := gtx.Event(key.Filter{Focus: &clicks[i], Name: key.NameHome})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				move(0)
			}
		}
		for {
			e, ok := gtx.Event(key.Filter{Focus: &clicks[i], Name: key.NameEnd})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				move(n - 1)
			}
		}
	}
}

func drawTabs(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	clicks []widget.Clickable,
	selected int,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Dimensions {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, colors.Surface, clip.Rect{Max: size}.Op())

	stripH := gtx.Dp(unit.Dp(stripHDp))
	if stripH > size.Y {
		stripH = size.Y
	}

	stripGtx := gtx
	stripGtx.Constraints = layout.Exact(image.Pt(size.X, stripH))
	drawStrip(stripGtx, shaper, props, clicks, selected, colors, sp, ts)

	if size.Y > stripH && selected >= 0 && selected < len(props.Tabs) && props.Tabs[selected].Content != nil {
		panelSize := image.Pt(size.X, size.Y-stripH)
		st := op.Offset(image.Pt(0, stripH)).Push(gtx.Ops)
		contentGtx := gtx
		contentGtx.Constraints = layout.Exact(panelSize)
		props.Tabs[selected].Content(contentGtx)
		st.Pop()
	}

	return layout.Dimensions{Size: size}
}

func drawStrip(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	clicks []widget.Clickable,
	selected int,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Dimensions {
	if len(props.Tabs) == 0 {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	children := make([]layout.FlexChild, 0, len(props.Tabs))
	for i := range props.Tabs {
		i := i
		children = append(children, layout.Rigid(tabCell(
			shaper, props.Tabs[i].Label, clickFor(clicks, i), i == selected,
			colors, sp, ts,
		)))
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx, children...)
}

func clickFor(clicks []widget.Clickable, i int) *widget.Clickable {
	if i >= len(clicks) {
		return nil
	}
	return &clicks[i]
}

// tabCell renders a single tab label centred inside (S3, S2) padding,
// with a strip-height cell. When selected, a Primary-coloured underline
// of underlineDp px is drawn along the cell's bottom edge. The cell
// width is at least 2×S3 so the underline is visible even when the
// label rasterises to zero width (e.g., in deterministic empty-label
// golden tests).
func tabCell(
	shaper *text.Shaper,
	label string,
	click *widget.Clickable,
	selected bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		stripH := gtx.Constraints.Max.Y
		padH := gtx.Dp(unit.Dp(sp.S3))
		underlineH := gtx.Dp(unit.Dp(underlineDp))

		inner := func(gtx layout.Context) layout.Dimensions {
			labelGtx := gtx
			labelGtx.Constraints.Min = image.Point{}
			labelGtx.Constraints.Max.X -= 2 * padH
			if labelGtx.Constraints.Max.X < 0 {
				labelGtx.Constraints.Max.X = 0
			}

			mColor := op.Record(gtx.Ops)
			paint.ColorOp{Color: colors.OnSurface}.Add(gtx.Ops)
			textMaterial := mColor.Stop()

			mLabel := op.Record(gtx.Ops)
			wl := widget.Label{MaxLines: 1}
			labelDims := wl.Layout(labelGtx, shaper, font.Font{}, unit.Sp(ts.LabelLarge), label, textMaterial)
			labelCall := mLabel.Stop()

			cellW := labelDims.Size.X + 2*padH
			cellH := stripH

			offY := (cellH - labelDims.Size.Y - underlineH) / 2
			if offY < 0 {
				offY = 0
			}
			st := op.Offset(image.Pt(padH, offY)).Push(gtx.Ops)
			labelCall.Add(gtx.Ops)
			st.Pop()

			if selected {
				underline := image.Rect(0, cellH-underlineH, cellW, cellH)
				paint.FillShape(gtx.Ops, colors.Primary, clip.Rect(underline).Op())
			}
			return layout.Dimensions{Size: image.Pt(cellW, cellH)}
		}

		if click == nil {
			return inner(gtx)
		}
		return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			semantic.LabelOp(label).Add(gtx.Ops)
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return inner(gtx)
		})
	}
}
