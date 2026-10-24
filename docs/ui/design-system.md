# XFlickr design system

This document records the shared visual language for both roles. The approved
reference is the root block and role/theme overrides in
[the POC](poc/xflickr-poc.html). The POC is a visual baseline; the token
contract in [tokens.md](tokens.md) is the canonical implementation source.
Component behavior is specified in [components.md](components.md). View
behavior belongs in the view specifications.

## Roles and themes

| Role | Default theme | Optional theme | Shape and density |
| --- | --- | --- | --- |
| USER | Light gallery | Dark | Spacious, photo-led, 14px card radius, pill controls |
| ADMIN | Dark control room | Light | Dense panels, 9px card radius, compact controls |

Explicit theme selection takes precedence over the operating-system preference.
Without an explicit choice, USER follows the system preference and ADMIN
defaults to dark. Theme changes update color-scheme so browser controls match.
ADMIN typography and geometry remain role-specific in its light theme.

## Color tokens

Each theme table copies the POC. Required value is the value an implementation
must use. POC value is the original custom property from the POC root or role
override for that theme. A Required value marked (corrected) is an
accessibility change so normal text meets 4.5:1 and meaningful UI graphics,
including control boundaries and chart series, meet 3:1 against adjacent
surfaces. USER light and ADMIN light correct series colors separately, so
their required `s2` values differ. A viewport `@media` rule that changes a
root custom property is an extra row in every theme table. `--row-h` is
`190px` until `max-width: 720px`, where the POC sets it to `120px`
(`row-h@max-720`). The following tables list the measured
contrast of text and status/chart colors against each of the four surfaces in
that theme. Ratios use WCAG 2 relative luminance, are rounded to two decimals,
and the lowest listed value governs. `faint` is decorative only. Readable text
uses `ink` or `muted`. `fav` marks icons and non-text badges and must meet 3:1.
Counts and other text use a pair that meets 4.5:1. Status colors may be used as
text only where all four ratios are at least 4.5:1. Series colors are chart
graphics and must meet 3:1; provide labels or markers as well.

### USER light gallery

| Token | Required value | POC value |
| --- | --- | --- |
| bg | #f6f7fa | #f6f7fa |
| surface | #ffffff | #ffffff |
| sunken | #eef0f5 | #eef0f5 |
| raise | #e7eaf1 | #e7eaf1 |
| line | #e2e5ec | #e2e5ec |
| line2 | #747b88 (corrected) | #cfd4de |
| ink | #161a22 | #161a22 |
| muted | #5d6574 | #5d6574 |
| faint | #8a92a1 | #8a92a1 |
| accent | #2457d6 | #2457d6 |
| accent-ink | #ffffff | #ffffff |
| accent-soft | #e6edfd | #e6edfd |
| ok | #187244 (corrected) | #1d8a52 |
| warn | #875300 (corrected) | #a86500 |
| bad | #c12824 (corrected) | #c9302c |
| info | #5a52c9 | #5a52c9 |
| fav | #d93c74 | #d93c74 |
| s1 | #1f66b4 (corrected) | #2a78d6 |
| s2 | #b4431a (corrected) | #eb6834 |
| s3 | #08774f (corrected) | #1baf7a |
| shade | rgba(18,22,30,.10) | rgba(18,22,30,.10) |
| overlay | #0c0e12 | #0c0e12 |
| f-disp | "Bricolage Grotesque","Figtree",system-ui,sans-serif | "Bricolage Grotesque","Figtree",system-ui,sans-serif |
| f-body | "Figtree",system-ui,-apple-system,"Segoe UI",sans-serif | "Figtree",system-ui,-apple-system,"Segoe UI",sans-serif |
| f-mono | "JetBrains Mono",ui-monospace,Menlo,Consolas,monospace | "JetBrains Mono",ui-monospace,Menlo,Consolas,monospace |
| radius | 14px | 14px |
| btn-r | 999px | 999px |
| row-h | 190px | 190px |
| row-h@max-720 | 120px | 120px |

Text and graphic contrast ratios (foreground over bg, surface, sunken, raise):

| Foreground | bg | surface | sunken | raise |
| --- | ---: | ---: | ---: | ---: |
| ink | 16.27 | 17.43 | 15.28 | 14.47 |
| muted | 5.48 | 5.87 | 5.15 | 4.87 |
| accent | 5.75 | 6.16 | 5.40 | 5.11 |
| ok | 5.55 | 5.95 | 5.22 | 4.94 |
| warn | 6.00 | 6.43 | 5.64 | 5.34 |
| bad | 5.45 | 5.84 | 5.12 | 4.85 |
| info | 5.62 | 6.03 | 5.28 | 5.00 |
| fav (icon/badge only) | 4.04 | 4.33 | 3.80 | 3.60 |
| s1 (chart) | 5.42 | 5.80 | 5.09 | 4.82 |
| s2 (chart) | 5.22 | 5.59 | 4.90 | 4.64 |
| s3 (chart) | 5.21 | 5.58 | 4.90 | 4.64 |
| line2 (control boundary) | 3.98 | 4.26 | 3.73 | 3.54 |

Text on accent (`accent-ink` over `accent`): 6.16:1.

### USER dark gallery

| Token | Required value | POC value |
| --- | --- | --- |
| bg | #0f1116 | #0f1116 |
| surface | #171a21 | #171a21 |
| sunken | #1e222b | #1e222b |
| raise | #252a35 | #252a35 |
| line | #2a2f3a | #2a2f3a |
| line2 | #778396 (corrected) | #3a4150 |
| ink | #e8eaf0 | #e8eaf0 |
| muted | #a0a7b4 | #a0a7b4 |
| faint | #747c8a | #747c8a |
| accent | #7d9cff | #7d9cff |
| accent-ink | #0d1220 | #0d1220 |
| accent-soft | #1f2a47 | #1f2a47 |
| ok | #4fc887 | #4fc887 |
| warn | #f0b04a | #f0b04a |
| bad | #ff7b72 | #ff7b72 |
| info | #a59ff2 | #a59ff2 |
| fav | #ff6f9f | #ff6f9f |
| s1 | #3987e5 | #3987e5 |
| s2 | #d95926 | #d95926 |
| s3 | #199e70 | #199e70 |
| shade | rgba(0,0,0,.4) | rgba(0,0,0,.4) |
| overlay | #0c0e12 | #0c0e12 |
| f-disp | "Bricolage Grotesque","Figtree",system-ui,sans-serif | "Bricolage Grotesque","Figtree",system-ui,sans-serif |
| f-body | "Figtree",system-ui,-apple-system,"Segoe UI",sans-serif | "Figtree",system-ui,-apple-system,"Segoe UI",sans-serif |
| f-mono | "JetBrains Mono",ui-monospace,Menlo,Consolas,monospace | "JetBrains Mono",ui-monospace,Menlo,Consolas,monospace |
| radius | 14px | 14px |
| btn-r | 999px | 999px |
| row-h | 190px | 190px |
| row-h@max-720 | 120px | 120px |

Text and graphic contrast ratios (foreground over bg, surface, sunken, raise):

| Foreground | bg | surface | sunken | raise |
| --- | ---: | ---: | ---: | ---: |
| ink | 15.70 | 14.47 | 13.24 | 11.95 |
| muted | 7.81 | 7.20 | 6.58 | 5.94 |
| accent | 7.26 | 6.69 | 6.12 | 5.52 |
| ok | 8.94 | 8.24 | 7.54 | 6.81 |
| warn | 9.90 | 9.13 | 8.35 | 7.54 |
| bad | 7.49 | 6.90 | 6.31 | 5.70 |
| info | 7.95 | 7.33 | 6.70 | 6.05 |
| fav (icon/badge only) | 7.22 | 6.65 | 6.09 | 5.49 |
| s1 (chart) | 5.19 | 4.78 | 4.38 | 3.95 |
| s2 (chart) | 4.86 | 4.48 | 4.10 | 3.70 |
| s3 (chart) | 5.54 | 5.11 | 4.68 | 4.22 |
| line2 (control boundary) | 4.92 | 4.54 | 4.15 | 3.74 |

Text on accent (`accent-ink` over `accent`): 7.18:1.

### ADMIN dark control room

| Token | Required value | POC value |
| --- | --- | --- |
| bg | #0b0f15 | #0b0f15 |
| surface | #11171f | #11171f |
| sunken | #161e28 | #161e28 |
| raise | #1c2632 | #1c2632 |
| line | #222c3a | #222c3a |
| line2 | #7a8798 (corrected) | #2e3a4c |
| ink | #d6dee8 | #d6dee8 |
| muted | #8190a4 | #8190a4 |
| faint | #5d6b82 | #5d6b82 |
| accent | #4cb4d8 | #4cb4d8 |
| accent-ink | #04131a | #04131a |
| accent-soft | #12303d | #12303d |
| ok | #3cb87c | #3cb87c |
| warn | #e2ae3d | #e2ae3d |
| bad | #f2766b (corrected) | #ea5a4e |
| info | #9590ee | #9590ee |
| fav | #ee5d98 | #ee5d98 |
| s1 | #3987e5 | #3987e5 |
| s2 | #d95926 | #d95926 |
| s3 | #199e70 | #199e70 |
| shade | rgba(0,0,0,.45) | rgba(0,0,0,.45) |
| overlay | #0c0e12 | #0c0e12 |
| f-disp | "Barlow Semi Condensed","Arial Narrow",system-ui,sans-serif | "Barlow Semi Condensed","Arial Narrow",system-ui,sans-serif |
| f-body | "Barlow",system-ui,-apple-system,"Segoe UI",sans-serif | "Barlow",system-ui,-apple-system,"Segoe UI",sans-serif |
| f-mono | "JetBrains Mono",ui-monospace,Menlo,Consolas,monospace | "JetBrains Mono",ui-monospace,Menlo,Consolas,monospace |
| radius | 9px | 9px |
| btn-r | 7px | 7px |
| row-h | 190px | 190px |
| row-h@max-720 | 120px | 120px |

Text and graphic contrast ratios (foreground over bg, surface, sunken, raise):

| Foreground | bg | surface | sunken | raise |
| --- | ---: | ---: | ---: | ---: |
| ink | 14.15 | 13.27 | 12.37 | 11.27 |
| muted | 5.91 | 5.54 | 5.17 | 4.71 |
| accent | 8.07 | 7.57 | 7.06 | 6.43 |
| ok | 7.63 | 7.15 | 6.67 | 6.08 |
| warn | 9.47 | 8.88 | 8.28 | 7.54 |
| bad | 6.93 | 6.50 | 6.06 | 5.52 |
| info | 6.86 | 6.43 | 6.00 | 5.47 |
| fav (icon/badge only) | 6.09 | 5.71 | 5.32 | 4.85 |
| s1 (chart) | 5.28 | 4.95 | 4.62 | 4.20 |
| s2 (chart) | 4.95 | 4.64 | 4.32 | 3.94 |
| s3 (chart) | 5.64 | 5.29 | 4.93 | 4.49 |
| line2 (control boundary) | 5.26 | 4.93 | 4.60 | 4.19 |

Text on accent (`accent-ink` over `accent`): 7.93:1.

### ADMIN light control room

| Token | Required value | POC value |
| --- | --- | --- |
| bg | #eef1f5 | #eef1f5 |
| surface | #ffffff | #ffffff |
| sunken | #f5f7fa | #f5f7fa |
| raise | #e9edf3 | #e9edf3 |
| line | #dce2ea | #dce2ea |
| line2 | #737f90 (corrected) | #c9d2de |
| ink | #16202c | #16202c |
| muted | #56657a | #56657a |
| faint | #7d8a9c | #7d8a9c |
| accent | #006482 (corrected) | #0c7da3 |
| accent-ink | #ffffff | #ffffff |
| accent-soft | #dcf0f7 | #dcf0f7 |
| ok | #176d42 (corrected) | #1c8752 |
| warn | #815800 (corrected) | #9c6a00 |
| bad | #c5372c | #c5372c |
| info | #5a52c9 | #5a52c9 |
| fav | #c7366f | #c7366f |
| s1 | #1f66b4 (corrected) | #2a78d6 |
| s2 | #b8481e (corrected) | #eb6834 |
| s3 | #08774f (corrected) | #1baf7a |
| shade | rgba(20,32,48,.14) | rgba(20,32,48,.14) |
| overlay | #0c0e12 | #0c0e12 |
| f-disp | "Barlow Semi Condensed","Arial Narrow",system-ui,sans-serif | "Barlow Semi Condensed","Arial Narrow",system-ui,sans-serif |
| f-body | "Barlow",system-ui,-apple-system,"Segoe UI",sans-serif | "Barlow",system-ui,-apple-system,"Segoe UI",sans-serif |
| f-mono | "JetBrains Mono",ui-monospace,Menlo,Consolas,monospace | "JetBrains Mono",ui-monospace,Menlo,Consolas,monospace |
| radius | 9px | 9px |
| btn-r | 7px | 7px |
| row-h | 190px | 190px |
| row-h@max-720 | 120px | 120px |

Text and graphic contrast ratios (foreground over bg, surface, sunken, raise):

| Foreground | bg | surface | sunken | raise |
| --- | ---: | ---: | ---: | ---: |
| ink | 14.51 | 16.44 | 15.32 | 13.99 |
| muted | 5.24 | 5.94 | 5.53 | 5.05 |
| accent | 5.89 | 6.68 | 6.22 | 5.68 |
| ok | 5.61 | 6.36 | 5.92 | 5.41 |
| warn | 5.57 | 6.31 | 5.88 | 5.37 |
| bad | 4.68 | 5.31 | 4.95 | 4.52 |
| info | 5.32 | 6.03 | 5.61 | 5.13 |
| fav (icon/badge only) | 4.43 | 5.02 | 4.67 | 4.27 |
| s1 (chart) | 5.12 | 5.80 | 5.41 | 4.94 |
| s2 (chart) | 4.65 | 5.27 | 4.91 | 4.49 |
| s3 (chart) | 4.93 | 5.58 | 5.20 | 4.75 |
| line2 (control boundary) | 3.59 | 4.06 | 3.79 | 3.46 |

Text on accent (`accent-ink` over `accent`): 6.68:1.

Pair status colors with text, icons, labels, or chart markers; color alone never
communicates state. Line is a low-contrast decorative divider. Interactive
boundaries, focus indicators, and meaningful chart graphics need at least 3:1
contrast against adjacent surfaces. Check text against its actual surface
(surface, sunken, or raise), not only bg.

## Typography

| Role | Display/headings | Body | IDs and machine values |
| --- | --- | --- | --- |
| USER | Bricolage Grotesque; Figtree, system UI, sans-serif fallbacks | Figtree; system UI, -apple-system, Segoe UI, sans-serif | JetBrains Mono; ui-monospace, Menlo, Consolas, monospace |
| ADMIN | Barlow Semi Condensed; Arial Narrow, system UI, sans-serif | Barlow; system UI, -apple-system, Segoe UI, sans-serif | JetBrains Mono; ui-monospace, Menlo, Consolas, monospace |

Load weights: Bricolage 500/700, Figtree 400/500/600/700, Barlow 400/500/600,
Barlow Semi Condensed 500/600/700, JetBrains Mono 400/500. Body rendering uses
antialiased font smoothing (`typography.smoothing`: `antialiased`,
`-webkit-font-smoothing: antialiased`). The root
font size stays the browser default of 16px, so `rem` resolves against 16px.
`em` resolves against the parent font size. IDs and machine values use the
mono family. Never shrink body text to fit a translation.

### Type scale

`typeScale` is shared. The key is the JSON path under `typeScale`. A weight or
tracking cell with two values is USER, then ADMIN. A dash means that property
stays inherited and is omitted from JSON. Line heights are unitless. Family is
`display`, `body`, or `mono` (the role stack in the table above).
`heading.user.wrap` and `heading.admin.wrap` are `balance` and apply to h1, h2,
h3, h4, hero, and panelTitle.

| Key | Size | Line height | Weight | Tracking | Family | Extra |
| --- | --- | --- | --- | --- | --- | --- |
| heading.user | — | — | 700 | -0.01em | display | text-wrap: balance. POC tracking `-.01em` |
| heading.admin | — | — | 600 | 0.01em | display | text-wrap: balance. POC tracking `.01em` |
| body.user | 15px | 1.5 | 400 | normal | body | — |
| body.admin | 14px | 1.45 | 400 | normal | body | — |
| h1.user | clamp(24px, 3vw, 32px) | 1.15 | 700 | -0.01em | display | POC `clamp(24px,3vw,32px)` |
| h1.admin | 1.55rem | 1.15 | 600 | 0.01em | display | 24.8px at the 16px root |
| h2 | 19px | 1.25 | 700 / 600 | -0.01em / 0.01em | display | — |
| h3 | 15.5px | 1.3 | 700 / 600 | -0.01em / 0.01em | display | — |
| h4 | 1em | — | 700 / 600 | -0.01em / 0.01em | display | 1em equals the role body size |
| hero | clamp(28px, 4vw, 44px) | 1.15 | 700 / 600 | -0.01em / 0.01em | display | POC `clamp(28px,4vw,44px)` |
| panelTitle | 0.86rem | 1.25 | 600 | 0.08em | display | uppercase; color muted; 13.76px at the 16px root |
| small | 0.86em | — | — | — | — | 0.86em of the parent font size |
| helper | 12px | — | — | — | — | color muted |
| eyebrow | 0.74rem | — | 600 | 0.08em | display | uppercase; color muted; 11.84px at the 16px root |
| mono | 0.85em | — | — | — | mono | Loaded weights are 400 and 500 |
| numeric | — | — | — | — | — | font-variant-numeric: tabular-nums |

## Spacing, shape, elevation, and motion

`spacing` is 4px, 6px, 8px, 10px, 12px, 14px, 16px, 18px, 20px, 24px, 32px,
40px. Preserve existing shell gaps and padding specified in the POC. Each theme
stores `radius` and `btnRadius` from that theme's `radius` and `btn-r` rows.
`rowHeight` keeps every root `--row-h` value:

    "rowHeight": {"default": "190px", "max-720": "120px"}

`default` is the `row-h` row (`190px`). `max-720` is the `row-h@max-720` row
(`120px`), copied from `@media (max-width: 720px) { :root { --row-h: 120px } }`.
The breakpoint applies to every theme. A grid that sets `--row-h` on itself
(hero art 120px, onboarding art 110px) is local layout, not this token. USER
cards use 14px radius and pill buttons (999px); ADMIN cards use 9px and compact
7px buttons. Inputs, badges, and small labels may use local 4–8px radii as in
the POC.

### Elevation

Each key is the JSON path under `elevation`. The value is the CSS `box-shadow`.
`var(--shade)` is the active theme's `shade` color. Resting levels are the
small control shadow and the stacked-media shadow. Stronger levels are hover,
overlays, and the lightbox. Borders stay on elevated surfaces. Focus, selection,
and hierarchy keep their borders. Focus uses a 2px accent outline with a 2px
offset (`focus.width` 2px, `focus.offset` 2px, `focus.color` accent).

| Key | Shadow | Use |
| --- | --- | --- |
| resting | 0 1px 3px rgba(0,0,0,.25) | Switch thumb at rest. Fixed color |
| raised | 0 2px 8px var(--shade) | Album stack at rest |
| hover | 0 6px 20px var(--shade) | Dense card hover and prototype chrome |
| popover | 0 8px 24px var(--shade) | Tooltip |
| overlay | 0 10px 30px var(--shade) | Stronger card hover and toast |
| modal | 0 20px 50px var(--shade) | Dialog |
| hero | 0 20px 60px rgba(0,0,0,.5) | Lightbox photo. Fixed color |
| drawer | 0 0 40px var(--shade) | Drawer and off-canvas rail |
| sheet | 0 30px 60px var(--shade) | Onboarding panel |

Halo shadows sit outside this scale: search focus `0 0 0 4px var(--accent-soft)`,
input focus `0 0 0 3px var(--accent-soft)`, chart point `0 0 0 2px var(--surface)`,
and the live-status glow `0 0 0 3px color-mix(in srgb, var(--ok) 25%, transparent)`.

### Motion

Each key is the JSON path under `motion`. Duration and easing are the required
token strings. Where the POC sets a duration and omits a timing function, easing
is `ease` (the CSS initial value). `fresh` and `spin` set easing in the POC.
Store iteration when it is `infinite`. `motion.reducedMotion` is `none`.

| Key | Duration | Easing | Iteration | Parameters |
| --- | --- | --- | --- | --- |
| feedback | .15s | ease | 1 | Feedback for the switch background and thumb transform. Standard feedback duration |
| stateChange | .2s | ease | 1 | State change for caption opacity, tray opacity, and off-canvas rail transform |
| emphasis | .35s | ease | 1 | State change for the photo hover transform |
| entrance | .2s | ease | 1 | Entrance for the toast. offset 8px, fromOpacity 0 (`translateY(8px)`) |
| fresh | 1.6s | ease-out | 1 | Entrance for a new row. from `var(--accent-soft)` to the resting background |
| loading | 1.2s | ease | infinite | Loading skeleton shimmer. to background-position -200% |
| spin | .8s | linear | infinite | Loading spinner. to `rotate(1turn)` |
| progress | .6s | ease | 1 | Progress bar width and ring `stroke-dasharray` |

Under `prefers-reduced-motion: reduce`, transitions and animations are `none`,
including loading and progress. The end state stays visible. Feedback uses color,
text, or icons.

## Iconography

Use the POC inline SVG line set: 24×24 viewBox, currentColor, no fill, 1.8px
stroke, round caps and joins. Keep a 24px drawing grid; rendered size may be
14–30px in compact controls or empty-state artwork. Icons are decorative when
adjacent text names the action; otherwise give the control an accessible name.
Filled variants are limited to stateful glyphs such as starFill.

| Icon name | Usage |
| --- | --- |
| search | Search field/action |
| bell | Notifications |
| sun, moon | Theme control |
| star, starFill | Favorite state |
| download, upload, cloud | Download, upload, cloud destination |
| key, server | API keys, server/node resources |
| left, right, x | Back/forward navigation, close/dismiss |
| check, plus | Completion/selection, add/create |
| play, pause, refresh | Start, pause, retry/reload |
| alert, info | Warning/error, informational messages |
| heart | Favorite/like where the domain calls for it |
| share, link, copy | Sharing, opening a link, copying a value |
| sparkle | AI-generated caption or assistive feature |
| lock, shield | Privacy/security, admin role |
| user, users | Individual and group/contact records |
| gear, sliders | Settings, filters/configuration |
| logout | Sign out |
| album, image, grid, tray | Albums, photos, gallery layout, download tray |
| activity, chart, list, flag, cpu | Activity, metrics, records, issues, resources |
| tree | Discovery hierarchy |
| home, menu, globe, eye | Home, navigation, locale/public scope, visibility |

Do not substitute emoji or mix another icon family into this set. A missing
concept uses the closest clear existing glyph until a component spec defines an
addition.

## Photos, avatars, and localization

Photo placeholders preserve the requested aspect ratio and crop box. Use a
neutral sunken base with simple low-detail image treatment; never show a broken
image glyph or imply that the placeholder is a real photo. Decorative artwork
has empty alt text; meaningful photos have concise descriptive alt text. Keep
captions legible against photos using a scrim or solid surface.

Avatars are circular and use a Flickr profile image when available. Otherwise
show a deterministic initials/letter fallback on a solid color. Center bold
white fallback text with sufficient contrast. An interactive avatar needs an
accessible name beyond its visual fallback.

English and Vietnamese labels may expand by at least 30%. Let buttons and
navigation grow or wrap, let table cells reflow at narrow widths, and prevent
fixed-height text containers from clipping. Headings may wrap; do not truncate
labels needed to identify an action or status. Use min-width: 0 and
content-based sizing in flex/grid layouts, and check Vietnamese diacritics with
the selected font fallbacks.
