// Package navbar provides the Cadence Navbar pattern: a horizontal
// Surface bar with three slots — a leading Brand, a centred row of
// Links, and trailing Actions. The active link is marked with a
// Primary-coloured underline.
//
// The package follows the Phase 4 Composition contract: Navbar is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. Source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// "Centred" is approximate. The link row sits between two equal flexed
// spacers, so it is centred in the space that Brand and Actions leave
// over, not in the bar: a wide Brand against narrow Actions pushes the
// links right of the true centre by half the difference. Match the two
// slots' widths when that matters.
//
// Link.Active and Link.OnClick are independent. Active only selects the
// Primary underline, and a nil OnClick makes a link non-interactive and
// drops it out of focus traversal — so an Active link with no OnClick
// renders underlined and inert, which is usually a bug in the caller.
//
// There is no overflow behaviour: every Link renders on one line, and a
// bar too narrow for its own contents clips rather than wrapping or
// collapsing to a menu affordance. The bar also fills the height it is
// given — it reports gtx.Constraints.Max — so it needs a
// height-constrained slot; cadence/shell pins it to the density's bar
// height (ControlHeight + 2·PaddingY — 52 dp Comfortable, 40 dp Compact)
// for you. The bar's own vertical inset is Density.PaddingY, so a
// ControlHeight action (a prism/button) fills a density-pinned slot
// exactly; the horizontal inset stays spacing S4.
package navbar

import (
	"image"

	"gioui.org/font"
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
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

// Link is one entry in the navbar's link row. OnClick may be nil, in
// which case the link is treated as non-interactive and does not
// participate in focus traversal. Active selects the Primary-underline
// indicator and is independent of OnClick.
type Link struct {
	Label   string
	OnClick func(gtx layout.Context)
	Active  bool
}

// Props configures a Navbar. Brand is optional (a nil Brand collapses
// the leading slot to zero width while preserving document order).
// Actions entries that are nil are filtered before layout. Links may
// be empty.
type Props struct {
	Brand   layout.Widget
	Links   []Link
	Actions []layout.Widget

	// Shaper is an explicit per-instance override of the text shaper.
	// Leave it nil in normal use: the navbar then shapes its link labels
	// with the theme's shaper (Typography.Shaper()), which is built once
	// and cached inside the theme's Typography value. Set it only when
	// this instance must shape with a different shaper than the theme
	// provides.
	Shaper *text.Shaper
}

// Navbar returns an rx.Observable[layout.Widget] that emits a new
// widget whenever any consumed theme token changes. Click handlers
// fire for any Link whose OnClick is non-nil; interaction mirrors the
// prism/button model (widget.Clickable + semantic ops) per link.
func Navbar(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	// Flatten the nested theme observables into a concrete snapshot. The
	// typography emission supplies both the LabelLarge text style and the
	// theme's cached shaper (ADR-003: the theme owns the typeface).
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest4(t.Color, t.Spacing, t.Typography, t.Density),
			func(n rx.Tuple4[tokens.ColorTokens, tokens.SpacingScale, tokens.Typography, tokens.Density]) resolvedTokens {
				typ := n.Third
				return resolvedTokens{
					color:   n.First,
					spacing: n.Second,
					label:   typ.LabelLarge,
					density: n.Fourth,
					shaper:  typ.Shaper(),
				}
			},
		)
	})
	return rx.Defer(func() rx.Observable[layout.Widget] {
		clicks := make([]widget.Clickable, len(props.Links))
		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			// Props.Shaper is an explicit override; the theme's shaper is
			// the default.
			shaper := props.Shaper
			if shaper == nil {
				shaper = tok.shaper
			}
			return func(gtx layout.Context) layout.Dimensions {
				for i := range props.Links {
					if props.Links[i].OnClick != nil && clicks[i].Clicked(gtx) {
						props.Links[i].OnClick(gtx)
					}
				}
				return drawNavbar(gtx, shaper, props, clicks, tok.color, tok.spacing, tok.label, tok.density)
			}
		})
	})
}

// Render produces a layout.Widget for a navbar with pre-resolved
// tokens and no event processing. Intended for golden-image testing
// and static demonstrations; production code should use Navbar, which
// reads both of the parameters below off the theme.
//
// label is the LabelLarge role's whole text style — typeface, weight,
// size and line height all reach the shaper — and d is the density the
// bar draws at (its vertical inset and the links' padding). Pass
// tokens.DefaultTypography.LabelLarge and tokens.Comfortable for the
// default desktop look; before F3.4 the static path was pinned to
// Comfortable with no way to say otherwise.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	label tokens.TextStyle,
	d tokens.Density,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return drawNavbar(gtx, shaper, props, nil, colors, sp, label, d)
	}
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	label   tokens.TextStyle // the LabelLarge role: typeface, weight, size, line height
	density tokens.Density   // bar inset and link padding source (E1.4)
	shaper  *text.Shaper     // the theme's shaper; nil in the Render path
}

// underlineDp is the thickness of the Active-link Primary indicator.
const underlineDp = 2

func drawNavbar(gtx layout.Context, shaper *text.Shaper, props Props, clicks []widget.Clickable, colors tokens.ColorTokens, sp tokens.SpacingScale, style tokens.TextStyle, d tokens.Density) layout.Dimensions {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, colors.Surface, clip.Rect{Max: size}.Op())

	// E1.4: the vertical inset is the density's control padding, so a
	// ControlHeight control in a density-pinned slot (ControlHeight +
	// 2·PaddingY) fills it exactly; the horizontal inset stays spacing S4.
	inset := layout.Inset{
		Top:    unit.Dp(d.PaddingY),
		Bottom: unit.Dp(d.PaddingY),
		Left:   unit.Dp(sp.S4),
		Right:  unit.Dp(sp.S4),
	}
	inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(brandSlot(props.Brand)),
			layout.Flexed(1, emptyWidget),
			layout.Rigid(linksRow(shaper, props.Links, clicks, colors, sp, style, d)),
			layout.Flexed(1, emptyWidget),
			layout.Rigid(actionsRow(props.Actions, sp)),
		)
	})

	return layout.Dimensions{Size: size}
}

func brandSlot(w layout.Widget) layout.Widget {
	if w == nil {
		return emptyWidget
	}
	return w
}

// emptyWidget reports the minimum-constraint size so a Flexed parent's
// allocated space is honoured for offset arithmetic. Returning a zero
// Dimensions breaks Flex placement: subsequent children are positioned
// as if no space were consumed.
func emptyWidget(gtx layout.Context) layout.Dimensions {
	return layout.Dimensions{Size: gtx.Constraints.Min}
}

func linksRow(shaper *text.Shaper, links []Link, clicks []widget.Clickable, colors tokens.ColorTokens, sp tokens.SpacingScale, style tokens.TextStyle, d tokens.Density) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if len(links) == 0 {
			return layout.Dimensions{}
		}
		children := make([]layout.FlexChild, 0, 2*len(links)-1)
		for i, l := range links {
			if i > 0 {
				children = append(children, layout.Rigid(pllayout.HSpacer(sp.S2)))
			}
			children = append(children, layout.Rigid(linkWidget(shaper, l, clickFor(clicks, i), colors, sp, style, d)))
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	}
}

func actionsRow(actions []layout.Widget, sp tokens.SpacingScale) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		first := true
		for _, a := range actions {
			if a == nil {
				continue
			}
			if !first {
				children = append(children, layout.Rigid(pllayout.HSpacer(sp.S2)))
			}
			children = append(children, layout.Rigid(a))
			first = false
		}
		if len(children) == 0 {
			return layout.Dimensions{}
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	}
}

func clickFor(clicks []widget.Clickable, i int) *widget.Clickable {
	if i >= len(clicks) {
		return nil
	}
	return &clicks[i]
}

// linkWidget renders a single link as a label centred inside
// (S3, Density.PaddingY) padding — the horizontal 12 dp stays on the
// spacing scale (the E1.3 input rule), the vertical padding follows
// density. The cell width is at least 2×S3 so the Active underline is
// visible even when the label rasterises to zero width, which an empty
// Link.Label does. Links are adjacent cells in
// a row, so their hit area stays the cell bounds (extension would steal
// a neighbour's slop).
func linkWidget(shaper *text.Shaper, l Link, click *widget.Clickable, colors tokens.ColorTokens, sp tokens.SpacingScale, style tokens.TextStyle, d tokens.Density) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		inner := func(gtx layout.Context) layout.Dimensions {
			padH := gtx.Dp(unit.Dp(sp.S3))
			padV := gtx.Dp(unit.Dp(d.PaddingY))
			underlineH := gtx.Dp(unit.Dp(underlineDp))

			labelGtx := gtx
			labelGtx.Constraints.Min = image.Point{}
			labelGtx.Constraints.Max.X -= 2 * padH
			if labelGtx.Constraints.Max.X < 0 {
				labelGtx.Constraints.Max.X = 0
			}

			mColor := op.Record(gtx.Ops)
			paint.ColorOp{Color: colors.Text}.Add(gtx.Ops)
			textMaterial := mColor.Stop()

			// Shape with the LabelLarge role's typeface, weight, size and
			// line height. Zero fields (the legacy Render path synthesizes
			// a size-only style) fall back to the shaper's defaults.
			f := font.Font{Typeface: font.Typeface(style.Typeface)}
			if style.Weight != 0 {
				f.Weight = tokens.FontWeight(style.Weight)
			}
			wl := widget.Label{MaxLines: 1}
			if style.LineHeight != 0 {
				wl.LineHeight = unit.Sp(style.LineHeight)
				wl.LineHeightScale = 1
			}
			mLabel := op.Record(gtx.Ops)
			labelDims := wl.Layout(labelGtx, shaper, f, unit.Sp(style.Size), l.Label, textMaterial)
			labelCall := mLabel.Stop()

			cellW := labelDims.Size.X + 2*padH
			cellH := labelDims.Size.Y + 2*padV + underlineH

			st := op.Offset(image.Pt(padH, padV)).Push(gtx.Ops)
			labelCall.Add(gtx.Ops)
			st.Pop()

			if l.Active {
				underline := image.Rect(0, cellH-underlineH, cellW, cellH)
				paint.FillShape(gtx.Ops, colors.Primary, clip.Rect(underline).Op())
			}
			return layout.Dimensions{Size: image.Pt(cellW, cellH)}
		}

		if click == nil || l.OnClick == nil {
			return inner(gtx)
		}
		return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			semantic.LabelOp(l.Label).Add(gtx.Ops)
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return inner(gtx)
		})
	}
}
