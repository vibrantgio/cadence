# cadence

The pattern layer of [Vibrant Gio](https://github.com/vibrantgio), a design
system for native desktop applications on macOS, Windows and Linux, written in
pure Go on [Gio](https://gioui.org). Where prism gives you a button, cadence
gives you the eighteen composed things an application is actually made of — an
application shell, a navbar, a sidebar, a virtualised data table, a modal, a
toast stack, a hero section.

Every one of them is the part you would otherwise write by hand and get subtly
wrong: the modal that traps Tab and lets Escape out, the popover that dismisses
when you open another one, the tooltip that is the only tooltip on screen, the
table that lays out only the rows you can see. Each pattern reads its visual
values from [spectrum](https://github.com/vibrantgio/spectrum)'s theme
observable, and the theme carries the whole look: colour, typography, density,
elevation and motion. A window follows the OS between light and dark with no
application code; switching an app to Compact density resizes the navbar,
sidebar items, tabs, pagination and table rows as a theme change, not a sweep;
overlay surfaces name their rung on the elevation ladder and fill from
`SurfaceAt` — the modal and toast at level 2, tonal in both modes — with cast
shadows reserved, per ADR-005, for the surfaces that float and can leave (the
toast; not the card).

Every package has the same two entry points, and the split is deliberate:

- **The live form** — `shell.Shell`, `table.Table`, `modal.Modal`, … — takes an
  `rx.Observable[theme.Theme]` plus a props struct and returns an
  `rx.Observable[layout.Widget]`. Dynamic state arrives as observables too
  (`modal.Props.Open`, `table.Props.Items`, `accordion.Props.Open`), and
  interaction state — the `widget.Clickable`s, the drag position — is allocated
  inside the pattern's `rx.Defer` scope so it survives the view rebuilds an MVU
  loop drives.
- **The static form** — `Render(shaper, props, <state>, colors, spacing,
  radius, <type>[, density])` — takes resolved tokens and the state as plain
  values and draws one frame with no event handling. That is what the
  golden-image tests drive, and what to use for static rendering. `shell` adds
  `RenderThreeColumn` and `RenderStackedPage` for the layouts whose slots are
  streams in the live form. Drive patterns through their live entry points
  unless you are rendering a static frame.

  Two rules fix the tail of every static signature, both settled in v0.3.0.
  A pattern that draws one type role takes that role's whole
  `tokens.TextStyle` — typeface, weight, size and line height, exactly what
  the live path reads off the theme — while one that spends several (`hero`,
  `pricing`, `feature`, `testimonial`) takes the whole `tokens.Typography`
  and picks its own roles, as it does live. And a `tokens.Density` follows
  only where the pattern sizes a control: `navbar`, `sidebar`, `tabs`,
  `table`, `pagination`, `shell`, `hero`, `pricing` and `modal` take one;
  `alert`, `accordion`, `breadcrumb`, `tooltip`, `toast`, `feature`,
  `testimonial` and `table.RenderTextCell` do not, because nothing in them
  has a control height. Until v0.3.0 these signatures took a
  `tokens.TypeScale` and rendered at a hardcoded `tokens.Comfortable`.

Typography is theme-owned: in the live form every pattern that draws text
shapes with the theme's `Typography.Shaper()` and the Material Design 3 text
styles it carries, so leaving `Props.Shaper` nil is the normal case.
`Props.Shaper` is an explicit per-instance override for the rare pattern
instance that must shape with a different shaper than the theme provides.

The source of each pattern is short and free of opaque configuration on
purpose. When a pattern is nearly what you want, copying its file into your
application and editing it is a supported outcome, not a defeat — several are a
couple of hundred lines, and the props struct is not trying to anticipate you.

## Where it sits

Tier 4 of the stack — `mvu → spectrum → prism → pulse → cadence → markdown` —
alongside [markdown](https://github.com/vibrantgio/markdown). cadence imports
`theme` and `tokens` from [spectrum](https://github.com/vibrantgio/spectrum),
`button`, `coordination`, `icon`, `layout` and `list` from
[prism](https://github.com/vibrantgio/prism), plus `depth` and `tween` from
[pulse](https://github.com/vibrantgio/pulse); [mvu](https://github.com/vibrantgio/mvu)
it uses only indirectly, through those. Nothing inside the design system
imports cadence — the [workbench](https://github.com/vibrantgio/workbench)
applications are its consumers. The
[organization page](https://github.com/vibrantgio) has the full tier table.

```sh
go get github.com/vibrantgio/cadence
```

Every module in the organization is on gioui.org v0.10.1,
github.com/reactivego/rx v0.3.0 and Go 1.25.1.

## Packages

**Shells and navigation** — the frame an application lives in.

| Package | |
| --- | --- |
| `shell` | The top-level layout, in four variants: `SidebarHeaderMain`, `SplitPane` (draggable divider on either axis), `ThreeColumn` (navbar, sidebar, main, resizable aside, footer strip) and `StackedPage` (pinned navbar over a shell-owned scroll of page sections). |
| `navbar` | A horizontal surface bar with three slots — leading brand, centred links, trailing actions. The active link carries a Primary underline. |
| `sidebar` | A collapsible vertical column that swaps between an expanded width (icon + label) and a collapsed width (icon only). The active item is tinted Primary. |
| `tabs` | A tab strip with a Primary underline on the selection, plus the content panel below it. Click, Arrow-Left/Right (wrapping), Home and End all change the selection. |
| `breadcrumb` | A chevron-separated row of location segments. The last renders as the current location in a deep neutral text step; the ones before it are clickable. |

**Data and content** — the things that hold a screenful of stuff.

| Package | |
| --- | --- |
| `table` | The sortable, virtualised data table, built on `prism/list`: only the visible rows lay out, whatever the row count. Sort and filter are external — the `Items` observable emits already-sorted, already-filtered slices and the header surfaces intent through `OnSort`. Row heights follow the theme's density. |
| `pagination` | A row of numbered page buttons flanked by prev/next chevrons, the current page highlighted Primary/OnPrimary. |
| `card` | A rounded surface with optional Header / Body / Footer slots, in an outlined (1 dp stroke on the level-1 surface) or elevated variant — the latter a level-2 tonal fill. A card is raised in place, not floating, so neither variant casts a shadow (ADR-005; E2.2 retired the elevated card's `pulse/depth` call). |
| `accordion` | A vertical stack of collapsible sections with a rotating chevron. `SingleOpen` makes activating a closed section first toggle every open peer, so a parent's flip-the-bool handler converges on single-open with no extra bookkeeping. |

**Overlays and feedback** — the things that draw over everything else.

| Package | |
| --- | --- |
| `modal` | A centred dialog over a full-window scrim, its surface a level-2 fill from the elevation ladder: header, padded body, footer actions. Escape and a backdrop click close it, Tab and Shift+Tab cycle inside it and cannot escape to the background, and only the topmost modal on the exported `Stack` receives input. Footer actions own their own focus tags, so a focused action shows exactly one ring. |
| `popover` | An anchored elevated surface with a triangular tail pointing at a caller-supplied anchor. Outside-click dismissal and popover-vs-popover arbitration run through `prism/coordination` — opening a second popover dismisses the first. |
| `tooltip` | A hover/focus annotation next to a trigger after a delay. `DefaultDelay` resolves from the token motion scale's `DurXSlow` stop (500 ms), and the live form re-times from the theme's `Motion` observable. Arbitration keeps exactly one tooltip visible across the window. |
| `toast` | A position-anchored column of transient notifications, each on a level-2 surface with a `pulse/depth` cast shadow — a toast floats and can leave, which is exactly what ADR-005 reserves shadows for. Application code calls the package-scoped `Notify`; any active `Stack` renders the queue in its chosen corner and each toast fades out through `pulse/tween` across the theme's `DurSlow` stop at the end of its `Lifetime` (`DefaultLifetime`, 4 s). |
| `alert` | A tinted-surface banner with a leading variant icon, a title and an arbitrary body widget. Info, Success, Warning, Error. |

**Marketing** — the landing-page sections, for the app's own front door.

| Package | |
| --- | --- |
| `hero` | The landing block: optional eyebrow tag, display title, subtitle, optional visual slot, and a primary/secondary CTA pair. With no visual it is one centred column; with one it splits into two equal columns. |
| `feature` | An icon–title–body grid laid out `Columns × N`. The icon slot is opaque — any `layout.Widget`. |
| `pricing` | A row of tier cards — name, price and cadence, a checkmarked feature list, a CTA — with one tier optionally highlighted, which swaps the 1 dp outline for a 2 dp Primary border and adds a "Popular" chip. |
| `testimonial` | Quote cards with an author block and an avatar (or an initial in a circular placeholder), as a single centred card or a row of them. |

`modal/gallery` is a `main` inside this module, not a nineteenth pattern: it
demonstrates the modal's Tab cycle and focus-ring ownership. Run it with `go
run ./modal/gallery`.

## Usage

Patterns compose by handing one pattern's stream to another's slot. This is
`landing.go` from
[workbench/sitedocs](https://github.com/vibrantgio/workbench/tree/master/sitedocs)
— the marketing patterns mounted as the scrolling sections of a
`StackedPage` shell, which pins the navbar, owns the scroll region and re-emits
whenever any section emits. Note that nothing passes a shaper — the theme
carries the typography:

```go
gap := rx.Of[layout.Widget](pllayout.VSpacer(sectionGapDp))
return shell.Shell(th, shell.Props{
	Layout:          shell.StackedPage,
	ContentMaxWidth: contentMaxWidthDp, // centred reading column; navbar stays full-bleed
	Navbar:          navbarProps(mirrorTokens(th), pageHome),
	Sections: []rx.Observable[layout.Widget]{
		hero.Hero(th, heroContent(gotoDocs, gotoAbout)),
		gap,
		feature.Feature(th, featureContent()),
		gap,
		pricing.Pricing(th, pricingContent()),
		gap,
		testimonial.Testimonial(th, testimonialContent()),
	},
})
```

The props are plain data — `hero.Props{Eyebrow, Title, Subtitle, PrimaryCTA,
SecondaryCTA}`, `feature.Props{Columns, Items}` — so the copy lives in its own
file and the layout file stays structural.

A table is columns plus a row stream. This is condensed from `maincontent.go`
in
[workbench/watchlist](https://github.com/vibrantgio/workbench/tree/master/watchlist),
where the rows are one page of a watchlist and every interaction lands an MVU
message:

```go
columns := []table.Column[symbolRow]{
	{Header: "", Width: unit.Dp(selColWDp), Cell: checkboxCell}, // leading gutter
	{Header: "Symbol", Cell: symbolCell},                        // zero Width flexes
	{Header: "Exchange", Width: unit.Dp(exchColWDp), Cell: cellText(...)},
	{Header: "Notes", Width: unit.Dp(notesColWDp), Cell: cellText(...)},
}

tableObs := table.Table(th, table.Props[symbolRow]{
	Columns: columns,
	Items:   rowsObs, // already paged, sorted and filtered by the consumer
})
```

`Cell` is called fresh for every visible row on every frame, so the table holds
no per-row state. Anything stateful in a cell — a checkbox, an editor, a
per-row confirm popover — is kept alive by the consumer through
`prism/keyed.Defer`, which returns the same pointer for the same row key across
sort, filter and pagination:

```go
checkClicks := keyed.Defer(func(int) *widget.Clickable { return &widget.Clickable{} })

checkboxCell := func(r symbolRow) layout.Widget {
	click := checkClicks.For(r.idx) // r.idx is the absolute row index
	return func(gtx layout.Context) layout.Dimensions {
		if click.Clicked(gtx) {
			mvu.MessageOp{Message: ToggleSelect{Row: r.idx}}.Add(gtx.Ops)
		}
		// ... click.Layout, semantic label, draw
	}
}
```

Overlays are folded onto the shell stream and drawn after it, reporting the
shell's dimensions — the modal scrim and the toast column both need the whole
window. Both `feeds` and `watchlist` do exactly this:

```go
toastObs := toast.Stack(th, toast.Props{Position: toast.TopRight})

return rx.Map(rx.CombineLatest3(shellObs, modalObs, toastObs),
	func(n rx.Tuple3[layout.Widget, layout.Widget, layout.Widget]) layout.Widget {
		shellW, modalW, toastW := n.First, n.Second, n.Third
		return func(gtx layout.Context) layout.Dimensions {
			dims := shellW(gtx)
			if modalW != nil {
				modalW(gtx)
			}
			if toastW != nil {
				toastW(gtx)
			}
			return dims
		}
	},
)
```

Anywhere in the application, `toast.Notify(toast.Success, "Feed added")` puts a
toast in that column. It publishes onto a `prism/coordination` subject, so the
toast appears on the frame after the one that called it.

## For coding assistants

Read the canonical guide before writing code against this module — the module
inventory with current tags, the application skeleton, MVU and rx semantics,
typography, and the pitfalls that are not guessable:

<https://raw.githubusercontent.com/vibrantgio/.github/master/llms.txt>

[`AGENTS.md`](./AGENTS.md) in this repository has the build, test and
golden-image commands. The golden line there is exact and both halves of it
matter — `-golden.update` must follow the package list, and the list cannot be
replaced by `./...`.

## Status

Honest about what does not work yet:

- **v0.3.0 is a breaking release.** Every static `Render` is re-cut off the
  `tokens.TypeScale` spectrum dropped in its own v0.3.0, onto a
  `tokens.TextStyle` or a `tokens.Typography` plus, where a control is
  sized, a `tokens.Density` — see "Two forms" above for which shape each
  pattern takes. Old call: `…, tokens.Spacing, tokens.DefaultTypeScale)`.
  New call: `…, tokens.Spacing, tokens.DefaultTypography.LabelLarge,
  tokens.Comfortable)`. The live entry points are unchanged.
- **`hero`'s outlined Secondary CTA was 8 dp too tall until v0.3.0.** It
  hardcoded 44 dp to line up with the filled Primary, which was
  prism/button's height until E1.3 re-cut that to the density's
  `ControlHeight` (36 dp Comfortable). Both CTAs now follow the density and
  line up again.
- **`table` has no per-header widget slot.** Headers are drawn internally from
  `Column.Header` strings, so anything else on a header — a tooltip, a filter
  affordance — has to be positioned by arithmetic over the column widths from
  outside. `workbench/watchlist` does this, and duplicates the table's private
  header height to do it. No phase of the current plan fixes it.
- **`shell`'s slots are inconsistent.** `Sidebar`, `Aside` and `Sections` are
  `rx.Observable[layout.Widget]`, but `Main`, `Left`, `Right` and `Footer` are
  plain widgets. A live main pane therefore has to be bridged into the static
  slot through a cell the consumer folds onto another stream — the idiom every
  workbench app repeats. Same for `navbar.Props.Actions`.
- **`pagination.Props.Page` and `PageCount` are plain ints**, not observables,
  so a page change means rebuilding the whole pattern through an
  `rx.SwitchMap`. `accordion`, `modal`, `popover`, `sidebar`, `tabs` and
  `table` all take their dynamic state as observables; pagination is the
  outlier.
- **Overlays open and close instantly.** `modal`, `popover` and `tooltip` have
  no entrance or exit transition; only `toast` animates, and only its fade-out
  (whose duration does at least resolve from the theme's motion scale now).
  Integrating pulse's motion primitives across the overlays is still deferred.
- **No responsive behaviour.** `feature`, `pricing` and `testimonial` do not
  collapse to fewer columns or a vertical stack on a narrow window, and
  `popover` does not flip or reflow when the chosen `Placement` would clip the
  viewport — it just clips. `pagination` renders every page in `[1, PageCount]`
  with no ellipsis collapse.

## License

MIT — see [LICENSE](./LICENSE).
