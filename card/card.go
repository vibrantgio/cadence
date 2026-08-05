// Package card provides the Cadence Card pattern: a rounded surface
// container with optional Header / Body / Footer slots, in either an
// outlined or elevated variant.
//
// Elevation (goal G-E2): a card is raised in place, not floating, so
// both variants read as raised by tonal surface step alone (ADR-007) —
// no cast shadow. The default outlined card sits on the standing
// content plane at SurfaceAt(Level1) (Neutral step 200) with a 1 dp
// Neutral step-500 stroke, ADR-007's strong border; the Elevated
// variant fills one storey deeper at SurfaceAt(Level2) (Neutral step
// 300). E2.2's verdict retired the Elevated variant's pulse/depth
// shadow: shadows are reserved for surfaces that float and can leave
// (toasts, menus), which a card is not.
//
// The package follows the Phase 4 Composition contract: Card is a callable
// Go function consuming a Prism theme observable, returning a stream of
// layout.Widget. The source is intentionally short and free of opaque
// configuration — copy it into your own app and modify as needed.
//
// Card draws no text of its own, which is why Props carries no Shaper
// where its sibling patterns do: all three slots are caller-supplied
// layout.Widgets, so the typeface of anything inside a card is settled by
// whoever builds those widgets. Nil slots are dropped from the stack
// entirely, and the S3 gaps fall only between the slots that survive — a
// Body-only card is not padded as though the Header and Footer were there
// but empty.
//
// The card fills the constraints it is given rather than shrinking to its
// content — it reports gtx.Constraints.Max as its size — so a Card handed
// a full-height column takes the whole column. Constrain it yourself.
package card

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

// Props configures a Card. All slot fields are optional; nil slots are
// simply omitted from the inner stack. Elevated swaps the outlined
// variant (a level-1 fill with a 1 dp neutral step-500 stroke, ADR-007's
// strong border) for a level-2 tonal fill (SurfaceAt(Level2)) with no
// stroke and no shadow — a card is raised in place, per E2.2's verdict.
type Props struct {
	Header layout.Widget
	Body   layout.Widget
	Footer layout.Widget

	// Elevated selects the shadowed surface variant. Defaults to the
	// outlined variant.
	Elevated bool
}

// Card returns an rx.Observable[layout.Widget] that emits a new widget
// whenever any consumed theme token changes. The widget fills its
// available constraints and renders a rounded Surface, with the three
// slots stacked vertically inside an S4 inset and separated by S3 gaps.
func Card(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest4(t.Color, t.Spacing, t.Radius, t.Elevation),
			func(n rx.Tuple4[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale, tokens.ElevationScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, radius: n.Third, elevation: n.Fourth}
			},
		)
	})
	return rx.Map(resolved, func(tok resolvedTokens) layout.Widget {
		return Render(props, tok.color, tok.spacing, tok.radius)
	})
}

// Render produces a layout.Widget for a card with pre-resolved tokens.
// Intended for golden-image testing and static demonstrations; production
// code should use Card.
func Render(props Props, colors tokens.ColorTokens, sp tokens.SpacingScale, rad tokens.RadiusScale) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return drawCard(gtx, props, colors, sp, rad)
	}
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	// elevation is snapshotted so a theme elevation change re-emits the
	// widget; the fills themselves resolve through SurfaceAt, which reads
	// the default tokens.Elevation scale.
	elevation tokens.ElevationScale
}

func drawCard(gtx layout.Context, props Props, colors tokens.ColorTokens, sp tokens.SpacingScale, rad tokens.RadiusScale) layout.Dimensions {
	size := gtx.Constraints.Max
	bounds := image.Rectangle{Max: size}
	r := gtx.Dp(unit.Dp(rad.Lg))
	gap := gtx.Dp(unit.Dp(sp.S3))

	// The card is raised by tonal step alone: level 1 for the default
	// outlined variant, level 2 for Elevated (no shadow — E2.2's verdict).
	fill := colors.SurfaceAt(tokens.Level1)
	if props.Elevated {
		fill = colors.SurfaceAt(tokens.Level2)
	}

	rrect := clip.RRect{Rect: bounds, SE: r, SW: r, NE: r, NW: r}
	paint.FillShape(gtx.Ops, fill, rrect.Op(gtx.Ops))

	if !props.Elevated {
		paint.FillShape(gtx.Ops, colors.Ramps.Neutral.Step(500), clip.Stroke{
			Path:  rrect.Path(gtx.Ops),
			Width: float32(gtx.Dp(unit.Dp(1))),
		}.Op())
	}

	layout.UniformInset(unit.Dp(sp.S4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return stack(gtx, gap, props.Header, props.Body, props.Footer)
	})

	return layout.Dimensions{Size: size}
}

// stack lays out the non-nil children top-to-bottom with a gap of gapPx
// pixels between adjacent children. Nil children are skipped.
func stack(gtx layout.Context, gapPx int, children ...layout.Widget) layout.Dimensions {
	ws := make([]layout.Widget, 0, len(children)*2)
	for _, c := range children {
		if c == nil {
			continue
		}
		if len(ws) > 0 {
			ws = append(ws, gapWidget(gapPx))
		}
		ws = append(ws, c)
	}
	if len(ws) == 0 {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	return pllayout.Col(gtx, ws...)
}

// gapWidget reserves gapPx vertical pixels and zero horizontal space.
func gapWidget(gapPx int) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(0, gapPx)}
	}
}
