// Package sidebar provides the Cadence Sidebar pattern: a collapsible
// vertical Surface column that swaps between an expanded width
// (label+icon) and a collapsed width (icon-only) on demand. The active
// Item is rendered on the Primary ramp's selected step (ADR-007's
// two-step walk past the Surface ground).
//
// The package follows the Phase 4 Composition contract: Sidebar is a
// callable Go function consuming a Prism theme observable, returning a
// stream of layout.Widget. Source is intentionally short — copy it into
// your own app and modify as needed.
//
// The column's width is not negotiable: 192 dp expanded and 48 dp
// collapsed, both fixed constants in this file that ignore the horizontal
// constraint entirely. Height is whatever it is handed. FX.6 considered
// making the widths respond — to density, or to the horizontal
// constraint — and kept them fixed: E1.4 scopes density to vertical
// rhythm (control heights), and clamping to the constraint would
// introduce a third, unpredictable width where the expanded↔collapsed
// swap between two known numbers is the pattern's contract. Vertical
// overflow was irrecoverable (content unreachable, hence the scroll
// region below); horizontal space is the caller's explicit allocation,
// and a caller wanting a different rail width copies the file, per the
// Composition contract above. Collapsed is an
// rx.Observable[bool] the caller owns — the sidebar renders that state
// and does not hold it — and OnToggleCollapse is the request to change
// it, so wiring the affordance to nothing leaves a sidebar that cannot
// collapse.
//
// Items are stacked at the density's row pitch — exactly
// Density.ControlHeight (E1.4; 36 dp Comfortable, 28 dp Compact) — in a
// prism/list scroll region filling the column below the toggle (FX.6):
// a list longer than the column is tall scrolls by wheel or touch
// instead of painting past the bottom edge. No scrollbar is drawn — the
// bare list.Layout, the same idiom cadence/table's body uses. Items are
// stacked full-width rows, so each row's hit area stays the row bounds
// (extending it to the 44 dp pointer floor would steal the neighbouring
// row's slop).
//
// Keyboard reach stops at the items. Each Item with a non-nil OnClick
// takes focus, and Arrow-Up/Down move between them, skipping the ones
// without; the collapse affordance deliberately registers no focus tag —
// it answers pointer clicks only — so that Arrow traversal stays bounded
// by the item list. The scroll region virtualizes offscreen rows, so a
// row's focus tag is registered only while it is laid out: Arrow
// traversal reaches the rows currently in view. Its glyph is a
// placeholder filled square until prism/icon lands.
package sidebar

import (
	"image"

	"gioui.org/font"
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
	"github.com/vibrantgio/prism/list"
	"github.com/vibrantgio/spectrum/theme"
	"github.com/vibrantgio/spectrum/tokens"
)

// Item is one entry in the sidebar's list. OnClick may be nil, in which
// case the item is treated as non-interactive and does not participate
// in focus traversal. Active selects the Primary selected-state
// background and is independent of OnClick.
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

	// Shaper is an explicit per-instance override of the text shaper.
	// Leave it nil in normal use: the sidebar then shapes its item labels
	// with the theme's shaper (Typography.Shaper()), which is built once
	// and cached inside the theme's Typography value. Set it only when
	// this instance must shape with a different shaper than the theme
	// provides.
	Shaper *text.Shaper
}

// Width constants.
// SpacingScale tops out at S24 = 96 dp, so the "~S48" expanded width
// cited in PLAN G4.3b is materialised as a local 192 dp constant
// (≈ 4 × S12) rather than a new spacing-token field. Widths do not
// follow density (the column contract is fixed; FX.6 revisited and kept
// this — see the package comment); the item and toggle heights do —
// both are exactly Density.ControlHeight (E1.4 row rule).
const (
	expandedDp  = 192
	collapsedDp = 48
	iconColDp   = 48
)

type resolvedTokens struct {
	color   tokens.ColorTokens
	spacing tokens.SpacingScale
	label   tokens.TextStyle // the LabelLarge role: typeface, weight, size, line height
	density tokens.Density   // item/toggle height source (E1.4)
	shaper  *text.Shaper     // the theme's shaper; nil in the Render path
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
	inputs := rx.CombineLatest2(resolved, collapsed)
	return rx.Defer(func() rx.Observable[layout.Widget] {
		clicks := make([]widget.Clickable, len(props.Items))
		state := list.NewState()
		var toggleTag toggleTag
		return rx.Map(inputs, func(next rx.Tuple2[resolvedTokens, bool]) layout.Widget {
			tok, col := next.First, next.Second
			// Props.Shaper is an explicit override; the theme's shaper is
			// the default.
			shaper := props.Shaper
			if shaper == nil {
				shaper = tok.shaper
			}
			return func(gtx layout.Context) layout.Dimensions {
				processInput(gtx, props, clicks, &toggleTag)
				return drawSidebar(gtx, shaper, props, clicks, &toggleTag, state, col, tok.color, tok.spacing, tok.label, tok.density)
			}
		})
	})
}

// Render produces a layout.Widget for a sidebar with pre-resolved
// tokens, an explicit collapsed flag, and no event processing.
// Intended for golden-image testing and static demonstrations;
// production code should use Sidebar, which reads both of the parameters
// below off the theme.
//
// label is the LabelLarge role's whole text style — typeface, weight,
// size and line height all reach the shaper — and d is the density the
// column draws at (item rows and the collapse toggle are each exactly
// Density.ControlHeight). Pass tokens.DefaultTypography.LabelLarge and
// tokens.Comfortable for the default desktop look; before F3.4 the
// static path was pinned to Comfortable with no way to say otherwise.
func Render(
	shaper *text.Shaper,
	props Props,
	collapsed bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	label tokens.TextStyle,
	d tokens.Density,
) layout.Widget {
	state := list.NewState()
	return func(gtx layout.Context) layout.Dimensions {
		return drawSidebar(gtx, shaper, props, nil, nil, state, collapsed, colors, sp, label, d)
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
	state *list.State,
	collapsed bool,
	colors tokens.ColorTokens,
	sp tokens.SpacingScale,
	style tokens.TextStyle,
	d tokens.Density,
) layout.Dimensions {
	widthDp := float32(expandedDp)
	if collapsed {
		widthDp = collapsedDp
	}
	w := gtx.Dp(unit.Dp(widthDp))
	h := gtx.Constraints.Max.Y
	size := image.Pt(w, h)

	paint.FillShape(gtx.Ops, colors.Surface, clip.Rect{Max: size}.Op())

	// Toggle affordance at the top: a row like the items, so it shares
	// the density's control height.
	toggleH := gtx.Dp(unit.Dp(d.ControlHeight))
	drawToggle(gtx, tt, image.Pt(w, toggleH), colors)

	// Items below the toggle, in a prism/list scroll region filling the
	// rest of the column (FX.6) — the bare list.Layout, like table's
	// body: wheel/touch scrolling, no scrollbar. Each row is a full-width
	// row at the density's pitch (E1.4 row rule: exactly ControlHeight,
	// which is what list.RowHeight resolves to).
	listH := h - toggleH
	if listH <= 0 {
		return layout.Dimensions{Size: size}
	}
	itemH := gtx.Dp(list.RowHeight(d))
	stk := op.Offset(image.Pt(0, toggleH)).Push(gtx.Ops)
	lGtx := gtx
	lGtx.Constraints = layout.Exact(image.Pt(w, listH))
	idx := make([]int, len(props.Items))
	for i := range idx {
		idx[i] = i
	}
	list.Layout(lGtx, state, idx, func(rGtx layout.Context, i int) layout.Dimensions {
		return drawItem(rGtx, shaper, props.Items[i], clickFor(clicks, i), image.Pt(w, itemH), collapsed, colors, sp, style)
	})
	stk.Pop()

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
	paint.FillShape(gtx.Ops, colors.Ramps.Neutral.Step(700), clip.Rect(rect).Op())

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
	style tokens.TextStyle,
) layout.Dimensions {
	inner := func(gtx layout.Context) layout.Dimensions {
		if item.Active {
			// Selected background per ADR-007: a two-step walk past the
			// sidebar's Surface ground (200 → 400) on the Primary ramp,
			// keeping the highlight's primary hue as a real, addressable
			// colour instead of the old 20%-alpha Primary tint.
			active := colors.StateColor(tokens.RolePrimary, 200, tokens.StateSelected)
			paint.FillShape(gtx.Ops, active, clip.Rect{Max: size}.Op())
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
				paint.ColorOp{Color: colors.Text}.Add(gtx.Ops)
				textMaterial := mColor.Stop()

				labelGtx := gtx
				labelGtx.Constraints.Min = image.Point{}
				labelGtx.Constraints.Max.X = labelMaxW
				labelGtx.Constraints.Max.Y = size.Y

				// Shape with the LabelLarge role's typeface, weight, size
				// and line height. Zero fields (the legacy Render path
				// synthesizes a size-only style) fall back to the shaper's
				// defaults.
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
				labelDims := wl.Layout(labelGtx, shaper, f, unit.Sp(style.Size), item.Label, textMaterial)
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
