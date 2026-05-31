// Package sidebar provides the Cadence Sidebar pattern: a collapsible
// vertical Surface column that swaps between an expanded width
// (label+icon) and a collapsed width (icon-only) on demand. The active
// Item is rendered with a Primary background tint.
//
// The package follows the Phase 4 Composition contract: Sidebar is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. Source is intentionally short — copy it into
// your own app and modify as needed.
package sidebar

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/event"
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

// Item is one entry in the sidebar's list. OnClick may be nil, in which
// case the item is treated as non-interactive and does not participate
// in focus traversal. Active selects the Primary background tint and is
// independent of OnClick.
type Item struct {
	Icon    layout.Widget
	Label   string
	OnClick func(gtx layout.Context)
	Active  bool
}

// Props configures a Sidebar.
type Props struct {
	Items []Item

	// Collapsed drives the expanded↔collapsed width swap. A nil Collapsed
	// is treated as a constant false (always expanded).
	Collapsed rx.Observable[bool]

	// OnToggleCollapse is invoked when the toggle affordance is clicked.
	// May be nil.
	OnToggleCollapse func(gtx layout.Context)

	// Shaper, if nil, defaults to a shaper backed by Go fonts. The
	// default shaper is created once per subscription inside the
	// rx.Defer scope, so it is not re-allocated on every theme change.
	Shaper *text.Shaper
}

// Width constants.
// SpacingScale tops out at S24 = 96 dp, so the "~S48" expanded width
// cited in PLAN G4.3b is materialised as a local 192 dp constant
// (≈ 4 × S12) rather than a new spacing-token field.
const (
	expandedDp  = 192
	collapsedDp = 48
	itemDp      = 48
	iconColDp   = 48
)

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	typ     tokens.TypeScale
}

// Sidebar returns an rx.Observable[layout.Widget] that emits a new
// widget whenever a consumed theme token or the Collapsed observable
// changes. Click handlers fire for any Item whose OnClick is non-nil
// (mouse or Space/Enter via widget.Clickable); Arrow-Up/Down move
// focus between items. Clicking the toggle affordance dispatches
// OnToggleCollapse.
func Sidebar(th rx.Observable[theme.Theme], props Props) rx.Observable[layout.Widget] {
	collapsed := props.Collapsed
	if collapsed == nil {
		collapsed = rx.Of(false)
	}
	resolved := rx.SwitchMap(th, func(t theme.Theme) rx.Observable[resolvedTokens] {
		return rx.Map(
			rx.CombineLatest3(t.Color, t.Spacing, t.Type),
			func(n rx.Tuple3[tokens.ColorTokens, tokens.SpacingScale, tokens.TypeScale]) resolvedTokens {
				return resolvedTokens{color: n.First, spacing: n.Second, typ: n.Third}
			},
		)
	})
	inputs := rx.CombineLatest2(resolved, collapsed)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		shaper := props.Shaper
		if shaper == nil {
			shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
		}
		clicks := make([]widget.Clickable, len(props.Items))
		var toggleTag toggleTag
		return rx.Map(inputs, func(next rx.Tuple2[resolvedTokens, bool]) layout.Widget {
			tok, col := next.First, next.Second
			return func(gtx layout.Context) layout.Dimensions {
				processInput(gtx, props, clicks, &toggleTag)
				return drawSidebar(gtx, shaper, props, clicks, &toggleTag, col, tok.color, tok.spacing, tok.typ)
			}
		})
	})
}

// Render produces a layout.Widget for a sidebar with pre-resolved
// tokens, an explicit collapsed flag, and no event processing.
// Intended for golden-image testing and static demonstrations;
// production code should use Sidebar.
func Render(
	shaper *text.Shaper,
	props Props,
	collapsed bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return drawSidebar(gtx, shaper, props, nil, nil, collapsed, colors, sp, ts)
	}
}

// toggleTag is a non-zero-size type so its address is a unique event
// tag for the toggle affordance's pointer hit area.
type toggleTag struct{ _ byte }

func processInput(gtx layout.Context, props Props, clicks []widget.Clickable, tt *toggleTag) {
	for i := range props.Items {
		if props.Items[i].OnClick != nil && clicks[i].Clicked(gtx) {
			props.Items[i].OnClick(gtx)
			// Pull focus to the clicked item so subsequent Arrow-Up/Down
			// traversal is anchored to it. widget.Clickable does not move
			// focus on pointer click by itself.
			gtx.Execute(key.FocusCmd{Tag: &clicks[i]})
		}
		for {
			e, ok := gtx.Event(key.Filter{Focus: &clicks[i], Name: key.NameUpArrow})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				if prev := focusableNeighbour(props.Items, i, -1); prev >= 0 {
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
				if next := focusableNeighbour(props.Items, i, +1); next >= 0 {
					gtx.Execute(key.FocusCmd{Tag: &clicks[next]})
				}
			}
		}
	}
	// Toggle: pointer-click only (no focus tag → never the FocusForward
	// target, so Arrow-Up/Down traversal is bounded by the items list).
	for {
		e, ok := gtx.Event(pointer.Filter{Target: tt, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			if props.OnToggleCollapse != nil {
				props.OnToggleCollapse(gtx)
			}
		}
	}
}

// focusableNeighbour returns the index of the nearest Item with a
// non-nil OnClick in direction dir (±1), or -1 if none exists.
func focusableNeighbour(items []Item, from, dir int) int {
	for i := from + dir; i >= 0 && i < len(items); i += dir {
		if items[i].OnClick != nil {
			return i
		}
	}
	return -1
}

func drawSidebar(
	gtx layout.Context,
	shaper *text.Shaper,
	props Props,
	clicks []widget.Clickable,
	tt *toggleTag,
	collapsed bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Dimensions {
	widthDp := float32(expandedDp)
	if collapsed {
		widthDp = collapsedDp
	}
	w := gtx.Dp(unit.Dp(widthDp))
	h := gtx.Constraints.Max.Y
	size := image.Pt(w, h)

	paint.FillShape(gtx.Ops, colors.Surface, clip.Rect{Max: size}.Op())

	// Toggle affordance at the top.
	toggleH := gtx.Dp(unit.Dp(itemDp))
	drawToggle(gtx, tt, image.Pt(w, toggleH), colors)

	// Items stacked vertically below the toggle.
	itemH := gtx.Dp(unit.Dp(itemDp))
	for i, it := range props.Items {
		off := image.Pt(0, toggleH+i*itemH)
		stk := op.Offset(off).Push(gtx.Ops)
		drawItem(gtx, shaper, it, clickFor(clicks, i), image.Pt(w, itemH), collapsed, colors, sp, ts)
		stk.Pop()
	}

	return layout.Dimensions{Size: size}
}

func clickFor(clicks []widget.Clickable, i int) *widget.Clickable {
	if i >= len(clicks) {
		return nil
	}
	return &clicks[i]
}

// drawToggle paints a chevron-like glyph centred in a (w × h) area at
// the current offset and registers a pointer.Press hit area against tt.
// In test or static rendering (tt == nil) only the glyph is drawn.
func drawToggle(gtx layout.Context, tt *toggleTag, size image.Point, colors tokens.ColorTokens) {
	// Glyph: a centred filled square as a deterministic affordance icon.
	g := gtx.Dp(unit.Dp(16))
	gx := (size.X - g) / 2
	gy := (size.Y - g) / 2
	rect := image.Rect(gx, gy, gx+g, gy+g)
	paint.FillShape(gtx.Ops, colors.OnSurfaceVariant, clip.Rect(rect).Op())

	if tt == nil {
		return
	}
	area := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, tt)
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()
}

func drawItem(
	gtx layout.Context,
	shaper *text.Shaper,
	item Item,
	click *widget.Clickable,
	size image.Point,
	collapsed bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	ts tokens.TypeScale,
) layout.Dimensions {
	inner := func(gtx layout.Context) layout.Dimensions {
		if item.Active {
			paint.FillShape(gtx.Ops, primaryTint(colors.Primary), clip.Rect{Max: size}.Op())
		}

		iconW := gtx.Dp(unit.Dp(iconColDp))
		if iconW > size.X {
			iconW = size.X
		}

		// Icon slot: centred inside the leading iconCol.
		if item.Icon != nil {
			iconGtx := gtx
			iconGtx.Constraints = layout.Constraints{
				Min: image.Point{},
				Max: image.Pt(iconW, size.Y),
			}
			st := op.Offset(image.Point{}).Push(gtx.Ops)
			rec := op.Record(gtx.Ops)
			d := item.Icon(iconGtx)
			call := rec.Stop()
			offX := (iconW - d.Size.X) / 2
			offY := (size.Y - d.Size.Y) / 2
			if offX < 0 {
				offX = 0
			}
			if offY < 0 {
				offY = 0
			}
			st.Pop()
			stk := op.Offset(image.Pt(offX, offY)).Push(gtx.Ops)
			call.Add(gtx.Ops)
			stk.Pop()
		}

		// Label slot: trailing, hidden when collapsed.
		if !collapsed && size.X > iconW {
			padH := gtx.Dp(unit.Dp(sp.S2))
			labelMaxW := size.X - iconW - padH
			if labelMaxW > 0 {
				mColor := op.Record(gtx.Ops)
				paint.ColorOp{Color: colors.OnSurface}.Add(gtx.Ops)
				textMaterial := mColor.Stop()

				labelGtx := gtx
				labelGtx.Constraints.Min = image.Point{}
				labelGtx.Constraints.Max.X = labelMaxW
				labelGtx.Constraints.Max.Y = size.Y

				mLabel := op.Record(gtx.Ops)
				wl := widget.Label{MaxLines: 1}
				labelDims := wl.Layout(labelGtx, shaper, font.Font{}, unit.Sp(ts.LabelLarge), item.Label, textMaterial)
				labelCall := mLabel.Stop()

				offY := (size.Y - labelDims.Size.Y) / 2
				stk := op.Offset(image.Pt(iconW, offY)).Push(gtx.Ops)
				labelCall.Add(gtx.Ops)
				stk.Pop()
			}
		}
		return layout.Dimensions{Size: size}
	}

	if click == nil || item.OnClick == nil {
		gtx.Constraints = layout.Exact(size)
		return inner(gtx)
	}
	gtx.Constraints = layout.Exact(size)
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.LabelOp(item.Label).Add(gtx.Ops)
		semantic.EnabledOp(true).Add(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
		return inner(gtx)
	})
}

// primaryTint returns Primary at ~20% alpha so the underlying Surface
// remains visible. The Tint depth is chosen to register a non-trivial
// pixel delta on both light and dark schemes.
func primaryTint(p color.NRGBA) color.NRGBA {
	return color.NRGBA{R: p.R, G: p.G, B: p.B, A: 0x33}
}
