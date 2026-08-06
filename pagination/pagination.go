// Package pagination provides the Cadence Pagination pattern: a horizontal
// row of numbered page buttons flanked by prev/next chevrons. The current
// page button is highlighted via Primary/OnPrimary; the other page buttons
// use a neutral tinted-fill pair (neutral 300 fill, neutral 700 label) so
// they remain visually distinct from the active page.
//
// Page cells are drawn natively (E1.4). They previously bridged through
// prism/button.Render, but that static bridge's frozen signature renders
// at tokens.Comfortable — inside a density-sized ControlHeight square its
// Comfortable PaddingX (16 dp) truncated the page digit to a sliver, and
// nothing was gained: the bridge only ever drew the normal state (no
// hover/press/focus visuals reached it). Drawing the cell here — the same
// Primary/neutral fill, radius.Md corners, centred digit — keeps the
// visuals aligned with prism/button while letting every metric follow the
// density. The trade-off, accepted deliberately: future prism/button
// styling changes must be mirrored here by hand.
//
// The package follows the Phase 4 Composition contract: Pagination is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. Source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// No virtualisation, no ellipsis collapse — every page in [1, PageCount]
// renders. Large page counts are deferred to G4.4 (table + pagination at
// scale).
package pagination

import (
	"image"
	"image/color"
	"strconv"

	"gioui.org/f32"
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
	"github.com/vibrantgio/prism/icon"
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
	"github.com/vibrantgio/spectrum/typeset"
)

// Props configures a Pagination instance. Page is 1-indexed; values outside
// [1, PageCount] still render but disable both chevrons and no page is
// highlighted as current. PageCount < 1 renders to zero-sized Dimensions.
type Props struct {
	Page      int
	PageCount int
	OnSelect  func(gtx layout.Context, page int)

	// Shaper is an explicit per-instance override of the text shaper.
	// Leave it nil in normal use: the pagination then shapes its page
	// digits with the theme's shaper (Typography.Shaper()), which is
	// built once and cached inside the theme's Typography value. Set it
	// only when this instance must shape with a different shaper than
	// the theme provides.
	Shaper *text.Shaper
}

// Pagination returns an rx.Observable[layout.Widget] that emits a new
// widget whenever any consumed theme token changes. Click handlers fire
// for the chevrons (when not at the corresponding edge) and for each
// numbered page button; in all cases OnSelect receives the resulting page
// number (1-indexed).
func Pagination(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	// Flatten the nested theme observables into a concrete snapshot. The
	// typography emission supplies both the LabelLarge text style and the
	// theme's cached shaper (ADR-003: the theme owns the typeface).
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest5(t.Color, t.Spacing, t.Radius, t.Typography, t.Density),
			func(n rx.Tuple5[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale, tokens.Typography, tokens.Density]) resolvedTokens {
				typ := n.Fourth
				return resolvedTokens{
					color:   n.First,
					spacing: n.Second,
					radius:  n.Third,
					label:   typ.LabelLarge,
					density: n.Fifth,
					shaper:  typ.Shaper(),
				}
			},
		)
	})

	return rx.Defer(func() rx.Observable[layout.Widget] {
		var prevClick, nextClick widget.Clickable
		n := props.PageCount
		if n < 0 {
			n = 0
		}
		pageClicks := make([]widget.Clickable, n)

		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			// Props.Shaper is an explicit override; the theme's shaper is
			// the default.
			shaper := props.Shaper
			if shaper == nil {
				shaper = tok.shaper
			}
			return func(gtx layout.Context) layout.Dimensions {
				if props.OnSelect != nil {
					if props.Page > 1 && prevClick.Clicked(gtx) {
						props.OnSelect(gtx, props.Page-1)
					}
					if props.Page < props.PageCount && nextClick.Clicked(gtx) {
						props.OnSelect(gtx, props.Page+1)
					}
					for i := range pageClicks {
						if pageClicks[i].Clicked(gtx) {
							props.OnSelect(gtx, i+1)
						}
					}
				}
				return drawPagination(gtx, shaper, props, &prevClick, &nextClick, pageClicks, tok)
			}
		})
	})
}

// Render produces a layout.Widget for a pagination row with pre-resolved
// tokens. Intended for golden-image testing and static demonstrations;
// production code should use Pagination, which reads both of the
// parameters below off the theme.
//
// label is the LabelLarge role's whole text style — typeface, weight,
// size and line height all reach the shaper — and d is the density the
// row draws at (every cell is a Density.ControlHeight square, and the
// chevron glyph scales with it). Pass
// tokens.DefaultTypography.LabelLarge and tokens.Comfortable for the
// default desktop look; before F3.4 the static path was pinned to
// Comfortable with no way to say otherwise.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
	label tokens.TextStyle,
	d tokens.Density,
) layout.Widget {
	tok := resolvedTokens{color: colors, spacing: sp, radius: rad, label: label, density: d}
	return func(gtx layout.Context) layout.Dimensions {
		return drawPagination(gtx, shaper, props, nil, nil, nil, tok)
	}
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	label   tokens.TextStyle // the LabelLarge role: typeface, weight, size, line height
	density tokens.Density   // cell square and chevron glyph source (E1.4)
	shaper  *text.Shaper     // the theme's shaper; nil in the Render path
}

// Cell metrics (E1.4): every pagination control is a Density.ControlHeight
// square (36 dp Comfortable, 28 dp Compact — the old 40×44 cell was built
// around the 44 dp pointer floor, which is a hit metric, not a control
// height; the digit centres in the square like an icon-button glyph). The
// chevron glyph takes the icon rule, icon.Size(d) = ControlHeight −
// 2·PaddingY (20/16 dp), matching prism icon buttons. Cells are adjacent
// controls separated by S2 gaps, so their hit area stays the cell bounds
// (extending to the 44 dp pointer floor would overlap the neighbouring
// cell's slop — the E1.3 stacked/tiled-controls precedent).

func drawPagination(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	prevClick, nextClick *widget.Clickable,
	pageClicks []widget.Clickable,
	tok resolvedTokens,
) layout.Dimensions {
	if props.PageCount < 1 {
		return layout.Dimensions{}
	}

	gap := layout.Rigid(pllayout.HSpacer(tok.spacing.S2))
	children := make([]layout.FlexChild, 0, 2*props.PageCount+5)
	children = append(children, layout.Rigid(chevronCellWidget(false, prevClick, props.Page > 1, tok)))
	children = append(children, gap)
	for i := 1; i <= props.PageCount; i++ {
		children = append(children, layout.Rigid(pageCellWidget(shaper, i, i == props.Page, clickFor(pageClicks, i-1), tok)))
		if i < props.PageCount {
			children = append(children, gap)
		}
	}
	children = append(children, gap)
	children = append(children, layout.Rigid(chevronCellWidget(true, nextClick, props.Page < props.PageCount, tok)))
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

func clickFor(clicks []widget.Clickable, i int) *widget.Clickable {
	if i < 0 || i >= len(clicks) {
		return nil
	}
	return &clicks[i]
}

// pageCellWidget returns a clickable ControlHeight-square cell rendering
// page n natively (see the package doc for why the prism/button.Render
// bridge was dropped in E1.4). The current page uses the real
// Primary/OnPrimary pair — prism/button's normal-state colours — and
// other pages a neutral tinted fill (step 300) with low-contrast text
// (step 700), so they remain visually distinct from both the active page
// and the surrounding surface.
func pageCellWidget(shaper *text.Shaper, n int, current bool, click *widget.Clickable, tok resolvedTokens) layout.Widget {
	bg, fg := tok.color.Ramps.Neutral.Step(300), tok.color.Ramps.Neutral.Step(700)
	if current {
		bg, fg = tok.color.Primary, tok.color.OnPrimary
	}
	label := strconv.Itoa(n)

	return func(gtx layout.Context) layout.Dimensions {
		side := gtx.Dp(unit.Dp(tok.density.ControlHeight))
		cgtx := gtx
		cgtx.Constraints = layout.Exact(image.Pt(side, side))
		draw := func(gtx layout.Context) layout.Dimensions {
			return drawPageCell(gtx, shaper, label, bg, fg, tok, side)
		}
		if click == nil {
			return draw(cgtx)
		}
		return click.Layout(cgtx, func(gtx layout.Context) layout.Dimensions {
			semantic.LabelOp(label).Add(gtx.Ops)
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return draw(gtx)
		})
	}
}

// drawPageCell paints one page-number cell: a side×side rounded square
// (radius.Md, prism/button's corner) filled with bg, the digit shaped in
// the LabelLarge role and centred. The digit is never truncated — the
// square is the control, the digit its glyph, mirroring the icon-button
// rule rather than the text-button padding rule.
func drawPageCell(gtx layout.Context, shaper *text.Shaper, label string, bg, fg color.NRGBA, tok resolvedTokens, side int) layout.Dimensions {
	rad := gtx.Dp(unit.Dp(tok.radius.Md))
	rrect := clip.RRect{Rect: image.Rectangle{Max: image.Pt(side, side)}, SE: rad, SW: rad, NE: rad, NW: rad}
	paint.FillShape(gtx.Ops, bg, rrect.Op(gtx.Ops))

	mColor := op.Record(gtx.Ops)
	paint.ColorOp{Color: fg}.Add(gtx.Ops)
	material := mColor.Stop()

	labelGtx := gtx
	labelGtx.Constraints.Min = image.Point{}
	labelGtx.Constraints.Max = image.Pt(side, side)

	// Shape with the LabelLarge role's typeface, weight, size and line
	// height. Zero fields (the legacy Render path synthesizes a size-only
	// style) fall back to the shaper's defaults.
	style := tok.label
	f := typeset.Font(style, font.Normal)
	wl := typeset.Label(style, 1)
	mLabel := op.Record(gtx.Ops)
	labelDims := typeset.Layout(labelGtx, shaper, wl, f, unit.Sp(style.Size), label, material)
	labelCall := mLabel.Stop()

	offX := (side - labelDims.Size.X) / 2
	offY := (side - labelDims.Size.Y) / 2
	if offX < 0 {
		offX = 0
	}
	if offY < 0 {
		offY = 0
	}
	st := op.Offset(image.Pt(offX, offY)).Push(gtx.Ops)
	labelCall.Add(gtx.Ops)
	st.Pop()

	return layout.Dimensions{Size: image.Pt(side, side)}
}

// chevronCellWidget renders a ControlHeight-square chevron cell whose
// glyph takes the icon rule, icon.Size(d). pointsRight selects
// the "next" direction; otherwise the chevron points "prev". enabled=false
// dims the glyph to tokens.DisabledOpacity and skips click registration —
// matching the disabled-control convention used by prism/button.
func chevronCellWidget(pointsRight bool, click *widget.Clickable, enabled bool, tok resolvedTokens) layout.Widget {
	fg := tok.color.Text
	if !enabled {
		fg = tokens.Disabled(fg)
	}
	return func(gtx layout.Context) layout.Dimensions {
		side := gtx.Dp(unit.Dp(tok.density.ControlHeight))
		sz := gtx.Dp(icon.Size(tok.density))
		cgtx := gtx
		cgtx.Constraints = layout.Exact(image.Pt(side, side))
		draw := func(gtx layout.Context) layout.Dimensions {
			drawChevron(gtx, side/2, side/2, sz, fg, pointsRight)
			return layout.Dimensions{Size: image.Pt(side, side)}
		}
		if click == nil || !enabled {
			return draw(cgtx)
		}
		return click.Layout(cgtx, func(gtx layout.Context) layout.Dimensions {
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return draw(gtx)
		})
	}
}

// drawChevron paints a filled triangle centred at (cx, cy) fitting within
// an sz × sz square. pointsRight selects the apex direction along +X (next)
// or -X (prev).
func drawChevron(gtx layout.Context, cx, cy, sz int, col color.NRGBA, pointsRight bool) {
	half := float32(sz) / 2
	quarter := float32(sz) / 4
	fcx := float32(cx)
	fcy := float32(cy)

	var p clip.Path
	p.Begin(gtx.Ops)
	if pointsRight {
		p.MoveTo(f32.Pt(fcx-quarter, fcy-half))
		p.LineTo(f32.Pt(fcx+quarter, fcy))
		p.LineTo(f32.Pt(fcx-quarter, fcy+half))
	} else {
		p.MoveTo(f32.Pt(fcx+quarter, fcy-half))
		p.LineTo(f32.Pt(fcx-quarter, fcy))
		p.LineTo(f32.Pt(fcx+quarter, fcy+half))
	}
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: p.End()}.Op())
}
