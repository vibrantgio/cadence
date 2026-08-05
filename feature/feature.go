// Package feature provides the Cadence Feature pattern: an icon-title-body
// grid laid out as `Columns × N`, suitable for a marketing or onboarding
// "features" section.
//
// The package follows the Phase 4 Composition contract: Feature is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. The source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// Layout: an S6 outer inset. The grid lays Items into rows of `Columns`
// equal-width cells (the last row pads with empty cells), separated by an
// S5 gap horizontally between cells and vertically between rows. Each cell
// stacks (top to bottom) an optional icon sized to an S8 × S8 square, the
// Title in title-medium typography in Text, and the Body in body-medium
// typography in the low-contrast neutral-700 step.
//
// The Icon slot is opaque — callers supply any layout.Widget. No
// responsive collapse from `Columns` to a smaller column count on narrow
// viewports is provided; render at a width that fits `Columns × cell` or
// adopt a caller-side breakpoint.
package feature

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/reactivego/rx"
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

// defaultColumns is the column count used when Props.Columns is zero.
const defaultColumns = 3

// Item describes a single grid cell.
type Item struct {
	// Icon is an optional leading visual rendered at the top of the cell,
	// sized to an S8 × S8 square. nil omits the icon row entirely.
	Icon layout.Widget

	// Title is rendered in title-medium typography in Text.
	Title string

	// Body is rendered in body-medium typography in neutral 700.
	Body string
}

// Props configures a Feature grid.
type Props struct {
	// Columns is the number of cells per row. Zero defaults to 3.
	Columns int

	// Items is the ordered list of cells. Length 0 renders an empty grid.
	Items []Item

	// Shaper is an explicit per-instance override of the text shaper. Leave
	// it nil in normal use: the grid then shapes its titles and bodies with
	// the theme's shaper (Typography.Shaper()), which is built once and
	// cached inside the theme's Typography value. Set it only when this
	// instance must shape with a different shaper than the theme provides.
	Shaper *text.Shaper
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	title   tokens.TextStyle // the TitleMedium role: typeface, weight, size, line height
	body    tokens.TextStyle // the BodyMedium role
	shaper  *text.Shaper     // the theme's shaper; nil in the Render path
}

// Feature returns an rx.Observable[layout.Widget] that emits a new widget
// whenever any consumed theme token changes. The grid is purely
// presentational: cells carry no interaction state, so no per-emission
// click bookkeeping is needed.
func Feature(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	if props.Columns <= 0 {
		props.Columns = defaultColumns
	}

	// Flatten the nested theme observables into a concrete snapshot. The
	// typography emission supplies the TitleMedium and BodyMedium text
	// styles and the theme's cached shaper (ADR-003: the theme owns the
	// typeface).
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest3(t.Color, t.Spacing, t.Typography),
			func(n rx.Tuple3[tokens.ColorTokens, tokens.SpacingScale, tokens.Typography]) resolvedTokens {
				typ := n.Third
				return resolvedTokens{
					color:   n.First,
					spacing: n.Second,
					title:   typ.TitleMedium,
					body:    typ.BodyMedium,
					shaper:  typ.Shaper(),
				}
			},
		)
	})

	return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
		// Props.Shaper is an explicit override; the theme's shaper is the
		// default.
		shaper := props.Shaper
		if shaper == nil {
			shaper = tok.shaper
		}
		return func(gtx layout.Context) layout.Dimensions {
			return drawFeature(gtx, shaper, props, tok)
		}
	})
}

// Render produces a layout.Widget for a feature grid with pre-resolved
// tokens. Intended for golden-image testing and static demonstrations;
// production code should use Feature, which takes the shaper and the
// TitleMedium/BodyMedium text styles from the theme's Typography. The
// TypeScale parameter contributes only the role sizes; the title falls
// back to a SemiBold weight (matching the pre-Typography rendering) and
// the shaper's default typeface and line height.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	if props.Columns <= 0 {
		props.Columns = defaultColumns
	}
	tok := resolvedTokens{
		color:   colors,
		spacing: sp,
		title:   tokens.TextStyle{Size: ts.TitleMedium},
		body:    tokens.TextStyle{Size: ts.BodyMedium},
	}
	return func(gtx layout.Context) layout.Dimensions {
		return drawFeature(gtx, shaper, props, tok)
	}
}

func drawFeature(gtx layout.Context, shaper *text.Shaper, props Props, tok resolvedTokens) layout.Dimensions {
	if len(props.Items) == 0 {
		return layout.Dimensions{}
	}
	pad := unit.Dp(tok.spacing.S6)
	return layout.UniformInset(pad).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		cols := props.Columns
		rowCount := (len(props.Items) + cols - 1) / cols

		rows := make([]layout.Widget, 0, rowCount)
		for r := range rowCount {
			rows = append(rows, func(gtx layout.Context) layout.Dimensions {
				return drawRow(gtx, shaper, props, tok, r)
			})
		}

		gap := tok.spacing.S5
		spaced := make([]layout.Widget, 0, 2*len(rows)-1)
		for i, w := range rows {
			if i > 0 {
				spaced = append(spaced, pllayout.VSpacer(gap))
			}
			spaced = append(spaced, w)
		}
		return pllayout.Col(gtx, spaced...)
	})
}

// drawRow draws row r of the grid as a Flex of equal-width Flexed(1)
// cells separated by S5 HSpacer gutters. Trailing positions past
// len(Items) render as empty cells so per-cell widths stay uniform across
// the final row.
func drawRow(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	tok resolvedTokens,
	r int,
) layout.Dimensions {
	cols := props.Columns
	children := make([]layout.FlexChild, 0, 2*cols-1)
	for c := range cols {
		if c > 0 {
			children = append(children, layout.Rigid(pllayout.HSpacer(tok.spacing.S5)))
		}
		idx := r*cols + c
		if idx < len(props.Items) {
			item := props.Items[idx]
			children = append(children, layout.Flexed(1, cellWidget(shaper, item, tok)))
		} else {
			children = append(children, layout.Flexed(1, emptyCell))
		}
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx, children...)
}

// cellWidget stacks the cell's optional icon, title, and body in a column
// with S3 gaps between adjacent items.
func cellWidget(shaper *text.Shaper, item Item, tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		var ws []layout.Widget
		if item.Icon != nil {
			ws = append(ws, iconCellWidget(item.Icon, tok))
		}
		ws = append(ws, titleWidget(shaper, item.Title, tok))
		ws = append(ws, bodyWidget(shaper, item.Body, tok))

		gap := tok.spacing.S3
		spaced := make([]layout.Widget, 0, 2*len(ws)-1)
		for i, w := range ws {
			if i > 0 {
				spaced = append(spaced, pllayout.VSpacer(gap))
			}
			spaced = append(spaced, w)
		}
		return pllayout.Col(gtx, spaced...)
	}
}

// iconCellWidget renders the caller-supplied icon clipped to an S8 × S8
// square. The icon receives a fixed-size constraint so callers can draw
// freely without measuring.
func iconCellWidget(icon layout.Widget, tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := gtx.Dp(unit.Dp(tok.spacing.S8))
		cgtx := gtx
		cgtx.Constraints = layout.Exact(image.Pt(size, size))
		icon(cgtx)
		return layout.Dimensions{Size: image.Pt(size, size)}
	}
}

// titleWidget renders the title in the TitleMedium role in Text. A
// zero style weight (the legacy Render path synthesizes a size-only
// style) falls back to SemiBold, matching the pre-Typography rendering.
func titleWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return textWidget(shaper, label, tok.color.Text, tok.title, font.SemiBold)
}

// bodyWidget renders the body in the BodyMedium role in the low-contrast
// text step (neutral 700).
func bodyWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return textWidget(shaper, label, tok.color.Ramps.Neutral.Step(700), tok.body, font.Normal)
}

// textWidget renders a wrapped widget.Label in the supplied colour and
// text style. The style's typeface, weight, size and line height are
// honoured; a zero style weight falls back to fallbackWeight and a zero
// line height stays at the shaper's default.
func textWidget(shaper *text.Shaper, label string, fg color.NRGBA, style tokens.TextStyle, fallbackWeight font.Weight) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if label == "" {
			return layout.Dimensions{}
		}
		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: fg}.Add(gtx.Ops)
		material := mColor.Stop()
		f := font.Font{Typeface: font.Typeface(style.Typeface), Weight: fallbackWeight}
		if style.Weight != 0 {
			f.Weight = tokens.FontWeight(style.Weight)
		}
		wl := widget.Label{MaxLines: 3}
		if style.LineHeight != 0 {
			wl.LineHeight = unit.Sp(style.LineHeight)
			wl.LineHeightScale = 1
		}
		return wl.Layout(gtx, shaper, f, unit.Sp(style.Size), label, material)
	}
}

// emptyCell renders nothing but reports its full Flex allocation so the
// last row keeps uniform per-cell widths when len(Items) is not a multiple
// of Columns.
func emptyCell(gtx layout.Context) layout.Dimensions {
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
}
