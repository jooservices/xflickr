# Design token source contract

web/src/shared/design/tokens.json is the single source of design token values
for USER and ADMIN themes. Application CSS and components consume values from
this JSON through derived CSS variables; they must not define a second palette.
This contract specifies the file for T01.3.3 to implement. Approved values and
accessibility constraints are in [design-system.md](design-system.md).

## Shape

The JSON is UTF-8, valid JSON without comments, and versioned. Group values by
role and theme. Theme names are light and dark; defaultTheme is light for USER
and dark for ADMIN. Store colors as CSS-compatible hex or rgba strings,
dimensions with units, and font stacks as ordered CSS family strings. Every
semantic token appears in each role/theme, even when values are shared.

    {
      "version": 1,
      "roles": {
        "user": {
          "defaultTheme": "light",
          "themes": {
            "light": {
              "color": {
                "bg": "#f6f7fa",
                "surface": "#ffffff",
                "ink": "#161a22",
                "accent": "#2457d6",
                "series": {"s1": "#1f66b4", "s2": "#b4431a", "s3": "#08774f"}
              }
            }
          }
        }
      }
    }

This is an abbreviated shape example, not the finished token file. The complete
schema includes every token, all four role/theme combinations, the typeScale,
elevation, and motion values below.

## Required keys

Each theme color object includes bg, surface, sunken, raise, line, line2, ink,
muted, faint, accent, accentInk, accentSoft, ok, warn, bad, info, fav,
series.s1, series.s2, series.s3, shade, and overlay. Each role includes
typography.display, typography.body, typography.mono, typography.weights, and
typography.smoothing (`antialiased`). Weights are the loaded face list in the
Typography section. Display, body, and mono are the font stacks in the role
table. A value marked (corrected) replaces the POC value in this file.

`typeScale` is shared. Keys and values are the Type scale table in
design-system.md. Store a dash as an omitted field. Store a unitless line
height as a JSON number. Store size, tracking, family, and extra CSS keywords
as strings.

- typeScale.heading.user and typeScale.heading.admin: weight, tracking, family, wrap (`balance`)
- typeScale.body.user and typeScale.body.admin: size, lineHeight, weight, tracking, family
- typeScale.h1.user and typeScale.h1.admin: size, lineHeight, weight, tracking, family
- typeScale.h2, typeScale.h3, typeScale.h4, and typeScale.hero: size, lineHeight, weight, tracking, family
- typeScale.panelTitle: size, lineHeight, weight, tracking, family, transform (`uppercase`), color (`muted`)
- typeScale.small: size (`0.86em`)
- typeScale.helper: size (`12px`), color (`muted`)
- typeScale.eyebrow: size, weight, tracking, family, transform (`uppercase`), color (`muted`)
- typeScale.mono: size, family
- typeScale.numeric.variant: `tabular-nums`

`h1.user.size` is `clamp(24px, 3vw, 32px)`. `h1.admin.size` is `1.55rem`.
`helper.size` is `12px`. Weight and tracking pairs in that table are USER, then
ADMIN (`700` / `600`, `-0.01em` / `0.01em`).

`spacing` is the spacing list in the Spacing section. Each theme stores
`radius` and `btnRadius` from its `radius` and `btn-r` rows. `rowHeight` is an
object, not one length:

    rowHeight: {default: "190px", "max-720": "120px"}

In the JSON file the keys are quoted:

    "rowHeight": {"default": "190px", "max-720": "120px"}

`default` is that theme's `row-h` row (`190px`). Each `row-h@<key>` row in the
theme table adds a key. `max-720` applies at `max-width: 720px`, where the POC
sets `:root { --row-h: 120px }` for every theme. Do not store a single
`rowHeight` string; a phone layout needs the `120px` value. `focus.width` is
`2px`, `focus.offset` is `2px`, and `focus.color` is `accent`.

`elevation` keys are the Elevation table: elevation.resting, elevation.raised,
elevation.hover, elevation.popover, elevation.overlay, elevation.modal,
elevation.hero, elevation.drawer, and elevation.sheet. Each value is that row's
shadow string. `var(--shade)` means the active theme's `color.shade`.

`motion` keys are the Motion table. Each of feedback, stateChange, emphasis,
entrance, fresh, loading, spin, and progress stores `duration` and `easing`
from that row.

- motion.feedback is the standard feedback duration: duration `.15s`, easing `ease`
- motion.stateChange: `.2s`, `ease`
- motion.emphasis: `.35s`, `ease`
- motion.entrance: `.2s`, `ease`, offset `8px`, fromOpacity `0`
- motion.fresh: `1.6s`, `ease-out`, from `var(--accent-soft)`
- motion.loading: `1.2s`, `ease`, iteration `infinite`, to `-200%`
- motion.spin: `.8s`, `linear`, iteration `infinite`, to `1turn`
- motion.progress: `.6s`, `ease`
- motion.reducedMotion: `none`

Store each Required value from the Type scale, Elevation, and Motion tables in
design-system.md.

Use camelCase keys in JSON; map them to POC CSS names (accentInk to
--accent-ink, series.s1 to --s1, etc.). CSS variables are a derived runtime
representation, not a competing source. Select role and theme explicitly with
data-role and data-theme; when theme is unset, follow documented role defaults
and system-preference behavior.

## POC coverage check

`make ui-spec-check` compares the custom property names in the POC root/theme
blocks with the token tables for all four themes in design-system.md. It fails
when a POC property is absent from any theme table, when a `@media` rule sets
a `:root` custom property that the tables do not record, and when the icon
inventory misses a POC `ICONS` entry. A single `max-width` or `min-width`
override needs a `<token>@max-<px>` or `<token>@min-<px>` row in every theme,
with both value cells equal to the override (`row-h@max-720` is `120px`). A
single `prefers-color-scheme` override must match the POC value cell of the
theme that query selects. Any other `:root` override inside `@media` needs a
`<token>@<query-slug>` row in every theme; the slug is the query in lowercase
with non-alphanumeric runs replaced by hyphens. The target also runs
`node --test "scripts/**/*.test.mjs"`, including a check that the real
design-system document reports no missing tokens, media overrides, or icons
and that the type scale, elevation, and motion tables keep the required keys.
`make check` runs this target. The same target reads
[components.md](components.md) and [the component gallery](mockups/components.html).
Every shared slice under `web/src/shared/ui` needs a state table for default,
hover, focus, active, disabled, loading, and error, and the gallery shows each
of those states in USER and ADMIN light and dark frames. The target also
lints components.md with markdownlint. The dynamic --r
property is excluded because it
is per-photo aspect-ratio runtime data, not a design token. Custom properties
set on a component, including a grid's own `--row-h`, are not root tokens. The
POC is read-only reference input.
