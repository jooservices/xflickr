# XFlickr design system

This document records the shared visual language for both roles. The approved
reference is the root block and role/theme overrides in
[the POC](poc/xflickr-poc.html). The POC is a visual baseline; the token
contract in [tokens.md](tokens.md) is the canonical implementation source.
Component and view behavior belongs in their respective specifications.

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

Hex values below come from the POC. Some light-theme POC colors fail the
contrast requirements in this specification. In those cells the accessible
value is the implementation value, a deliberate correction to the reference.
Measure foreground contrast against its actual background. Normal text needs
4.5:1; large text and meaningful non-text UI graphics need 3:1. Do not use faint
for information-bearing text.

### USER light gallery

| Token | POC | Required value | Meaning |
| --- | --- | --- | --- |
| bg | #f6f7fa | #f6f7fa | Page canvas |
| surface | #ffffff | #ffffff | Cards, menus, panels |
| sunken | #eef0f5 | #eef0f5 | Inputs and recessed areas |
| raise | #e7eaf1 | #e7eaf1 | Raised/hover surface |
| line | #e2e5ec | #e2e5ec | Decorative separators only |
| line2 | #cfd4de | #cfd4de | Stronger boundaries |
| ink | #161a22 | #161a22 | Primary text |
| muted | #5d6574 | #5d6574 | Secondary text (5.48:1 on bg) |
| faint | #8a92a1 | #8a92a1 | Decorative, text-free detail only |
| accent | #2457d6 | #2457d6 | Links, focus, primary action (5.75:1 on bg) |
| accent-ink | #ffffff | #ffffff | Text on accent (6.16:1) |
| accent-soft | #e6edfd | #e6edfd | Accent tint/background |
| ok | #1d8a52 | #187244 | Success state |
| warn | #a86500 | #875300 | Warning state |
| bad | #c9302c | #c9302c | Error state |
| info | #5a52c9 | #5a52c9 | Informational state |
| fav | #d93c74 | #d93c74 | Favorite and notification badge |
| s1 | #2a78d6 | #1f66b4 | First chart series |
| s2 | #eb6834 | #b8481e | Second chart series |
| s3 | #1baf7a | #08774f | Third chart series |
| shade | rgba(18,22,30,.10) | same | Surface shadow |
| overlay | #0c0e12 | same | Modal/lightbox backdrop |

### USER dark gallery

| Token | Value | Token | Value |
| --- | --- | --- | --- |
| bg | #0f1116 | surface | #171a21 |
| sunken | #1e222b | raise | #252a35 |
| line | #2a2f3a | line2 | #3a4150 |
| ink | #e8eaf0 | muted | #a0a7b4 |
| faint | #747c8a | accent | #7d9cff |
| accent-ink | #0d1220 | accent-soft | #1f2a47 |
| ok | #4fc887 | warn | #f0b04a |
| bad | #ff7b72 | info | #a59ff2 |
| fav | #ff6f9f | s1 | #3987e5 |
| s2 | #d95926 | s3 | #199e70 |
| shade | rgba(0,0,0,.4) | overlay | #0c0e12 |

### ADMIN dark control room

| Token | Value | Token | Value |
| --- | --- | --- | --- |
| bg | #0b0f15 | surface | #11171f |
| sunken | #161e28 | raise | #1c2632 |
| line | #222c3a | line2 | #2e3a4c |
| ink | #d6dee8 | muted | #8190a4 |
| faint | #5d6b82 | accent | #4cb4d8 |
| accent-ink | #04131a | accent-soft | #12303d |
| ok | #3cb87c | warn | #e2ae3d |
| bad | #ea5a4e | info | #9590ee |
| fav | #ee5d98 | s1 | #3987e5 |
| s2 | #d95926 | s3 | #199e70 |
| shade | rgba(0,0,0,.45) | overlay | #0c0e12 |

### ADMIN light control room

| Token | POC | Required value | Meaning |
| --- | --- | --- | --- |
| bg | #eef1f5 | #eef1f5 | Page canvas |
| surface | #ffffff | #ffffff | Cards and panels |
| sunken | #f5f7fa | #f5f7fa | Inputs and recessed areas |
| raise | #e9edf3 | #e9edf3 | Raised/hover surface |
| line | #dce2ea | #dce2ea | Decorative separators only |
| line2 | #c9d2de | #c9d2de | Stronger boundaries |
| ink | #16202c | #16202c | Primary text |
| muted | #56657a | #56657a | Secondary text (5.24:1 on bg) |
| faint | #7d8a9c | #7d8a9c | Decorative, text-free detail only |
| accent | #0c7da3 | #006482 | Links, focus, primary action |
| accent-ink | #ffffff | #ffffff | Text on accent |
| accent-soft | #dcf0f7 | #dcf0f7 | Accent tint/background |
| ok | #1c8752 | #176d42 | Success state |
| warn | #9c6a00 | #815800 | Warning state |
| bad | #c5372c | #c5372c | Error state |
| info | #5a52c9 | #5a52c9 | Informational state |
| fav | #c7366f | #c7366f | Favorite and notification badge |
| s1 | #2a78d6 | #1f66b4 | First chart series |
| s2 | #eb6834 | #b8481e | Second chart series |
| s3 | #1baf7a | #08774f | Third chart series |
| shade | rgba(20,32,48,.14) | same | Surface shadow |
| overlay | #0c0e12 | same | Modal/lightbox backdrop |

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
Barlow Semi Condensed 500/600/700, JetBrains Mono 400/500. USER body is 15px at
1.5 line height; ADMIN body is 14px at 1.45. H1 is responsive 24–32px (ADMIN
1.55rem), H2 19px, H3 15.5px. Small helper text is about 0.86em. Eyebrows are
0.74rem, 600 weight, uppercase, with 0.08em tracking. IDs and machine values use
monospace; numeric data uses tabular figures. Never shrink body text to fit a
translation.

## Spacing, shape, elevation, and motion

Use a 4px rhythm: 4, 6, 8, 10, 12, 14, 16, 18, 20, 24, 32, 40px. Preserve
existing shell gaps/padding specified in the POC. USER cards use 14px radius and
pill buttons (999px); ADMIN cards use 9px and compact 7px buttons. Inputs,
badges, and small labels may use local 4–8px radii as in the POC.

Use low-contrast shade shadows for resting surfaces and stronger shadows only
for hover or overlays. Do not use elevation alone to show focus, selection, or
hierarchy; retain borders and focus rings. Focus uses a 2px accent outline with
2px offset. Motion is brief and limited to feedback or state changes. Under
prefers-reduced-motion: reduce, remove transitions and animations, including
loading motion; preserve the state and provide non-motion feedback.

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
