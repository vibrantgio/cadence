// Package accordion provides the Cadence Accordion pattern: a vertical
// stack of collapsible Section groups. Each Section has a Title header
// row with a chevron rotated per open state, and an optional Body widget
// shown beneath the header when the Section is open. When SingleOpen is
// true, activating a closed Section first dispatches OnToggle for every
// currently-open Section so the parent's flip-the-bool handler converges
// on a single-open state without additional bookkeeping.
//
// The package follows the Phase 4 Composition contract: Accordion is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. Source is intentionally short and free of
// opaque configuration — copy it into your own app and modify as needed.
//
// Note on prism/button: the implementation plan suggested reusing
// prism/button for header rows, but a Button renders with a Primary
// background fill, 6 dp corner radius, and 44 dp minimum height — none
// of which fit a full-width accordion header. The headers here use the
// same widget.Clickable + custom rendering pattern as cadence/navbar,
// cadence/sidebar, and cadence/tabs, which faced the same mismatch.
package accordion

import (
	"image"
	"image/color"

	"gioui.org/f32"
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

// Section is one entry in the accordion's vertical stack. Body may be
// nil; a nil Body renders the header row only, even when the Section is
// open.
type Section struct {
	Title string
	Body  layout.Widget
}

// Props configures an Accordion.
type Props struct {
	Sections []Section

	// Open drives which sections are rendered open. A nil Open is treated
	// as a constant empty map (all sections closed). The map is read by
	// index; absent keys are equivalent to false.
	Open rx.Observable[map[int]bool]

	// OnToggle is invoked when the user activates a header — via pointer
	// click, Enter, or Space. May be nil. In SingleOpen mode, opening a
	// closed Section first invokes OnToggle for every currently-open
	// peer Section before invoking OnToggle for the activated index.
	OnToggle func(idx int)

	// SingleOpen enforces the single-open invariant on activation: when
	// true, opening a closed Section first closes every other open peer
	// by calling OnToggle on each.
	SingleOpen bool

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The
	// default shaper is created once per subscription inside the
	// rx.Defer scope, so it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

// Layout constants. headerHDp and bodyHDp are deliberately chosen so a
// three-section accordion with one open body packs to 240 dp tall,
// matching the canonical golden canvas.
const (
	headerHDp     = 48
	bodyHDp       = 96
	chevronColDp  = 32
	chevronSizeDp = 10
	dividerDp     = 1
)

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	typ     tokens.TypeScale
}

// Accordion returns an rx.Observable[layout.Widget] that emits a new
// widget whenever a consumed theme token or the Open observable changes.
// Pointer clicks, Enter, and Space on a focused header invoke OnToggle.
// Arrow-Up/Down move focus between section headers (no wrap).
func Accordion(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	open := props.Open
	if open == nil {
		open = rx.Of(map[int]bool{})
	}
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest3(t.Color, t.Spacing, t.Type),
			func(n rx.Tuple3[tokens.ColorTokens, tokens.SpacingScale, tokens.TypeScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, typ: n.Third}
			},
		)
	})
	inputs := rx.CombineLatest2(resolved, open)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}
		clicks := make([]widget.Clickable, len(props.Sections))
		return rx.Map(inputs, func(n rx.Tuple2[resolvedTokens, map[int]bool]) layout.Widget {
			tok, openMap := n.First, n.Second
			return func(gtx layout.Context) layout.Dimensions {
				processInput(gtx, props, clicks, openMap)
				return drawAccordion(gtx, shaper, props, clicks, openMap, tok.color, tok.spacing, tok.typ)
			}
		})
	})
}

// Render produces a layout.Widget for an accordion with a fixed open
// map and no event processing. Intended for golden-image testing and
// static demonstrations; production code should use Accordion.
func Render(
	shaper *text.Shaper,
	props Props,
	open map[int]bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return drawAccordion(gtx, shaper, props, nil, open, colors, sp, ts)
	}
}

// processInput drains click and arrow-key events for each section
// header. widget.Clickable's default key filters cover both Enter and
// Space, so a single Clicked() check captures pointer and keyboard
// activation paths uniformly.
func processInput(gtx layout.Context, props Props, clicks []widget.Clickable, openMap map[int]bool) {
	activate := func(i int) {
		// In SingleOpen mode, opening a currently-closed section closes
		// every other currently-open section first. Closes are emitted in
		// ascending index order so the OnToggle call sequence is
		// deterministic. The captured openMap is a snapshot from the
		// inputs emission that produced this widget; the parent's flip-
		// the-bool handler reaches a single-open state on the next
		// emission regardless of how many sections were open in the
		// snapshot.
		if props.SingleOpen && !openMap[i] && props.OnToggle != nil {
			for j := range props.Sections {
				if j != i && openMap[j] {
					props.OnToggle(j)
				}
			}
		}
		if props.OnToggle != nil {
			props.OnToggle(i)
		}
	}
	for i := range props.Sections {
		if clicks[i].Clicked(gtx) {
			activate(i)
			// Pull focus to the activated header so subsequent arrow
			// traversal is anchored to it.
			gtx.Execute(key.FocusCmd{Tag: &clicks[i]})
		}
		for {
			e, ok := gtx.Event(key.Filter{Focus: &clicks[i], Name: key.NameUpArrow})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				if prev := i - 1; prev >= 0 {
					gtx.Execute(key.FocusCmd{Tag: &clicks[prev]})
				}
			}
		}
		for {
			e, ok := gtx.Event(key.Filter{Focus: &clicks[i], Name: key.NameDownArrow})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				if next := i + 1; next < len(props.Sections) {
					gtx.Execute(key.FocusCmd{Tag: &clicks[next]})
				}
			}
		}
	}
}

func drawAccordion(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	clicks []widget.Clickable,
	openMap map[int]bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Dimensions {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, colors.Surface, clip.Rect{Max: size}.Op())

	headerH := gtx.Dp(unit.Dp(headerHDp))
	bodyH := gtx.Dp(unit.Dp(bodyHDp))

	y := 0
	for i, sec := range props.Sections {
		hSize := image.Pt(size.X, headerH)
		stH := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		hGtx := gtx
		hGtx.Constraints = layout.Exact(hSize)
		drawHeader(hGtx, shaper, sec, clickFor(clicks, i), openMap[i], hSize, colors, sp, ts)
		stH.Pop()
		y += headerH

		if openMap[i] && sec.Body != nil {
			bSize := image.Pt(size.X, bodyH)
			stB := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			bGtx := gtx
			bGtx.Constraints = layout.Exact(bSize)
			sec.Body(bGtx)
			stB.Pop()
			y += bodyH
		}
	}

	return layout.Dimensions{Size: size}
}

func clickFor(clicks []widget.Clickable, i int) *widget.Clickable {
	if i >= len(clicks) {
		return nil
	}
	return &clicks[i]
}

func drawHeader(
	gtx layout.Context,
	shaper *text.Shaper,
	sec Section,
	click *widget.Clickable,
	open bool,
	size image.Point,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Dimensions {
	chevW := gtx.Dp(unit.Dp(chevronColDp))
	if chevW > size.X {
		chevW = size.X
	}
	padH := gtx.Dp(unit.Dp(sp.S3))

	inner := func(gtx layout.Context) layout.Dimensions {
		// Chevron, centred inside the leading icon column.
		drawChevron(gtx, open, image.Pt(chevW, size.Y), colors.OnSurface)

		// Title label, trailing the chevron column.
		labelMaxW := size.X - chevW - padH
		if labelMaxW > 0 {
			labelGtx := gtx
			labelGtx.Constraints.Min = image.Point{}
			labelGtx.Constraints.Max.X = labelMaxW
			labelGtx.Constraints.Max.Y = size.Y

			mColor := op.Record(gtx.Ops)
			paint.ColorOp{Color: colors.OnSurface}.Add(gtx.Ops)
			material := mColor.Stop()

			mLabel := op.Record(gtx.Ops)
			wl := widget.Label{MaxLines: 1}
			labelDims := wl.Layout(labelGtx, shaper, font.Font{}, unit.Sp(ts.LabelLarge), sec.Title, material)
			labelCall := mLabel.Stop()

			offY := (size.Y - labelDims.Size.Y) / 2
			if offY < 0 {
				offY = 0
			}
			st := op.Offset(image.Pt(chevW, offY)).Push(gtx.Ops)
			labelCall.Add(gtx.Ops)
			st.Pop()
		}

		// Bottom divider so adjacent headers are visually separated even
		// when no body is rendered between them.
		divH := gtx.Dp(unit.Dp(dividerDp))
		if divH < 1 {
			divH = 1
		}
		divRect := image.Rect(0, size.Y-divH, size.X, size.Y)
		paint.FillShape(gtx.Ops, colors.Outline, clip.Rect(divRect).Op())

		return layout.Dimensions{Size: size}
	}

	gtx.Constraints = layout.Exact(size)
	if click == nil {
		return inner(gtx)
	}
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.LabelOp(sec.Title).Add(gtx.Ops)
		semantic.EnabledOp(true).Add(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
		return inner(gtx)
	})
}

// drawChevron draws a small filled triangle inside a col-sized column at
// the current offset. Closed (open=false) points right; open=true points
// down, giving the same bounding box rotated 90°.
func drawChevron(gtx layout.Context, open bool, col image.Point, c color.NRGBA) {
	chev := gtx.Dp(unit.Dp(chevronSizeDp))
	if chev > col.X {
		chev = col.X
	}
	if chev > col.Y {
		chev = col.Y
	}
	ox := (col.X - chev) / 2
	oy := (col.Y - chev) / 2

	var p clip.Path
	p.Begin(gtx.Ops)
	if open {
		// Pointing down: top-left → bottom-centre → top-right.
		p.MoveTo(f32.Pt(float32(ox), float32(oy)))
		p.LineTo(f32.Pt(float32(ox+chev/2), float32(oy+chev)))
		p.LineTo(f32.Pt(float32(ox+chev), float32(oy)))
	} else {
		// Pointing right: top-left → middle-right → bottom-left.
		p.MoveTo(f32.Pt(float32(ox), float32(oy)))
		p.LineTo(f32.Pt(float32(ox+chev), float32(oy+chev/2)))
		p.LineTo(f32.Pt(float32(ox), float32(oy+chev)))
	}
	p.Close()
	spec := p.End()
	paint.FillShape(gtx.Ops, c, clip.Outline{Path: spec}.Op())
}
