# Component library

Views are assembled from these shared slices. Each heading is the feature-sliced
directory under web/src/shared/ui. The React export is the PascalCase name in
that section. Tokens, type, icons, and motion come from the design system. The
gallery shows every slice in every state for USER light, USER dark, ADMIN dark,
and ADMIN light.

Hover, focus, and active in the gallery are forced so they stay visible without
a pointer. Production uses the real pointer, :focus-visible, and pressed styles
described here. Active means pressed, checked, selected, or current, whichever
that slice defines.

## Catalogue

| Slice | Export | Covers |
| --- | --- | --- |
| button | Button | neutral, primary, danger, ghost, sm |
| chip | Chip | Filter and count chips |
| pill | Pill | Status words and tones |
| badge | Badge | Mono ids and counts |
| tag | Tag | Photo tags that can start a search |
| text-field | TextField | Text, password, and mono inputs |
| select | Select | Native single select |
| switch | Switch | Immediate on/off |
| checkbox | Checkbox | Form and filter checks |
| table | Table | Caption, columns, and rows |
| card | Card | Static and interactive surfaces |
| panel | Panel | Titled header and body |
| stat-strip | StatStrip | A row of figures |
| banner | Banner | Info, warn, and bad messages |
| progress-bar | ProgressBar | Determinate meter |
| ring | Ring | Circular percentage |
| sparkline | Sparkline | Inline trend |
| chart | Chart | Line, area, and bars |
| photo-grid | PhotoGrid | Justified photo rows |
| contact-card | ContactCard | Mosaic, avatar, and star |
| album-card | AlbumCard | Stacked album cover |
| lightbox | Lightbox | Photo dialog |
| modal | Modal | Centered task dialog |
| drawer | Drawer | Side task or list dialog |
| toast | Toast | Transient confirmation |
| tabs | Tabs | Page tabs and subtabs |
| breadcrumb | Breadcrumb | Parent trail |
| empty-block | EmptyBlock | No-rows placeholder |
| skeleton | Skeleton | Loading placeholder |
| node-card | NodeCard | Cluster node summary |
| resource-bar | ResourceBar | CPU, RAM, disk, and network |
| tree | Tree | Disclosure tree |
| event-feed | EventFeed | Live operational events |

## button

A command control. Export `Button` from the slice. One primary button is the
main action in a region. Labels hug their text, including Vietnamese strings
that run about 30% longer, and never truncate an action name.

Slice: `web/src/shared/ui/button`.

Gallery: [USER light](mockups/components.html#user-light-button).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| children | ReactNode | — | Visible label. Required unless aria-label is set |
| variant | 'neutral' or 'primary' or 'danger' or 'ghost' | neutral | Emphasis. danger is destructive and still needs the view to confirm |
| size | 'md' or 'sm' | md | sm is the compact toolbar and table size |
| type | 'button' or 'submit' or 'reset' | button | Native button type |
| icon | IconName | — | Optional leading icon from the design-system set |
| disabled | boolean | false | Blocks activation and removes the button from the tab order |
| loading | boolean | false | Shows a spinner, keeps the label, and blocks activation |
| onClick | () => void | — | Activated by pointer, Enter, or Space |

### Variants

| Variant | Meaning |
| --- | --- |
| neutral | Bordered secondary action on surface. ADMIN uses a sunken fill |
| primary | The single main action. Accent fill and accent-ink text |
| danger | Destructive action. Bad text and a bad-tinted border. Primary is not combined with danger |
| ghost | Low emphasis. Transparent until hover, then sunken |
| sm | Compact padding (4px 10px) and 12.5px type. Combines with the other variants |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Surface or sunken fill, line2 border, ink label, weight 600 (500 in ADMIN). Radius is btn-r | Ready for pointer and keyboard |
| hover | Neutral border turns faint. Primary brightens by 1.07. Ghost fills sunken and turns ink. Danger keeps its bad border | Pointer cursor. The color change is instant under reduced motion |
| focus | 2px accent outline, 2px offset, via :focus-visible | Tab reaches it. A pointer click does not leave the ring stuck |
| active | Brightness 0.94 and 1px down while Enter, Space, or the pointer is held | The action runs on keyup or pointerup inside the control |
| disabled | Opacity 0.45 and a not-allowed cursor. Native disabled | Skipped in the tab order |
| loading | Spinner before the same label, aria-busy, aria-disabled. The spinner does not rotate under reduced motion | Does not activate. The name is still announced |
| error | Alert icon plus a bad border. A primary button uses a bad fill and white text | Stays activatable so the person can retry |

### Keyboard

- Tab and Shift+Tab move focus in page order.
- Enter and Space activate the button on keyup.
- Disabled buttons are not focused. A loading button stays in the accessibility tree with aria-disabled and does not activate.

### ARIA

- Native `button`. Icon-only buttons set aria-label.
- Loading sets aria-busy="true" and aria-disabled="true".
- The error icon is aria-hidden because the visible label still names the action.

## chip

A compact toggle or filter. Export `Chip`. Pressed chips are the selected
filters. Page tabs and subtabs reuse this pressed treatment only through the
tabs slice.

Slice: `web/src/shared/ui/chip`.

Gallery: [USER light](mockups/components.html#user-light-chip).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| children | ReactNode | — | Label |
| pressed | boolean | false | Selected filter or toggle |
| icon | IconName | — | Optional leading icon |
| count | number | — | Optional trailing count in tabular figures |
| disabled | boolean | false | Blocks toggling |
| loading | boolean | false | Replaces the count with a spinner |

### Variants

| Variant | Meaning |
| --- | --- |
| filter | Single or multi select. aria-pressed communicates the choice |
| with-count | Label plus a tabular count, used for facet filters |
| with-icon | Leading design-system icon plus label |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Surface fill, line border, btn-r radius. ADMIN uses a 6px radius and a sunken fill | Toggle or invoke on click |
| hover | Border turns faint and the label turns ink | Pointer cursor |
| focus | 2px accent outline, 2px offset | Tab reaches each chip |
| active | aria-pressed true. USER inverts to ink on surface. ADMIN uses accent-soft and an accent border | Announced as pressed |
| disabled | Opacity 0.45, not toggled | Removed from the tab order |
| loading | Spinner replaces the trailing count and aria-busy is set | Does not toggle |
| error | Bad border and an alert icon before the label | Stays activatable |

### Keyboard

- Tab moves between chips. Arrow keys do not move inside a loose chip group; a tabs slice owns arrow movement.
- Enter and Space toggle aria-pressed.

### ARIA

- Button with aria-pressed="true" or "false".
- A count is part of the accessible name, for example "Trees, 12".
- Loading sets aria-busy="true".

## pill

A read-only status marker with a dot and a word. Export `Pill`. Color is never
the only signal: the word and the dot shape travel together. Do not put a status
color on body text unless every surface in the theme clears 4.5:1.

Slice: `web/src/shared/ui/pill`.

Gallery: [USER light](mockups/components.html#user-light-pill).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| children | ReactNode | — | Status word, such as Healthy or Cap reached |
| tone | 'neutral' or 'ok' or 'warn' or 'bad' or 'info' or 'vio' | neutral | Semantic color. vio uses the info token |
| icon | IconName | — | Optional extra icon when the word needs a symbol |

### Variants

| Variant | Meaning |
| --- | --- |
| neutral | Queued, pending, or not connected. Muted on sunken |
| ok | Healthy, done, ready, or connected |
| warn | Reserve, partial, paused, or expired |
| bad | Failed, blocked, exhausted, or open incident |
| info | Running, crawling, or downloading. Uses accent |
| vio | Informational concept such as a parsed year. Uses the info token |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Sunken or tone-tinted fill, 6px dot, 12px semibold label. ADMIN is uppercase display type at 5px radius | Not interactive |
| hover | No pointer change. The fill mixes 4% more ink so the row is visible in this gallery | Cursor stays default |
| focus | Not tabbable. No ring on its own | Parents do not delegate focus to a pill |
| active | Current status adds a 1px ink ring around the pill | Still not interactive. The ring marks the status that matches the surrounding selection |
| disabled | Opacity 0.45 when that status does not apply | Ignored by assistive tech if aria-hidden is set by the view |
| loading | The dot becomes a spinner and the word changes to Checking | aria-busy on the pill. Reduced motion keeps a static dot and the word |
| error | Bad tone plus an alert icon, even when the tone was already bad | The icon keeps the meaning if the color is missed |

### Keyboard

- No key handling. A pill is not in the tab order.

### ARIA

- A span. The text is the status name.
- The dot is decorative (aria-hidden on the pseudo-element).
- Do not use color words alone. Prefer "Cap reached" over a bare bad dot.

## badge

A small mono marker for machine values: ids, counts, and nav tallies. Export
`Badge`. It is not a status pill. Status words belong on pill.

Slice: `web/src/shared/ui/badge`.

Gallery: [USER light](mockups/components.html#user-light-badge).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| children | ReactNode | — | Short mono value |
| tone | 'neutral' or 'accent' or 'bad' | neutral | accent marks the current nav count. bad marks a failing id |

### Variants

| Variant | Meaning |
| --- | --- |
| neutral | Raised mono chip for an id or a key name |
| count | Nav tally. Uses the fav token when it marks unread work |
| current | Accent-soft background for the selected section count |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Raise background, muted mono type, 5px radius | Not interactive |
| hover | No change. Cursor stays default | Not a control |
| focus | Not tabbable and has no ring | The neighbouring control owns focus |
| active | Accent-soft background and ink text when it marks the current item | Still not interactive |
| disabled | Opacity 0.45 | Hidden from the name when the view sets aria-hidden |
| loading | A skeleton of the same height replaces the glyphs | aria-busy on the badge |
| error | Bad text plus an alert icon | The id remains readable |

### Keyboard

- No key handling. A badge is not in the tab order.

### ARIA

- Span using the mono family. Include the value in the parent accessible name when the badge is the only place that value appears.
- Decorative count dots inside a named control are aria-hidden.

## tag

A photo tag. Export `Tag`. When it starts a search it is a button. A static tag
is a span used where the view has no search action.

Slice: `web/src/shared/ui/tag`.

Gallery: [USER light](mockups/components.html#user-light-tag).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| children | ReactNode | — | Tag text |
| href | string | — | When set, the tag is a link that starts a search for that text |
| disabled | boolean | false | Blocks the search |

### Variants

| Variant | Meaning |
| --- | --- |
| button | Starts a search for the tag text |
| static | Read-only tag with no hover color |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Sunken fill, muted text, 6px radius, 12.5px type | Button variant is clickable. Static variant is not |
| hover | Button text turns accent. Static text stays muted | Pointer only on the button variant |
| focus | 2px accent outline on the button variant | Tab reaches button tags only |
| active | Accent-soft fill while pressed or while it is the current search | aria-pressed when it represents the active query |
| disabled | Opacity 0.45 | Removed from the tab order |
| loading | Spinner after the word and aria-busy | Does not start a search |
| error | Bad text and an alert icon. The tag still names the failed query | Stays activatable so the search can be retried |

### Keyboard

- Tab reaches button tags. Enter and Space start the search. Static tags have no keys.

### ARIA

- Button or anchor. The accessible name is the tag text.
- The current search tag sets aria-pressed="true".
- Static tags are spans and are not buttons.

## text-field

A single-line text input with label, helper, and validation. Export `TextField`.
The focus indicator is the accent border plus a 3px accent-soft halo, which
replaces the generic outline so the two do not stack. Empty, filled, and mono
(API key) are the same control.

Slice: `web/src/shared/ui/text-field`.

Gallery: [USER light](mockups/components.html#user-light-text-field).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| label | string | — | Visible label, associated with for/id |
| value | string | '' | Current text |
| type | 'text' or 'password' or 'email' or 'search' | text | Native input type |
| mono | boolean | false | Uses the mono family for keys and ids |
| placeholder | string | — | Example only. Never the only label |
| help | string | — | Helper under the field, 12px muted |
| error | string | — | Validation message. When set, the field is invalid |
| disabled | boolean | false | Not editable |
| loading | boolean | false | Read-only while a check runs |
| required | boolean | false | Adds the required state to the label |

### Variants

| Variant | Meaning |
| --- | --- |
| text | Ordinary sentence or name |
| password | Obscured secret. The view must not echo it into a toast or a URL |
| mono | API key or other machine value. Placeholder may say 32 hex characters. No real secret is prefilled |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | line2 border, surface fill (ADMIN sunken), 10px radius (ADMIN 7px), 8px 12px padding | Editable. Helper text uses the muted token |
| hover | Border turns faint | Pointer cursor over the field |
| focus | Border becomes accent and the halo is 0 0 0 3px accent-soft. Outline is none | Tab moves into the field. The caret is visible |
| active | Same halo while the pointer is down. The caret stays visible | Typing inserts characters |
| disabled | Sunken fill, opacity 0.7, native disabled | Removed from the tab order |
| loading | Spinner inside the end of the field, aria-busy, read-only | The value is not edited while a check runs. Reduced motion keeps a static spinner ring |
| error | Bad border, aria-invalid, and the error string in bad under the field. Focus halo uses a bad mix instead of accent-soft | The helper remains. The error is also referenced by aria-describedby |

### Keyboard

- Tab enters and leaves the field.
- Characters edit the value. Escape does not clear it.
- A disabled or loading field does not accept edits.

### ARIA

- Native input with a label element.
- aria-invalid="true" when the error string is set.
- aria-describedby points at the helper and, when invalid, the error.
- aria-busy="true" while loading.

## select

A native listbox trigger. Export `Select`. It uses the same border, halo, and
validation pattern as text-field so forms line up.

Slice: `web/src/shared/ui/select`.

Gallery: [USER light](mockups/components.html#user-light-select).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| label | string | — | Visible label |
| value | string | — | Selected option value |
| options | {value: string, label: string}[] | — | Visible options. Labels may wrap |
| help | string | — | Helper text |
| error | string | — | Validation message |
| disabled | boolean | false | Blocks opening |
| loading | boolean | false | Options are not ready |

### Variants

| Variant | Meaning |
| --- | --- |
| single | One choice, such as language |
| placeholder | No selection yet. The first option is an empty prompt and is not a real value |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Same metrics as text-field, with the native disclosure | Opens the platform list |
| hover | Border turns faint | Pointer cursor |
| focus | Accent border and 3px accent-soft halo | Tab focuses the select. Arrow keys move the highlighted option while it is open |
| active | The list is open and the highlighted option uses accent-soft | Enter or a click commits the option. Escape closes without committing |
| disabled | Native disabled, opacity 0.7 | Not in the tab order |
| loading | Spinner at the end, aria-busy, options disabled | Does not open |
| error | Bad border, aria-invalid, error text such as Choose a language | Focus halo uses the bad mix. The error is in aria-describedby |

### Keyboard

- Tab focuses the closed select.
- Arrow Up and Arrow Down change the selected option. Enter or Space opens the platform list where the browser does that.
- Escape closes an open list and restores focus to the select.

### ARIA

- Native select with a label.
- aria-invalid="true" and aria-describedby when error is set.
- aria-busy="true" while options load.

## switch

An immediate on/off control. Export `Switch`. The thumb uses the resting
elevation shadow. The choice is saved by the view; this slice only reports the
change.

Slice: `web/src/shared/ui/switch`.

Gallery: [USER light](mockups/components.html#user-light-switch).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| checked | boolean | false | On when true |
| label | string | — | Accessible name. A visible label is passed as children of the row, or aria-label when the row already shows the name |
| disabled | boolean | false | Blocks toggling |
| loading | boolean | false | The previous value stays until the request finishes |
| error | string | — | Save failed. The switch keeps the last committed value |

### Variants

| Variant | Meaning |
| --- | --- |
| off | line2 track, white thumb at the start |
| on | Accent track, thumb translated 18px |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | 42 by 24 track, 18px white thumb, resting shadow 0 1px 3px rgba(0,0,0,.25) | Click or Space/Enter toggles |
| hover | Track color mixes toward faint | Pointer cursor |
| focus | 2px accent outline, 2px offset | Tab focuses the switch |
| active | Checked. Accent track and the thumb slides 18px over 0.15s (motion.feedback). aria-checked is true | Under reduced motion the thumb jumps to the end position |
| disabled | Opacity 0.45 and aria-disabled | Not in the tab order |
| loading | Spinner beside the track, aria-busy, value locked | Does not toggle. Reduced motion keeps a static ring |
| error | Bad outline on the track and the error string beside it, for example Sharing choice could not be saved | Stays activatable so the person can try again. aria-invalid is true |

### Keyboard

- Tab focuses the switch. Space and Enter toggle it. Arrow keys do nothing.

### ARIA

- button with role="switch" and aria-checked="true" or "false".
- The accessible name comes from aria-label or a labelling element.
- aria-invalid="true" when error is set. aria-busy="true" while loading.

## checkbox

A form or filter checkbox. Export `Checkbox`. Filter rows may show a trailing
count. Indeterminate is the loading state for a parent checkbox, not a third
saved value.

Slice: `web/src/shared/ui/checkbox`.

Gallery: [USER light](mockups/components.html#user-light-checkbox).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| checked | boolean | false | Checked |
| label | string | — | Visible label |
| count | number | — | Optional trailing facet count |
| disabled | boolean | false | Blocks the change |
| error | string | — | Validation message, such as a required terms box |

### Variants

| Variant | Meaning |
| --- | --- |
| plain | Label beside a 16px box |
| count | Filter row. The count sits at the end in faint 12px type |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | 16px box, accent-color from the accent token, label in 14px | Clicking the label toggles the box |
| hover | The box border strengthens to accent | Pointer cursor on the label |
| focus | 2px accent outline around the box | Tab focuses the input |
| active | Checked. The platform draws the accent mark. aria-checked is true | Space toggles it |
| disabled | Opacity 0.45, native disabled | Not in the tab order |
| loading | aria-checked="mixed" and a spinner after the label while child rows resolve | Does not toggle. Reduced motion keeps the mixed mark without spinning |
| error | Bad outline, aria-invalid, and the error string. Example: Accept the terms to continue | Stays focusable so the person can check it |

### Keyboard

- Tab focuses the checkbox. Space toggles it. Enter does not toggle a native checkbox.

### ARIA

- Native checkbox inside a label.
- aria-invalid="true" and aria-describedby when error is set.
- Loading sets aria-checked="mixed" and aria-busy="true".

## table

A data table with a caption, a header, and optional row actions. Export `Table`.
Cells use tabular numbers. Long text reflows into a wrap column instead of
clipping a name.

Slice: `web/src/shared/ui/table`.

Gallery: [USER light](mockups/components.html#user-light-table).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| caption | string | — | Names the table. Visually available, not only for screen readers |
| columns | {id, header, align}[] | — | Header cells. A leading > in the POC meant right alignment; here align is end or start |
| rows | Row[] | — | Body rows |
| selectedId | string | — | The active row |
| onRowActivate | (id) => void | — | When set, rows are buttons reached with the keyboard |

### Variants

| Variant | Meaning |
| --- | --- |
| plain | Read-only rows |
| interactive | Rows activate on click and Enter |
| selected | The active row uses accent-soft |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Full width, uppercase muted headers, line dividers, 13.5px cells | Horizontal scroll on narrow widths. The caption stays visible |
| hover | An interactive row fills sunken | Pointer cursor only when the row activates |
| focus | 2px accent outline on the focused row | Tab enters the table. Arrow Up and Arrow Down move between interactive rows |
| active | Selected row uses accent-soft and aria-selected | Enter activates an interactive row |
| disabled | Row opacity 0.45 and its buttons are disabled | That row is skipped |
| loading | Header stays. Body cells are skeleton bars. aria-busy on the table | No row activation. Reduced motion shows static skeletons |
| error | A full-width bad row replaces the body: the message and a retry button | The retry button is the only tab stop inside the body |

### Keyboard

- Tab moves to the table, then through interactive rows and row buttons.
- Arrow Up and Arrow Down move the selected interactive row. Home and End jump to the ends.
- Enter activates the focused row when onRowActivate is set.

### ARIA

- Native table with a caption element.
- Interactive rows set aria-selected. The grid does not use role="grid" unless a later pattern requires cell editing; this slice is a table.
- Loading sets aria-busy="true". The error row is an alert inside the table.

## card

A surface container with the theme radius and a line border. Export `Card`. An
interactive card is a button that wraps the whole surface; nested buttons are
not allowed inside it.

Slice: `web/src/shared/ui/card`.

Gallery: [USER light](mockups/components.html#user-light-card).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| children | ReactNode | — | Body |
| interactive | boolean | false | The whole card activates |
| title | string | — | Optional heading |

### Variants

| Variant | Meaning |
| --- | --- |
| static | Groups related text. Padding 18px |
| interactive | Button card. Hover lifts with the overlay shadow |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Surface, 1px line border, theme radius, 18px padding | Static cards do not activate |
| hover | Interactive cards raise with 0 10px 30px var(--shade) | Pointer cursor only when interactive |
| focus | 2px accent outline when the card is a button | Tab focuses an interactive card |
| active | 1px translate down while pressed | Enter or Space runs the card action |
| disabled | Opacity 0.45 and the button is disabled | Not in the tab order |
| loading | Body replaced by skeleton lines. The title can remain. aria-busy | No activation. Reduced motion uses static blocks |
| error | Bad border and an inline error with a retry button | The retry button is a nested control, so this card is static, not an interactive wrapper |

### Keyboard

- A static card has no keys.
- An interactive card is one tab stop. Enter and Space activate it.

### ARIA

- Static card is a section or a div. Interactive card is a button.
- Do not put another button inside an interactive card.
- Loading sets aria-busy="true".

## panel

A titled region with a header and a body. Export `Panel`. ADMIN panel titles use
the panelTitle type: uppercase, muted, 0.08em tracking. The header may hold
actions.

Slice: `web/src/shared/ui/panel`.

Gallery: [USER light](mockups/components.html#user-light-panel).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| title | string | — | Header heading |
| actions | ReactNode | — | Header end slot |
| children | ReactNode | — | Body |

### Variants

| Variant | Meaning |
| --- | --- |
| plain | Header plus body |
| actions | Header with buttons aligned to the end |
| feed | Body is a scrolling list. The header stays put |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Surface, line border, theme radius. Header has a line divider and 11px 14px padding | The body scrolls when the feed variant is used |
| hover | Header actions use their own hover. The panel itself does not lift | Cursor depends on the control under the pointer |
| focus | The focused header action shows the 2px accent outline | Tab moves through header actions, then the body |
| active | A 2px accent border on the start edge marks the current panel | The current panel is the one the view selected |
| disabled | Header actions are disabled and the body is inert | Actions leave the tab order |
| loading | Body is skeleton blocks. aria-busy on the panel | Header actions that depend on data are disabled. Reduced motion keeps static blocks |
| error | A bad banner fills the body and offers retry | The retry button is focused first when the error appears after a load |

### Keyboard

- Tab visits header actions, then controls in the body. The panel container is not a tab stop.

### ARIA

- section with an h2 in production. This gallery uses h4 only because the slice name is already an h3.
- aria-busy="true" while loading.
- The current panel sets aria-current="true" on the section.

## stat-strip

A row of figures separated by the line token. Export `StatStrip`. Values use
tabular figures. A segment may be a link when it filters a view.

Slice: `web/src/shared/ui/stat-strip`.

Gallery: [USER light](mockups/components.html#user-light-stat-strip).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| items | {label, value, detail, tone}[] | — | Two to four segments |
| onSelect | (id) => void | — | When set, each segment is a button |

### Variants

| Variant | Meaning |
| --- | --- |
| static | Read-only figures |
| selectable | The active segment filters the page |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Surface segments, line gaps, eyebrow label, display value, muted detail | Values use tabular-nums |
| hover | The segment under the pointer fills raise | Pointer cursor only when selectable |
| focus | 2px accent outline on a selectable segment | Tab moves between selectable segments |
| active | The chosen segment fills accent-soft and sets aria-pressed | Enter selects it |
| disabled | Opacity 0.45 | Selectable segments are removed from the tab order |
| loading | Labels stay. Values are skeleton bars. aria-busy | No selection. Reduced motion uses static bars |
| error | One value turns bad and gains an alert icon, with the word Failed in the detail | Other segments stay readable |

### Keyboard

- When selectable, Tab and Enter or Space choose a segment. Otherwise the strip has no keys.

### ARIA

- A group labelled by a visually available name from the view, or by the segment labels themselves.
- Selectable segments are buttons with aria-pressed.
- Loading sets aria-busy="true" on the strip.

## banner

A page or section message with an icon and an optional action. Export `Banner`.
Info, warn, and bad are the tones. The icon matches the tone so color is not the
only signal.

Slice: `web/src/shared/ui/banner`.

Gallery: [USER light](mockups/components.html#user-light-banner).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| tone | 'info' or 'warn' or 'bad' | info | Border and tint |
| children | ReactNode | — | Message copy |
| action | ReactNode | — | Optional button at the end |

### Variants

| Variant | Meaning |
| --- | --- |
| info | Accent tint. Informational and not a failure |
| warn | Warn tint. Needs attention, work can continue |
| bad | Bad tint. The action failed or data is missing |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Tinted fill, 1px tone border, theme radius, icon at the start | The action button uses the button slice |
| hover | The action uses button hover. The banner fill does not move | Pointer cursor on the action only |
| focus | The action shows the 2px accent outline | Tab reaches the action |
| active | The action shows the pressed button treatment | Enter or Space runs it |
| disabled | The action is disabled. The message stays at full opacity so it can still be read | The action leaves the tab order |
| loading | The icon slot becomes a spinner and aria-busy is set | The action is disabled until the request finishes. Reduced motion keeps a static ring |
| error | The bad tone, alert icon, and a retry action | role="alert" so the failure is announced |

### Keyboard

- Tab reaches the action button only. Enter and Space activate that button.

### ARIA

- Info and warn use role="status". Bad uses role="alert".
- The icon is aria-hidden. The message text carries the meaning.
- Loading sets aria-busy="true".

## progress-bar

A determinate meter for quota, crawl, or download progress. Export
`ProgressBar`. The width animates with motion.progress (0.6s). Tone follows the
value: ok under 75, warn from 75, bad from 90, unless the view passes a tone.

Slice: `web/src/shared/ui/progress-bar`.

Gallery: [USER light](mockups/components.html#user-light-progress-bar).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| value | number | — | 0 to 100 |
| label | string | — | Accessible name, such as API calls used |
| tone | 'accent' or 'ok' or 'warn' or 'bad' | — | Defaults from the value bands |
| thin | boolean | false | 4px track instead of 6px |

### Variants

| Variant | Meaning |
| --- | --- |
| accent | Neutral work in progress |
| ok | Healthy load under 75 |
| warn | Load from 75 through 89 |
| bad | Load at 90 or more, or a failed job |
| thin | 4px track for dense tables |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Raise track, accent or tone fill, 6px height, role progressbar | aria-valuenow matches the width |
| hover | No change. The bar is not a control | Cursor stays default |
| focus | Not tabbable and has no ring of its own | A labelled parent owns focus |
| active | Value 100 and ok fill. The text says Complete | Width transition is 0.6s unless reduced motion is on |
| disabled | Opacity 0.45 and aria-disabled | Not updated while the parent job is paused |
| loading | Indeterminate stripe. aria-busy and no valuenow | Under reduced motion the stripe stops and the word Loading remains |
| error | Bad fill, aria-valuetext Failed, and a visible Failed label | The meter stays in the accessibility tree |

### Keyboard

- No key handling. The bar is not in the tab order.

### ARIA

- role="progressbar" with aria-valuemin="0", aria-valuemax="100", and aria-valuenow when determinate.
- aria-valuetext carries words such as Complete or Failed.
- aria-label or aria-labelledby names the meter. The visible percentage is text next to the bar.

## ring

A circular meter for a single percentage. Export `Ring`. The arc uses accent,
warn, or bad. The center shows the percent and a short label.

Slice: `web/src/shared/ui/ring`.

Gallery: [USER light](mockups/components.html#user-light-ring).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| value | number | — | 0 to 100 |
| label | string | — | Word under the percent, such as used |
| tone | 'accent' or 'warn' or 'bad' or 'ok' | accent | Arc color |
| size | number | 92 | Pixel size of the square |

### Variants

| Variant | Meaning |
| --- | --- |
| accent | Ordinary usage |
| warn | Usage from 75 through 89 |
| bad | Usage at 90 or more, or a failure |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Raise track, tone arc, percent in display type, label in muted 10px | role img with an aria-label of the percent and the word |
| hover | No change | Not a control |
| focus | 2px accent outline around the svg when a parent delegates focus | The ring itself is not tabbable |
| active | Value 100 and ok arc. The word is Done | The dash transition is motion.progress. Reduced motion snaps to the end arc |
| disabled | Opacity 0.45 | Not updated while paused |
| loading | The arc spins and the center says Loading | aria-busy. Reduced motion shows a static arc and the word |
| error | Bad arc, the word Failed, and an alert icon under the ring | aria-label includes Failed |

### Keyboard

- No key handling. The ring is not in the tab order.

### ARIA

- svg with role="img" and an aria-label such as "62% used".
- The center glyphs are aria-hidden because the label repeats them.
- Loading sets aria-busy="true" on the svg.

## sparkline

A small inline trend with no axes. Export `Sparkline`. One series is an SVG
polyline plus an area fill at 12% opacity. The last value is available as text
next to it.

Slice: `web/src/shared/ui/sparkline`.

Gallery: [USER light](mockups/components.html#user-light-sparkline).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| points | number[] | — | Series values |
| label | string | — | Accessible name of the trend |
| color | 's1' or 's2' or 's3' or 'bad' | s1 | Stroke token |

### Variants

| Variant | Meaning |
| --- | --- |
| area | Stroke plus a 12% fill down to the baseline |
| point | The last point is marked when the row is current |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | 28px tall SVG polyline and a translucent polygon in the series color | Decorative if a neighbouring text value repeats the latest number. Otherwise role img |
| hover | A tooltip shows the latest value | Not separately tabbable when the row already focuses |
| focus | 2px accent outline and the same tooltip | Focused only when the sparkline is the row control |
| active | The last point draws an 8px marker | The marker uses the series color and a surface halo |
| disabled | Stroke uses faint and the fill drops to 6% opacity | Not interactive |
| loading | A skeleton rectangle of the same height | aria-busy. Reduced motion is the same static block |
| error | Stroke and fill use bad, with the word Failed beside it | aria-label includes Failed |

### Keyboard

- When the sparkline is inside a named row, the row takes the keys.
- When it is the control, Tab focuses it and the tooltip opens. Escape closes the tooltip.

### ARIA

- role="img" and an aria-label that includes the latest value, unless the graphic is aria-hidden because adjacent text repeats it.
- The tooltip is role="tooltip" and referenced while open.

## chart

A line, area, or bar chart with axes, a legend, and a tooltip. Export `Chart`.
Series colors are s1, s2, and s3. Every series is named in the legend and in the
tooltip. The graphic also has an accessible name.

Slice: `web/src/shared/ui/chart`.

Gallery: [USER light](mockups/components.html#user-light-chart).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| kind | 'line' or 'area' or 'bar' | line | Geometry. Area is a line with a 12% fill and is used for a single series |
| series | {name, color, data}[] | — | Named series. Colors are s1, s2, or s3 |
| label | string | — | Accessible name of the whole chart |
| format | (value) => string | — | Tick and tooltip formatter |

### Variants

| Variant | Meaning |
| --- | --- |
| line | Polyline per series. A marker sits on the latest point |
| area | One series with a translucent fill to the baseline |
| bar | Vertical bars. The current hour uses accent. A bar at the cap uses bad |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Axes in faint mono, gridlines in line, legend swatches, latest values beside the names | The tooltip is hidden until pointer or keyboard focus |
| hover | A crosshair follows the pointer and the tooltip shows every series at that index | Tooltip uses the popover shadow |
| focus | The chart outline is 2px accent. The tooltip stays open on the focused index | Tab focuses the chart. Arrow Left and Arrow Right move the index |
| active | The focused index grows a larger marker and the tooltip stays pinned | Enter does not navigate. The view decides row actions |
| disabled | Opacity 0.45. The tooltip does not open | Removed from the tab order |
| loading | Skeleton plot and a skeleton legend. aria-busy | No tooltip. Reduced motion is a static skeleton |
| error | The plot is replaced by a bad banner and retry. The legend remains so the series names are not lost | Retry is the tab stop |

### Keyboard

- Tab focuses the chart when it is not disabled or loading.
- Arrow Left and Arrow Right move the tooltip index. Home and End jump to the ends.
- Escape closes the tooltip and leaves focus on the chart.

### ARIA

- The svg is role="img" with aria-label naming the series.
- The legend text is real text, not only color.
- The tooltip is role="tooltip". Loading sets aria-busy="true". The error banner is role="alert".

## photo-grid

A justified photo row. Export `PhotoGrid`. Each tile sets flex-grow and
flex-basis from its aspect ratio times the row-h token (190px, and 120px at
max-width 720px). Placeholders keep that ratio. They are abstract sunken
artwork, never a broken image and never a stand-in that pretends to be the
photograph.

Slice: `web/src/shared/ui/photo-grid`.

Gallery: [USER light](mockups/components.html#user-light-photo-grid).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| photos | {id, title, owner, ratio, alt}[] | — | Tiles. alt is empty when the caption next to it repeats the description |
| onOpen | (id) => void | — | Opens the lightbox |
| onTray | (id) => void | — | Toggles the download tray |

### Variants

| Variant | Meaning |
| --- | --- |
| plain | Title and owner appear in the caption on hover and focus |
| flags | Sizes pending and starred flags sit on the tile |
| tray | A round tray button on the corner |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Justified row, 6px gap, 6px tile radius, caption hidden | Click or Enter opens the photo |
| hover | The artwork scales to 1.03 over motion.emphasis (0.35s) and the caption fades in | The tray button becomes visible. Reduced motion skips the scale and shows the caption immediately |
| focus | Caption visible and a 2px accent outline on the tile | Tab uses a roving tabindex. Arrow keys move between tiles |
| active | The tray button is pressed (accent fill, aria-pressed true) and stays visible | Enter on the tray button toggles the tray and does not open the photo |
| disabled | Opacity 0.45. Tiles do not open | Removed from the tab order |
| loading | Skeleton tiles in the same justified geometry | aria-busy. Reduced motion is a static sunken block |
| error | The failed tile has a bad border, an alert icon, and a retry button | Retry is a separate button from Open |

### Keyboard

- The grid is one tab stop with a roving tabindex.
- Arrow Left and Arrow Right move between tiles. Home and End jump to the ends.
- Enter or Space on a tile opens it. Enter or Space on the tray button toggles the tray.

### ARIA

- Each tile is a button named with the title and owner.
- The tray control is a separate button with aria-pressed and a name of Add to download tray or Remove from download tray.
- Decorative artwork is aria-hidden when the button name already describes the photo.

## contact-card

A contact summary with a photo mosaic, an overlapping avatar, a star, counts,
and a status pill. Export `ContactCard`. The mosaic is placeholder artwork until
real thumbnails exist. The avatar is initials on a solid color when no profile
image is available.

Slice: `web/src/shared/ui/contact-card`.

Gallery: [USER light](mockups/components.html#user-light-contact-card).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| name | string | — | Contact name |
| photos | number | — | Stored photo count |
| depth | number | — | Discovery depth |
| status | string | — | Crawl status passed to pill |
| starred | boolean | false | Star pressed |
| onOpen | () => void | — | Opens the contact |

### Variants

| Variant | Meaning |
| --- | --- |
| resting | Unstarred contact |
| starred | Star uses the filled star glyph and stays visible |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Line border, theme radius, 16:9 mosaic, avatar overlapping the body, meta line, pill, and a progress bar | The card opens the contact. The star is a separate button |
| hover | Overlay shadow 0 10px 30px var(--shade) | Pointer cursor |
| focus | 2px accent outline on the card. A focused star shows its own ring | Tab stops on the card, then the star |
| active | 1px translate while the card is pressed. A pressed star sets aria-pressed | Enter on the card opens it. Enter on the star does not open the card |
| disabled | Opacity 0.45. Neither control activates | Removed from the tab order |
| loading | Skeleton mosaic, skeleton lines, aria-busy | No open and no star. Reduced motion uses static blocks |
| error | The mosaic is replaced by a bad banner and retry | Retry is the tab stop. The name remains |

### Keyboard

- Tab focuses the card, then the star button.
- Enter or Space on the card opens the contact. Enter or Space on the star toggles it.

### ARIA

- The card is a button named with the contact name.
- The star is a button with aria-pressed and aria-label Star or Starred.
- The avatar text is the initials and is aria-hidden because the name is next to it.

## album-card

An album with a stacked cover, title, owner, and stored-of-total counts. Export
`AlbumCard`. Empty albums show a sunken stack and the empty-block copy belongs
to the view, not a fake cover.

Slice: `web/src/shared/ui/album-card`.

Gallery: [USER light](mockups/components.html#user-light-album-card).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| title | string | — | Album title |
| owner | string | — | Owner name |
| stored | number | — | Photos stored |
| total | number | — | Photos on Flickr |
| onOpen | () => void | — | Opens the album |

### Variants

| Variant | Meaning |
| --- | --- |
| stack | Three covers, the front one straight, the back two rotated |
| empty | A single sunken plate and the image icon when the album has no public photos |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Transparent button, stacked covers at 4:3, title and counts under it | Click opens the album |
| hover | The front cover lifts with the hover shadow | Pointer cursor |
| focus | 2px accent outline around the stack | Tab focuses the card |
| active | 1px translate down while pressed | Enter or Space opens the album |
| disabled | Opacity 0.45 | Not in the tab order |
| loading | Skeleton stack and skeleton text. aria-busy | Does not open |
| error | Bad border on the stack, the word Failed, and a retry button beside the title | Retry does not open the album |

### Keyboard

- Tab focuses the card. Enter or Space opens it. A retry button, when present, is the next tab stop.

### ARIA

- Button named with the title, owner, and counts.
- Cover artwork is aria-hidden.
- Loading sets aria-busy="true".

## lightbox

The full-screen photo viewer. Export `Lightbox`. Production position is fixed
over the overlay token. This gallery draws it inline so every state is visible.
Focus is trapped. Escape closes and returns focus to the tile that opened it.

Slice: `web/src/shared/ui/lightbox`.

Gallery: [USER light](mockups/components.html#user-light-lightbox).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| photo | {title, owner, alt} | — | Current photo |
| index | number | — | Position in the list |
| count | number | — | List length |
| onClose | () => void | — | Closes the viewer |
| onStep | (delta) => void | — | Previous or next |

### Variants

| Variant | Meaning |
| --- | --- |
| panel | Stage plus a 400px side panel. Below 900px the panel stacks under the stage |
| tray | The tray button in the top bar |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Overlay scrim, stage, previous and next buttons, close, and a side panel with title and sizes | Arrow keys step through the list |
| hover | Nav buttons lighten. The side panel does not scroll on its own | Pointer cursor on the controls |
| focus | Initial focus is the close button, which shows the 2px accent outline | Tab cycles inside the dialog. Shift+Tab moves backward |
| active | The next button shows the pressed treatment | Enter on next moves one photo |
| disabled | Previous is disabled on the first photo. Opacity 0.45 | Arrow Left does nothing on the first photo. Focus stays in the dialog |
| loading | Spinner on the stage and skeleton lines in the panel. aria-busy | Step controls are disabled. Reduced motion keeps a static spinner ring |
| error | Bad banner on the stage: Photo failed to load, plus retry. The close button remains | Retry loads again. Escape still closes |

### Keyboard

- Escape closes and restores focus to the opener.
- Arrow Left and Arrow Right move between photos. Home and End jump to the ends.
- Tab is trapped inside the dialog.

### ARIA

- role="dialog" and aria-modal="true", labelled by the photo title.
- Previous, next, and close are buttons with names.
- Loading sets aria-busy="true" on the dialog.

## modal

A dialog for a short task such as adding a key. Export `Modal`. Production is
centered on a scrim with the modal elevation. The gallery keeps it inline.
Escape closes. Focus returns to the invoker.

Slice: `web/src/shared/ui/modal`.

Gallery: [USER light](mockups/components.html#user-light-modal).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| title | string | — | Dialog heading |
| children | ReactNode | — | Body |
| footer | ReactNode | — | Actions, end aligned |
| onClose | () => void | — | Close |

### Variants

| Variant | Meaning |
| --- | --- |
| form | Fields plus a primary submit and a ghost cancel |
| confirm | Short copy plus danger and ghost actions |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Surface, line2 border, theme radius, modal shadow, header, body, footer | The scrim click closes only when the view allows it |
| hover | Footer buttons use button hover | Pointer cursor on the controls |
| focus | Initial focus is the first field, with the field halo. Tab is trapped | Shift+Tab wraps inside the dialog |
| active | The primary button shows the pressed treatment | Enter in a text field submits when the form is valid |
| disabled | The primary button is disabled until the form is valid | That button is skipped. Cancel stays available |
| loading | The primary button shows a spinner and aria-busy is set on the dialog | Fields are read-only. Reduced motion keeps a static ring |
| error | The invalid field uses the text-field error state and the footer primary remains | Focus moves to the invalid field |

### Keyboard

- Escape closes unless a request is in flight.
- Tab and Shift+Tab cycle inside the dialog.
- Enter submits from a text field. A disabled primary does not submit.

### ARIA

- role="dialog", aria-modal="true", aria-labelledby the title.
- The close button is named Close.
- Loading sets aria-busy="true". Invalid fields set aria-invalid.

## drawer

A side panel for a longer task or a notification list. Export `Drawer`.
Production is fixed to the end edge with the drawer shadow. Narrow is min(380px,
100%). Default is min(560px, 100%). The gallery draws it inline.

Slice: `web/src/shared/ui/drawer`.

Gallery: [USER light](mockups/components.html#user-light-drawer).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| title | string | — | Heading |
| children | ReactNode | — | Scrollable body |
| width | 'default' or 'narrow' | default | Narrow is the notifications panel |
| onClose | () => void | — | Close |

### Variants

| Variant | Meaning |
| --- | --- |
| default | 560px task drawer |
| narrow | 380px list, such as notifications |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Surface panel, line2 start border, header, scrolling body | The scrim is the same as modal |
| hover | A list row fills sunken | Pointer cursor on rows and buttons |
| focus | Initial focus is the heading (tabindex -1). The close button shows the ring when it is focused | Tab is trapped |
| active | The current row uses accent-soft and aria-current | Enter opens that row |
| disabled | Actions in the header are disabled | Those buttons leave the tab order. Close stays available |
| loading | Skeleton rows. aria-busy | Close still works. Reduced motion uses static rows |
| error | A bad banner at the top of the body and a retry button | Focus moves to the banner action |

### Keyboard

- Escape closes and restores focus to the invoker.
- Tab is trapped. Arrow Up and Arrow Down move between list rows when the body is a list.

### ARIA

- role="dialog", aria-modal="true", aria-labelledby the title.
- List rows that navigate are buttons.
- Loading sets aria-busy="true".

## toast

A short confirmation that does not block the page. Export `Toast`. Production
anchors a polite live region at the bottom center. The entrance is
motion.entrance (0.2s, 8px, from opacity 0). Hover or focus pauses the timer.

Slice: `web/src/shared/ui/toast`.

Gallery: [USER light](mockups/components.html#user-light-toast).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| children | ReactNode | — | Message |
| tone | 'neutral' or 'bad' | neutral | bad is a failure that uses role alert |
| action | ReactNode | — | Optional undo or dismiss |
| duration | number | 2600 | Milliseconds. Loading toasts do not auto-dismiss |

### Variants

| Variant | Meaning |
| --- | --- |
| neutral | Ink fill, surface text, overlay shadow, 12px radius |
| bad | Same shape with a bad start border and role alert |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Ink background, surface text, overlay shadow | Appears with an 8px rise. Auto-dismisses |
| hover | Shadow strengthens to the modal level and the timer pauses | Pointer over the toast |
| focus | 2px accent outline. The timer pauses | Tab can move to the dismiss button. Focus is not stolen if the person is typing |
| active | The dismiss button shows the pressed treatment | Enter activates dismiss |
| disabled | No dismiss button. The toast is informational and still announced | It leaves on the timer. It is not a disabled control |
| loading | Spinner before the message. The timer does not run. aria-busy | Reduced motion keeps a static ring. The toast stays until the request finishes |
| error | Bad start border, alert icon, role alert, and a retry button | Does not steal focus. Retry is available if the person tabs to it |

### Keyboard

- The toast does not move focus when it appears.
- If the person tabs to it, Enter activates the action and Escape dismisses it.

### ARIA

- The region is aria-live="polite" and aria-atomic="true".
- A bad toast uses role="alert".
- Loading sets aria-busy="true".

## tabs

Page tabs and subtabs. Export `Tabs`. Page tabs are the navtab treatment: a text
button with an accent bottom border when selected. Subtabs are chips. Arrow keys
move inside the tablist that has focus.

Slice: `web/src/shared/ui/tabs`.

Gallery: [USER light](mockups/components.html#user-light-tabs).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| tabs | {id, label, count}[] | — | Visible tabs |
| value | string | — | Selected id |
| variant | 'page' or 'subtab' | page | Page tabs or chip subtabs |
| onChange | (id) => void | — | Selection |

### Variants

| Variant | Meaning |
| --- | --- |
| page | Bottom border. Used for primary navigation within a page header |
| subtab | Chip group under a page tab, such as Photos and Albums |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Page tab is muted text. Subtab is an unpressed chip | Click selects |
| hover | The label turns ink | Pointer cursor |
| focus | 2px accent outline | Arrow Left and Arrow Right move inside the tablist |
| active | Page tab uses ink text and an accent bottom border. Subtab uses the pressed chip. aria-selected is true | The tabpanel is shown |
| disabled | Opacity 0.45 and aria-disabled. A disabled tab is skipped by arrows | It cannot be selected |
| loading | The selected tab sets aria-busy and the panel is a skeleton | Arrow keys still move. Reduced motion uses a static skeleton |
| error | The tabpanel shows a bad banner and retry. The selected tab stays | Retry is inside the panel |

### Keyboard

- Tab enters the active tab, then the panel.
- Arrow Left and Arrow Right (or Up and Down in a vertical list) move between enabled tabs and select them.
- Home and End jump to the ends. A disabled tab is skipped.

### ARIA

- role="tablist", role="tab", and role="tabpanel".
- The selected tab sets aria-selected="true" and tabindex="0". Other tabs are tabindex="-1".
- Each tab controls its panel with aria-controls. Loading sets aria-busy on the panel.

## breadcrumb

The parent trail above a page title. Export `Breadcrumb`. The current page is
text, not a link. A back icon sits on the parent link.

Slice: `web/src/shared/ui/breadcrumb`.

Gallery: [USER light](mockups/components.html#user-light-breadcrumb).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| items | {label, href}[] | — | Parents, then the current page. The last item has no href |

### Variants

| Variant | Meaning |
| --- | --- |
| parent | A button or link with the left icon |
| current | The page name, aria-current="page" |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | 13px muted parent, a separator, ink current page | The parent navigates |
| hover | The parent turns accent | Pointer cursor on the parent only |
| focus | 2px accent outline on the parent | Tab focuses the parent link |
| active | The current crumb is ink and is not a link | aria-current="page" |
| disabled | A parent that cannot be opened is opacity 0.45 and not a link | It is skipped |
| loading | Skeleton crumbs in place of the labels. aria-busy | No navigation |
| error | The last crumb reads Page not found in bad, with an alert icon | Parents that still resolve remain links |

### Keyboard

- Tab focuses each parent link. Enter follows it. The current page is not a tab stop.

### ARIA

- nav with aria-label="Breadcrumb".
- An ordered list. The current item sets aria-current="page".
- Separators are aria-hidden.

## empty-block

The placeholder when a list has no rows. Export `EmptyBlock`. It shows an icon
in a circle, a title, one sentence, and an optional call to action. Loading and
error are separate states so a view does not flash an empty message before the
request finishes.

Slice: `web/src/shared/ui/empty-block`.

Gallery: [USER light](mockups/components.html#user-light-empty-block).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| icon | IconName | — | Design-system icon inside the circle |
| title | string | — | Short heading |
| children | ReactNode | — | One sentence, max about 46ch |
| action | ReactNode | — | Optional call to action button |

### Variants

| Variant | Meaning |
| --- | --- |
| plain | Explanation only |
| action | Explanation plus one button |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Centered column, 72px sunken circle, faint icon, ink title, muted sentence | The action uses the button slice |
| hover | The action uses button hover. The circle does not move | Pointer cursor on the action |
| focus | The action shows the 2px accent outline | Tab focuses the action |
| active | The action shows the pressed treatment | Enter activates it |
| disabled | The action is disabled. The explanation stays readable | The action leaves the tab order |
| loading | Skeleton circle and two skeleton lines. This is shown while the view does not yet know the list is empty | aria-busy. No empty copy yet |
| error | Alert icon, bad title, and a retry call to action | role="alert" |

### Keyboard

- Tab focuses the call to action when it exists. Enter and Space activate it.

### ARIA

- The title is a heading in the production view.
- The icon is aria-hidden.
- The error state uses role="alert". Loading sets aria-busy="true".

## skeleton

A placeholder block used by other slices while data loads. Export `Skeleton`.
The loading state shimmers over 1.2s (motion.loading). Reduced motion shows the
static sunken block, which is also the default state.

Slice: `web/src/shared/ui/skeleton`.

Gallery: [USER light](mockups/components.html#user-light-skeleton).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| width | string | 100% | CSS length |
| height | string | 14px | CSS length |
| radius | string | 8px | Corner radius |

### Variants

| Variant | Meaning |
| --- | --- |
| text | A line the height of body text |
| block | A media or panel placeholder |
| circle | An avatar placeholder |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Static sunken block. This is also the reduced-motion end state | Not interactive |
| hover | No change | Not a control |
| focus | Not tabbable and has no ring | No focus |
| active | No change. Skeleton has no pressed state | No activation |
| disabled | Faint color at 0.55 opacity for a region that cannot load | aria-hidden when a neighbour explains the block |
| loading | Shimmer from sunken through raise, background-size 200%, animation 1.2s infinite | aria-busy on the parent. Reduced motion cancels the animation |
| error | Bad border and the words Could not preview | The text is the accessible name |

### Keyboard

- No key handling. A skeleton is not in the tab order.

### ARIA

- aria-hidden="true" when the parent already sets aria-busy and exposes a loading name.
- The error state removes aria-hidden and shows the words Could not preview.
- Do not announce every bar. One busy parent is enough.

## node-card

An operations summary for one crawl or control node. Export `NodeCard`. It shows
the role, egress address, health pill, resource bars, and a sparkline. Selecting
it opens the node.

Slice: `web/src/shared/ui/node-card`.

Gallery: [USER light](mockups/components.html#user-light-node-card).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| name | string | — | Node id |
| health | 'ok' or 'warn' or 'bad' | ok | Drives the border tint and the pill |
| resources | {cpu, ram, disk, net} | — | Percents for resource-bar |
| draining | boolean | false | The disabled state of this card |
| onOpen | () => void | — | Opens the node |

### Variants

| Variant | Meaning |
| --- | --- |
| ok | Line border and a healthy pill |
| warn | Warn-tinted border |
| bad | Bad-tinted border when the node is blocked |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Surface card, line border, name, mono address, health pill, three resource rows, sparkline | Click opens the node |
| hover | Border becomes line2 and the card takes the hover shadow | Pointer cursor |
| focus | 2px accent outline | Tab focuses the card |
| active | Accent-soft background marks the selected node and aria-current is true | Enter opens it |
| disabled | Opacity 0.7, a Draining pill, and no open action. This is the drain state | Not in the tab order |
| loading | Skeleton rows in the card. aria-busy | Does not open |
| error | Bad border, blocked pill, alert icon, and the word Blocked | Stays activatable so an operator can open the incident |

### Keyboard

- Tab focuses the card. Enter or Space opens the node. A draining card is not focused.

### ARIA

- Button named with the node id and health word.
- Resource rows expose their own meter names.
- The selected card sets aria-current="true".

## resource-bar

One labelled meter for CPU, RAM, disk, or network. Export `ResourceBar`. The
layout is a 44px label, a flexible bar, and a 64px mono value. Tone bands match
progress-bar.

Slice: `web/src/shared/ui/resource-bar`.

Gallery: [USER light](mockups/components.html#user-light-resource-bar).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| label | 'CPU' or 'RAM' or 'Disk' or 'Net' or string | — | Short uppercase label |
| value | number | — | 0 to 100 |
| unit | string | % | Printed after the number |

### Variants

| Variant | Meaning |
| --- | --- |
| ok | Under 75 |
| warn | 75 through 89 |
| bad | 90 or more |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Uppercase display label, tone bar, mono value | role progressbar |
| hover | No change | Not a control unless the view makes the row a link |
| focus | 2px accent outline when the row is a button to the metric | Otherwise not tabbable |
| active | Accent-soft row background when that metric is selected | aria-pressed on the row button |
| disabled | Opacity 0.45 | Not updated while the node is paused |
| loading | Indeterminate bar and an em dash for the value. aria-busy | Reduced motion keeps a static partial bar and the word Loading |
| error | Bad bar, bad value, and an alert icon. Example: CPU 96% | aria-valuetext includes the word high |

### Keyboard

- When the row is a button, Tab and Enter select that metric. Otherwise there are no keys.

### ARIA

- role="progressbar" with valuenow, valuemin, and valuemax.
- aria-label is the metric name, such as CPU.
- The printed value is adjacent text.

## tree

A disclosure tree for discovery depth. Export `Tree`. Nodes show a contact or a
depth and can expand to children. Failed nodes keep their label and add a bad
pill.

Slice: `web/src/shared/ui/tree`.

Gallery: [USER light](mockups/components.html#user-light-tree).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| nodes | TreeNode[] | — | Label, id, children, and optional status |
| expanded | string[] | — | Open node ids |
| selected | string | — | The active node |
| onToggle | (id) => void | — | Expand or collapse |

### Variants

| Variant | Meaning |
| --- | --- |
| branch | A node with children and aria-expanded |
| leaf | A node with no disclosure |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | 13.5px rows, a disclosure triangle, a label, an optional count | Click toggles a branch or selects a leaf |
| hover | The row fills sunken | Pointer cursor |
| focus | 2px accent outline on the focused node | Arrow keys move as in a tree |
| active | Accent-soft row, aria-current="true", and aria-expanded reflects the branch | Enter selects. Arrow Right expands. Arrow Left collapses |
| disabled | Opacity 0.45 and aria-disabled. The node does not expand | Arrows skip it |
| loading | Skeleton children under an open node. aria-busy | The parent stays. Reduced motion uses static bars |
| error | A bad pill reading Failed beside the label | The node remains expandable so the failure can be inspected |

### Keyboard

- Arrow Down and Arrow Up move between visible nodes.
- Arrow Right expands a closed branch or moves to the first child. Arrow Left collapses or moves to the parent.
- Enter selects the node. Home and End jump to the ends.

### ARIA

- role="tree", role="treeitem", and aria-expanded="true" or "false" on branches.
- The selected node sets aria-current="true".
- Loading children set aria-busy="true" on the parent item.

## event-feed

A live list of operational events. Export `EventFeed`. Each row has a time, a
source, a severity icon, and a message. A new row plays motion.fresh (1.6s from
accent-soft) unless reduced motion is on.

Slice: `web/src/shared/ui/event-feed`.

Gallery: [USER light](mockups/components.html#user-light-event-feed).

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| events | {time, source, severity, message}[] | — | Newest first |
| paused | boolean | false | The disabled state: the tail stops moving |
| onOpen | (id) => void | — | When a row navigates to the subject |

### Variants

| Variant | Meaning |
| --- | --- |
| live | New rows animate in |
| audit | Adds actor and target columns. Same row treatment |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
| default | Mono time, source, severity icon, message. Dividers use line | A row with onOpen is a button |
| hover | The row fills sunken | Pointer cursor when the row opens |
| focus | 2px accent outline on the focused row | Arrow Up and Arrow Down move between rows |
| active | The newest row holds accent-soft during the fresh animation and sets aria-current when it is the selected event | Reduced motion shows the row immediately with no animation |
| disabled | Opacity 0.7 and a visible Paused label. The tail does not insert rows | Rows can still be read. New events wait |
| loading | Three skeleton rows. aria-busy | No activation |
| error | The failed event uses a bad icon, bad text, and the severity word Error | The row stays in the list |

### Keyboard

- Tab focuses the feed, then Arrow Up and Arrow Down move between rows.
- Enter opens the focused row when it has a destination.
- Page Up and Page Down scroll the feed.

### ARIA

- A section labelled Events, with aria-live="polite" when it is live and not paused.
- Severity is text and an icon, not color alone.
- Loading sets aria-busy="true". Paused removes the live region so stale announcements stop.
