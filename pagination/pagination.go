// Package pagination provides the Cadence Pagination pattern: a horizontal
// row of numbered page buttons flanked by prev/next chevrons. The current
// page button is highlighted via Primary/OnPrimary; the other page buttons
// reuse prism/button with the SurfaceVariant/OnSurfaceVariant pair so they
// remain visually distinct from the active page.
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
	"gioui.org/font/gofont"
	"gioui.org/io/pointer"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/reactivego/rx"
	"github.com/vibrantgio/prism/button"
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
)

// Props configures a Pagination instance. Page is 1-indexed; values outside
// [1, PageCount] still render but disable both chevrons and no page is
// highlighted as current. PageCount < 1 renders to zero-sized Dimensions.
type Props struct {
	Page      int
	PageCount int
	OnSelect  func(page int)

	// Shaper, if nil, defaults to a shaper backed by Go fonts. Created once
	// per subscription inside the rx.Defer scope so it survives theme
	// emissions for the lifetime of the Pagination instance.
	Shaper *text.Shaper
}

// Pagination returns an rx.Observable[layout.Widget] that emits a new
// widget whenever any consumed theme token changes. Click handlers fire
// for the chevrons (when not at the corresponding edge) and for each
// numbered page button; in all cases OnSelect receives the resulting page
// number (1-indexed).
func Pagination(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest4(t.Color, t.Spacing, t.Radius, t.Type),
			func(n rx.Tuple4[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale, tokens.TypeScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, radius: n.Third, typ: n.Fourth}
			},
		)
	})

	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}
		var prevClick, nextClick widget.Clickable
		n := props.PageCount
		if n < 0 {
			n = 0
		}
		pageClicks := make([]widget.Clickable, n)

		return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
			return func(gtx layout.Context) layout.Dimensions {
				if props.OnSelect != nil {
					if props.Page > 1 && prevClick.Clicked(gtx) {
						props.OnSelect(props.Page - 1)
					}
					if props.Page < props.PageCount && nextClick.Clicked(gtx) {
						props.OnSelect(props.Page + 1)
					}
					for i := range pageClicks {
						if pageClicks[i].Clicked(gtx) {
							props.OnSelect(i + 1)
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
// production code should use Pagination.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
	ts tokens.TypeScale,
) layout.Widget {
	tok := resolvedTokens{color: colors, spacing: sp, radius: rad, typ: ts}
	return func(gtx layout.Context) layout.Dimensions {
		return drawPagination(gtx, shaper, props, nil, nil, nil, tok)
	}
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	typ     tokens.TypeScale
}

const (
	cellWidthDp  = 40
	cellHeightDp = 44
	chevronSizeDp = 16
)

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

// pageCellWidget returns a fixed-width clickable cell rendering page n via
// prism/button.Render. For the current page the real Primary/OnPrimary
// tokens are used; for other pages a copy of the colour set substitutes
// SurfaceVariant/OnSurfaceVariant so they remain visually distinct from
// both the active page and the surrounding surface.
func pageCellWidget(shaper *text.Shaper, n int, current bool, click *widget.Clickable, tok resolvedTokens) layout.Widget {
	pageColors := tok.color
	if !current {
		pageColors.Primary = tok.color.SurfaceVariant
		pageColors.OnPrimary = tok.color.OnSurfaceVariant
	}
	label := strconv.Itoa(n)
	rendered := button.Render(shaper, label, pageColors, tok.spacing, tok.radius, tok.typ, button.RenderState{})

	return func(gtx layout.Context) layout.Dimensions {
		cellW := gtx.Dp(unit.Dp(cellWidthDp))
		cellH := gtx.Dp(unit.Dp(cellHeightDp))
		cgtx := gtx
		cgtx.Constraints.Min = image.Point{}
		cgtx.Constraints.Max = image.Pt(cellW, cellH)
		if click == nil {
			return rendered(cgtx)
		}
		return click.Layout(cgtx, func(gtx layout.Context) layout.Dimensions {
			semantic.LabelOp(label).Add(gtx.Ops)
			semantic.EnabledOp(true).Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			return rendered(gtx)
		})
	}
}

// chevronCellWidget renders a fixed-size chevron cell. pointsRight selects
// the "next" direction; otherwise the chevron points "prev". enabled=false
// dims the glyph to 38% alpha and skips click registration — matching the
// disabled-control convention used by prism/button.
func chevronCellWidget(pointsRight bool, click *widget.Clickable, enabled bool, tok resolvedTokens) layout.Widget {
	fg := tok.color.OnSurface
	if !enabled {
		fg = withAlpha(fg, 0x61)
	}
	return func(gtx layout.Context) layout.Dimensions {
		cellW := gtx.Dp(unit.Dp(cellWidthDp))
		cellH := gtx.Dp(unit.Dp(cellHeightDp))
		sz := gtx.Dp(unit.Dp(chevronSizeDp))
		cgtx := gtx
		cgtx.Constraints = layout.Exact(image.Pt(cellW, cellH))
		draw := func(gtx layout.Context) layout.Dimensions {
			drawChevron(gtx, cellW/2, cellH/2, sz, fg, pointsRight)
			return layout.Dimensions{Size: image.Pt(cellW, cellH)}
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

func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = uint8(uint16(c.A) * uint16(a) / 255)
	return c
}
