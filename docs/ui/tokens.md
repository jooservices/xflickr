# Design token source contract

web/src/shared/design/tokens.json is the single source of design token values
for USER and ADMIN themes. Application CSS and components consume values from
this JSON through derived CSS variables; they must not define a second palette.
This contract specifies the file for JOOXF-79 to implement. Approved values and
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
                "series": {"s1": "#1f66b4", "s2": "#b8481e", "s3": "#08774f"}
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
required values and behavior.

Use camelCase keys in JSON; map them to POC CSS names (accentInk to
--accent-ink, series.s1 to --s1, etc.). CSS variables are a derived runtime
representation, not a competing source. Select role and theme explicitly with
data-role and data-theme; when theme is unset, follow documented role defaults
and system-preference behavior.

## POC coverage check

ui-spec-check must compare the required token names in this contract with the
custom properties declared in every POC root/theme block. It fails if a token
is missing from the design-system tables or machine-readable contract, and
reports unclassified POC properties for addition or explicit classification as
layout/runtime-only. Coverage includes bg, surface, sunken, raise, line, line2,
ink, muted, faint, accent, accent-ink, accent-soft, ok, warn, bad, info, fav,
s1, s2, s3, shade, overlay, f-disp, f-body, f-mono, radius, btn-r, and row-h.
The dynamic --r property is classified as per-photo aspect-ratio runtime data,
not a design token. The check also confirms that the documented icon inventory
includes every entry in the POC ICONS object. The POC is read-only reference
input.
