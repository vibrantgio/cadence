// Package card provides the Cadence Card pattern: a rounded Surface
// container with optional Header / Body / Footer slots, in either an
// outlined (flat) or elevated (shadowed) variant.
//
// The package follows the Phase 4 Composition contract: Card is a callable
// Go function consuming a Prism theme observable, returning a stream of
// layout.Widget. The source is intentionally short and free of opaque
// configuration — copy it into your own app and modify as needed.
package card

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/prism/theme"
	"github.com/vibrantgio/prism/tokens"
	"github.com/vibrantgio/pulse/depth"
)

// Props configures a Card. All slot fields are optional; nil slots are
// simply omitted from the inner stack. Elevated swaps the outlined
// variant (1 dp Outline stroke) for a shadowed variant rendered via
// pulse/depth at ElevationLevel2.
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
			rx.CombineLatest3(t.Color, t.Spacing, t.Radius),
			func(n rx.Tuple3[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, radius: n.Third}
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
}

func drawCard(gtx layout.Context, props Props, colors tokens.ColorTokens, sp tokens.SpacingScale, rad tokens.RadiusScale) layout.Dimensions {
	size := gtx.Constraints.Max
	bounds := image.Rectangle{Max: size}
	r := gtx.Dp(unit.Dp(rad.Lg))
	gap := gtx.Dp(unit.Dp(sp.S3))

	if props.Elevated {
		depth.Shadow(gtx, bounds, tokens.Level2)
	}

	rrect := clip.RRect{Rect: bounds, SE: r, SW: r, NE: r, NW: r}
	paint.FillShape(gtx.Ops, colors.Surface, rrect.Op(gtx.Ops))

	if !props.Elevated {
		paint.FillShape(gtx.Ops, colors.Outline, clip.Stroke{
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
