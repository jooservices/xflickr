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
`radius`, `btnRadius`, and `rowHeight` from its `radius`, `btn-r`, and `row-h`
rows. `focus.width` is `2px`, `focus.offset` is `2px`, and `focus.color` is
`accent`.

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
when a POC property is absent from any theme table and confirms that the icon
inventory documents every entry in the POC ICONS object. The target also runs
`node --test "scripts/**/*.test.mjs"`, including a check that the real
design-system document reports no missing tokens or icons and that the type
scale, elevation, and motion tables keep the required keys. `make check` runs
this target. The dynamic --r property is excluded because it is per-photo
aspect-ratio runtime data, not a design token. The POC is read-only reference
input.
