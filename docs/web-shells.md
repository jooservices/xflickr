# Web app shells, tokens and i18n

The web app has two shells that wrap every page. Their design is specified in
[user-shell](ui/views/user-shell.md) and [admin-shell](ui/views/admin-shell.md).

## Code map

| Path | Role |
| --- | --- |
| `web/src/app/router.tsx` | Route table: `/` (user shell), `/admin` (guarded admin shell), `/login` stand-in. Every nav item routes to `PlaceholderPage`. |
| `web/src/app/guards.tsx` | `AdminGuard` reads the session role; only `admin` renders children. Others get the 403 page, a signed-out visitor goes to `/login?next=`. |
| `web/src/app/shells/` | `UserShell`, `AdminShell`, controls, overlays (avatar menu, tray, notifications, dialogs), banners, maintenance probe, cluster footer. |
| `web/src/entities/session/` | `GET /me` once; `useSession()` returns `loading`, `unauthenticated` (401), `error` or `authenticated` with the role. |
| `web/src/shared/ui/tokens.css` | Colors, spacing, type, elevation and motion as CSS custom properties for `data-role` (`user`, `admin`) and `data-theme` (`light`, `dark`). Values follow [design-system](ui/design-system.md). |
| `web/src/shared/ui/states/` | Shared loading, empty, error, 403 and 404 components, `PageState`, and the development-only `StatePreview` control. |
| `web/src/shared/i18n/` | `en.json` and `vi.json` (flat, dotted keys), ICU plural formatter, `I18nProvider` and `useI18n`. |
| `web/src/shared/theme/` | Theme per role: stored choice, else system (user) or dark (admin). |

## Behaviour to know

- Language and theme are kept in `localStorage` (`xf.locale`, `xf.theme.<role>`)
  until the profile stores them (`PATCH /me` is a contract request).
- An admin who opens the app on a user route is sent to `/admin` once per page
  load; the `User`/`Admin` switch then works both ways.
- `/shell/summary`, `/tray`, `/notifications`, `/admin/shell` are contract
  requests that are not in `contracts/openapi` yet. The shells use the shapes
  from the specs and treat a failure as a partial state (hidden badges, inline
  error with Retry). SSE is not connected; the admin footer polls every 10 s.
- The Preview control (`shell-preview-<state>`, `admin-preview-<state>`) is
  rendered only when `import.meta.env.DEV` is true.

## Tests

`npm test` (Vitest, coverage gate 85 %) and `npm run e2e` (Playwright with
axe; the specs mock the API with `page.route`, see `web/e2e/support.ts`).
