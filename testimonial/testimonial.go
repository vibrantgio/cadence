// Package testimonial provides the Cadence Testimonial pattern: a single
// centered card or a horizontal row of cards quoting a named author,
// suitable for a marketing or onboarding "social proof" section.
//
// The package follows the Phase 4 Composition contract: Testimonial is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. The source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// Layout: each Item renders as a rounded Surface card with a 1 dp strong
// border and S5 padding on all sides. The card stacks (top to bottom) an
// opening double-quotation glyph in Primary rendered from a clip.Path,
// the Quote body in body-large typography in Text, and a horizontal
// author block — the AuthorAvatar (or, when nil, a border-stroked
// circular placeholder containing the first letter of AuthorName) sized
// to S8 × S8, then a vertical stack of AuthorName in Text and
// AuthorRole in the low-contrast neutral-700 step.
//
// The Single variant renders Items[0] in a single card centered inside an
// S6 outer inset; Grid lays the cards out as a horizontal row of equal-
// width cells separated by an S4 gutter. No responsive collapse from Grid
// to a vertical stack on narrow viewports is provided; render Grid at a
// width that fits the cells or adopt a caller-side breakpoint.
package testimonial

import (
	"image"
	"image/color"
	"math"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/reactivego/rx"
	pllayout "github.com/vibrantgio/prism/layout"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
	"github.com/vibrantgio/spectrum/typeset"
)

// Variant selects between the two testimonial layouts.
type Variant int

const (
	// Single renders one card (Items[0]) centered inside an S6 outer
	// inset. The zero Variant value is Single so a caller that forgets to
	// set Props.Variant still gets a sensible layout.
	Single Variant = iota
	// Grid renders all Items in a horizontal row of equal-width cards
	// separated by an S4 gutter.
	Grid
)

// Item describes a single testimonial card.
type Item struct {
	// Quote is the body of the testimonial rendered in body-large
	// typography in Text. Empty quotes collapse to zero height.
	Quote string

	// AuthorName is rendered in body-medium SemiBold Text, above
	// AuthorRole inside the author block.
	AuthorName string

	// AuthorRole is rendered in body-small neutral 700, below
	// AuthorName inside the author block.
	AuthorRole string

	// AuthorAvatar is an optional avatar widget rendered as an S8 × S8
	// leading visual inside the author block. When nil, a border-
	// bordered circular placeholder containing the first letter of
	// AuthorName is rendered instead.
	AuthorAvatar layout.Widget
}

// Props configures a Testimonial.
type Props struct {
	// Variant selects between the Single and Grid layouts.
	Variant Variant

	// Items is the ordered list of testimonials. In Single mode only
	// Items[0] is rendered; an empty slice collapses to zero dimensions.
	Items []Item

	// Shaper is an explicit per-instance override of the text shaper. Leave
	// it nil in normal use: the testimonial then shapes its quote and author
	// text with the theme's shaper (Typography.Shaper()), which is built
	// once for the process and shared by every component reading that
	// typography — the cache lives behind the Typography value, so it
	// survives the copy this component's map function makes of it (spectrum
	// F5.1). Set it only when this instance must shape with a different
	// shaper than the theme provides.
	//
	// A shaper is not safe to use from two goroutines; Gio lays the widget
	// forest out on the one goroutine that runs the event loop, which is
	// what makes sharing it correct. See spectrum/tokens.Typography.Shaper.
	Shaper *text.Shaper
}

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	radius  tokens.RadiusScale
	quote   tokens.TextStyle // the BodyLarge role: typeface, weight, size, line height
	body    tokens.TextStyle // the BodyMedium role (author name, placeholder letter)
	small   tokens.TextStyle // the BodySmall role (author role)
	shaper  *text.Shaper     // the theme's shaper; nil in the Render path
}

// Testimonial returns an rx.Observable[layout.Widget] that emits a new
// widget whenever any consumed theme token changes. The layout is purely
// presentational — no interaction state is carried across emissions.
func Testimonial(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	// Flatten the nested theme observables into a concrete snapshot. The
	// typography emission supplies the BodyLarge/BodyMedium/BodySmall text
	// styles and the theme's cached shaper (ADR-003: the theme owns the
	// typeface).
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest4(t.Color, t.Spacing, t.Radius, t.Typography),
			func(n rx.Tuple4[tokens.ColorTokens, tokens.SpacingScale, tokens.RadiusScale, tokens.Typography]) resolvedTokens {
				typ := n.Fourth
				return resolvedTokens{
					color:   n.First,
					spacing: n.Second,
					radius:  n.Third,
					quote:   typ.BodyLarge,
					body:    typ.BodyMedium,
					small:   typ.BodySmall,
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
			return drawTestimonial(gtx, shaper, props, tok)
		}
	})
}

// Render produces a layout.Widget for a testimonial with pre-resolved
// tokens. Intended for golden-image testing and static demonstrations;
// production code should use Testimonial, which reads the same typography
// off the theme.
//
// typo supplies the three roles the card draws — BodyLarge for the quote,
// BodyMedium for the author name, BodySmall for the role line — whole, so
// typeface, weight and line height reach the shaper exactly as they do on
// the live path. A pattern that spends more than one role takes the whole
// tokens.Typography rather than a role's tokens.TextStyle each: the roles
// it picks stay its own business, as they are on the live path. Pass
// tokens.DefaultTypography for the default desktop look. There is no
// density parameter: a testimonial card sizes no control.
func Render(
	shaper *text.Shaper,
	props Props,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	rad tokens.RadiusScale,
	typo tokens.Typography,
) layout.Widget {
	tok := resolvedTokens{
		color:   colors,
		spacing: sp,
		radius:  rad,
		quote:   typo.BodyLarge,
		body:    typo.BodyMedium,
		small:   typo.BodySmall,
	}
	return func(gtx layout.Context) layout.Dimensions {
		return drawTestimonial(gtx, shaper, props, tok)
	}
}

func drawTestimonial(gtx layout.Context, shaper *text.Shaper, props Props, tok resolvedTokens) layout.Dimensions {
	if len(props.Items) == 0 {
		return layout.Dimensions{}
	}
	switch props.Variant {
	case Grid:
		return drawGrid(gtx, shaper, props.Items, tok)
	default:
		return drawSingle(gtx, shaper, props.Items[0], tok)
	}
}

// drawSingle wraps a single card in an S6 UniformInset so it sits with
// equal margin on every side of the available space.
func drawSingle(gtx layout.Context, shaper *text.Shaper, item Item, tok resolvedTokens) layout.Dimensions {
	pad := unit.Dp(tok.spacing.S6)
	return layout.UniformInset(pad).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return drawCard(gtx, shaper, item, tok)
	})
}

// drawGrid lays the items out as a horizontal row of equal-width Flexed
// cells separated by S4 HSpacer gutters.
func drawGrid(gtx layout.Context, shaper *text.Shaper, items []Item, tok resolvedTokens) layout.Dimensions {
	gap := pllayout.HSpacer(tok.spacing.S4)
	children := make([]layout.FlexChild, 0, 2*len(items)-1)
	for i := range items {
		if i > 0 {
			children = append(children, layout.Rigid(gap))
		}
		item := items[i]
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return drawCard(gtx, shaper, item, tok)
		}))
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx, children...)
}

// drawCard draws one testimonial card: a rounded Surface filled to its
// allocated width with content height matching the inner stack plus S5
// padding on all sides, framed with a 1 dp neutral step-500 stroke.
func drawCard(gtx layout.Context, shaper *text.Shaper, item Item, tok resolvedTokens) layout.Dimensions {
	pad := gtx.Dp(unit.Dp(tok.spacing.S5))
	width := gtx.Constraints.Max.X

	inner := gtx
	inner.Constraints.Min = image.Point{}
	inner.Constraints.Max.X = max(0, width-2*pad)
	inner.Constraints.Max.Y = math.MaxInt32

	macro := op.Record(gtx.Ops)
	innerDims := drawCardContent(inner, shaper, item, tok)
	contentCall := macro.Stop()

	height := innerDims.Size.Y + 2*pad
	r := gtx.Dp(unit.Dp(tok.radius.Lg))
	rrect := clip.RRect{Rect: image.Rectangle{Max: image.Pt(width, height)}, SE: r, SW: r, NE: r, NW: r}

	paint.FillShape(gtx.Ops, tok.color.Surface, rrect.Op(gtx.Ops))
	strokeW := float32(gtx.Dp(unit.Dp(1)))
	paint.FillShape(gtx.Ops, tok.color.Ramps.Neutral.Step(500), clip.Stroke{Path: rrect.Path(gtx.Ops), Width: strokeW}.Op())

	off := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
	contentCall.Add(gtx.Ops)
	off.Pop()

	return layout.Dimensions{Size: image.Pt(width, height)}
}

// drawCardContent stacks the card's inner widgets — quote glyph, quote
// body, author block — top to bottom with S3 gaps between adjacent items.
func drawCardContent(gtx layout.Context, shaper *text.Shaper, item Item, tok resolvedTokens) layout.Dimensions {
	ws := []layout.Widget{
		quoteGlyphWidget(tok),
		quoteBodyWidget(shaper, item.Quote, tok),
		authorBlockWidget(shaper, item, tok),
	}
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

// quoteGlyphWidget paints an opening double-quotation glyph in Primary
// using a clip.Path. Two filled "comma" shapes — each a pentagon with a
// rectangular cap and a tail tapering down — sit side-by-side, separated
// by an S1 gap. The total size is roughly (2 × S3 + S1) × S4.
func quoteGlyphWidget(tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		commaW := gtx.Dp(unit.Dp(tok.spacing.S3))
		gap := gtx.Dp(unit.Dp(tok.spacing.S1))
		h := gtx.Dp(unit.Dp(tok.spacing.S4))
		totalW := 2*commaW + gap

		var path clip.Path
		path.Begin(gtx.Ops)
		appendComma(&path, 0, 0, commaW, h)
		appendComma(&path, commaW+gap, 0, commaW, h)
		paint.FillShape(gtx.Ops, tok.color.Primary, clip.Outline{Path: path.End()}.Op())

		return layout.Dimensions{Size: image.Pt(totalW, h)}
	}
}

// appendComma traces one pentagon-shaped "comma" into path. The cap is a
// rectangle covering the top 0.55h; the tail tapers from the cap's
// bottom-right to a point at (x + 0.3w, y + h).
func appendComma(path *clip.Path, x, y, w, h int) {
	fx, fy := float32(x), float32(y)
	fw, fh := float32(w), float32(h)
	path.MoveTo(f32.Pt(fx, fy))
	path.LineTo(f32.Pt(fx+fw, fy))
	path.LineTo(f32.Pt(fx+fw, fy+fh*0.55))
	path.LineTo(f32.Pt(fx+fw*0.3, fy+fh))
	path.LineTo(f32.Pt(fx, fy+fh*0.55))
	path.Close()
}

// quoteBodyWidget renders the quote text in the BodyLarge role in
// Text. Wrap to up to four lines so longer testimonials remain
// readable without growing the card unboundedly.
func quoteBodyWidget(shaper *text.Shaper, label string, tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if label == "" {
			return layout.Dimensions{}
		}
		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: tok.color.Text}.Add(gtx.Ops)
		material := mColor.Stop()
		wl := typeset.Label(tok.quote, 4)
		return typeset.Layout(gtx, shaper, wl, typeset.Font(tok.quote, font.Normal), unit.Sp(tok.quote.Size), label, material)
	}
}

// authorBlockWidget renders a horizontal row: the avatar (or its
// placeholder) followed by an S3 HSpacer and a vertical stack of the
// author's name and role. The row is cross-aligned to Middle so the
// avatar centres against the two-line text column.
func authorBlockWidget(shaper *text.Shaper, item Item, tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(avatarWidget(shaper, item, tok)),
			layout.Rigid(pllayout.HSpacer(tok.spacing.S3)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return pllayout.Col(gtx,
					textWidget(shaper, item.AuthorName, tok.color.Text, tok.body, font.SemiBold),
					textWidget(shaper, item.AuthorRole, tok.color.Ramps.Neutral.Step(700), tok.small, font.Normal),
				)
			}),
		)
	}
}

// avatarWidget renders the caller-supplied avatar widget clipped to an
// S8 × S8 square. When item.AuthorAvatar is nil, a border-stroked
// circular placeholder containing the first rune of item.AuthorName is
// drawn instead.
func avatarWidget(shaper *text.Shaper, item Item, tok resolvedTokens) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		size := gtx.Dp(unit.Dp(tok.spacing.S8))
		box := image.Pt(size, size)
		if item.AuthorAvatar != nil {
			cgtx := gtx
			cgtx.Constraints = layout.Exact(box)
			defer clip.UniformRRect(image.Rectangle{Max: box}, size/2).Push(gtx.Ops).Pop()
			item.AuthorAvatar(cgtx)
			return layout.Dimensions{Size: box}
		}
		drawPlaceholder(gtx, shaper, item.AuthorName, size, tok)
		return layout.Dimensions{Size: box}
	}
}

// drawPlaceholder paints a hollow step-500-stroked circle of diameter
// `size` and, when name is non-empty, the first rune centred inside it
// in BodyMedium neutral 700.
func drawPlaceholder(gtx layout.Context, shaper *text.Shaper, name string, size int, tok resolvedTokens) {
	r := size / 2
	stroke := float32(gtx.Dp(unit.Dp(1)))
	circle := clip.RRect{Rect: image.Rectangle{Max: image.Pt(size, size)}, SE: r, SW: r, NE: r, NW: r}
	paint.FillShape(gtx.Ops, tok.color.Ramps.Neutral.Step(500), clip.Stroke{Path: circle.Path(gtx.Ops), Width: stroke}.Op())
	if name == "" {
		return
	}
	first, _ := utf8.DecodeRuneInString(name)
	if first == utf8.RuneError {
		return
	}
	letter := string(first)

	letterGtx := gtx
	letterGtx.Constraints = layout.Constraints{Max: image.Pt(size, size)}
	mColor := op.Record(gtx.Ops)
	paint.ColorOp{Color: tok.color.Ramps.Neutral.Step(700)}.Add(gtx.Ops)
	material := mColor.Stop()
	mLabel := op.Record(gtx.Ops)
	wl := typeset.Label(tok.body, 1)
	wl.Alignment = text.Middle
	labelDims := typeset.Layout(letterGtx, shaper, wl, typeset.Font(tok.body, font.SemiBold), unit.Sp(tok.body.Size), letter, material)
	labelCall := mLabel.Stop()

	off := op.Offset(image.Pt((size-labelDims.Size.X)/2, (size-labelDims.Size.Y)/2)).Push(gtx.Ops)
	labelCall.Add(gtx.Ops)
	off.Pop()
}

// textWidget renders a single-line label in the supplied colour and text
// style, through spectrum/typeset so the role's line height is the height
// of the line box. Empty labels collapse to zero dimensions so adjacent
// section gaps are the only vertical contribution. A zero style weight
// (the legacy Render path synthesizes size-only styles) falls back to
// fallbackWeight, the pre-Typography hard-coded weight for the site.
func textWidget(shaper *text.Shaper, label string, fg color.NRGBA, style tokens.TextStyle, fallbackWeight font.Weight) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if label == "" {
			return layout.Dimensions{}
		}
		mColor := op.Record(gtx.Ops)
		paint.ColorOp{Color: fg}.Add(gtx.Ops)
		material := mColor.Stop()
		wl := typeset.Label(style, 1)
		return typeset.Layout(gtx, shaper, wl, typeset.Font(style, fallbackWeight), unit.Sp(style.Size), label, material)
	}
}
