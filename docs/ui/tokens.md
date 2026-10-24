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
schema includes every token, all four role/theme combinations, typography,
dimensions, and motion values.

## Required keys

Each theme color object includes bg, surface, sunken, raise, line, line2, ink,
muted, faint, accent, accentInk, accentSoft, ok, warn, bad, info, fav,
series.s1, series.s2, series.s3, shade, and overlay. Each role includes
typography.display, typography.body, typography.mono, typography.weights, and
the shared typeScale. Shared dimensions include the spacing scale, role card
and button radii, rowHeight, and elevation values. Motion includes standard
feedback duration and reduced-motion behavior (none). See design-system.md for
required values and behavior. Store each Required value from design-system.md.
A value marked (corrected) replaces the POC value in this file.

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
design-system document reports no missing tokens or icons. `make check` runs
this target. The dynamic --r property is excluded because it is per-photo
aspect-ratio runtime data, not a design token. The POC is read-only reference
input.
