# UI view catalogue

This catalogue is the single index of every view in XFlickr. The gate
`docker/scripts/ui-spec-check.sh [slug]` (run by `make lint` and the CI lint
job) reads it, so a view cannot exist without a spec, a mockup and an E2E hook.

Reference baseline: [proof of concept](poc/README.md). Shared rules:
[design system](design-system.md), [components](components.md),
[patterns](patterns.md), [accessibility](a11y.md), [i18n](i18n.md).

## Conventions

- View id: the slug, also the spec file name `views/<id>.md`, the mockup
  `mockups/views/<id>.html` and the E2E spec `web/e2e/<id>.spec.ts`.
- A spec has the headings `Purpose`, `Layout`, `Data`, `Interactions`,
  `States`, `Accessibility` and `Test hooks` (level 2), the states `Loading`,
  `Empty`, `Error` and `Ready` as level 3 headings under `States`, and at least
  one `data-testid` in `Test hooks`.
- A mockup has one element per state: `data-state="loading"`, `"empty"`,
  `"error"` and `"ready"`.
- A spec or mockup cell is a link to an existing file, or the word `pending`
  while the owning ticket has not delivered it. `pending` is tolerated by the
  default run and rejected by `ui-spec-check.sh --strict` and by
  `ui-spec-check.sh <slug>`. A cell marked `pending` whose file already exists
  fails, so the marker is removed as soon as the file lands.
- Every file in `views/` must be listed here.

## Views

| id | role | route | title | spec | mockup | build ticket | E2E spec |
|---|---|---|---|---|---|---|---|
| `user-shell` | user | all user routes | App shell, navigation and global states | [spec](views/user-shell.md) | [mockup](mockups/user-shell.html) | JOOXF-79, JOOXF-166 | `web/e2e/user-shell.spec.ts` |
| `user-home` | user | `/` | Home | [spec](views/user-home.md) | [mockup](mockups/user-home.html) | JOOXF-169 | `web/e2e/user-home.spec.ts` |
| `user-library` | user | `/library` | Library | pending | pending | TBD | `web/e2e/user-library.spec.ts` |
| `user-search` | user | `/search` | Search | pending | pending | TBD | `web/e2e/user-search.spec.ts` |
| `user-contacts` | user | `/contacts` | Contacts | [spec](views/user-contacts.md) | [mockup](mockups/user-contacts.html) | JOOXF-173 | `web/e2e/user-contacts.spec.ts` |
| `user-contact-photos` | user | `/contacts/:nsid?tab=photos` | Contact: Header and Photos tab | [spec](views/user-contact-photos.md) | [mockup](mockups/user-contact-photos.html) | JOOXF-173 | `web/e2e/user-contact-photos.spec.ts` |
| `user-contact-favorites` | user | `/contacts/:nsid?tab=favorites` | Contact: Favorites tab | [spec](views/user-contact-favorites.md) | [mockup](mockups/user-contact-favorites.html) | JOOXF-173 | `web/e2e/user-contact-favorites.spec.ts` |
| `user-contact-albums-galleries` | user | `/contacts/:nsid?tab=albums` or `?tab=galleries` | Contact: Albums and Galleries tabs | [spec](views/user-contact-albums-galleries.md) | [mockup](mockups/user-contact-albums-galleries.html) | JOOXF-173, JOOXF-174 | `web/e2e/user-contact-albums-galleries.spec.ts` |
| `user-contact-groups-about` | user | `/contacts/:nsid?tab=groups` or `?tab=about` | Contact: Groups and About tabs | [spec](views/user-contact-groups-about.md) | [mockup](mockups/user-contact-groups-about.html) | JOOXF-173 | `web/e2e/user-contact-groups-about.spec.ts` |
| `user-albums` | user | `/albums` | Albums | [spec](views/user-albums.md) | [mockup](mockups/user-albums.html) | JOOXF-174 | `web/e2e/user-albums.spec.ts` |
| `user-album` | user | `/albums/:id` | Album | pending | pending | TBD | `web/e2e/user-album.spec.ts` |
| `user-crawl` | user | `/crawl` | Crawl | [spec](views/user-crawl.md) | [mockup](mockups/user-crawl.html) | JOOXF-175 | `web/e2e/user-crawl.spec.ts` |
| `user-keys` | user | `/keys` | Keys and quota | [spec](views/user-keys.md) | [mockup](mockups/user-keys.html) | JOOXF-176, JOOXF-167 | `web/e2e/user-keys.spec.ts` |
| `user-downloads` | user | `/downloads` | Downloads | [spec](views/user-downloads.md) | [mockup](mockups/user-downloads.html) | JOOXF-177 | `web/e2e/user-downloads.spec.ts` |
| `user-sync` | user | `/sync` | Cloud sync | pending | pending | TBD | `web/e2e/user-sync.spec.ts` |
| `user-sharing` | user | `/sharing` | Sharing | pending | pending | TBD | `web/e2e/user-sharing.spec.ts` |
| `user-wizard` | user | `/onboarding` | Setup wizard: API key, Flickr connection, more accounts | [spec](views/user-wizard.md) | [mockup](mockups/user-wizard.html) | JOOXF-167 | `web/e2e/user-wizard.spec.ts` |
| `user-profile` | user | `/profile` | Profile | [spec](views/user-profile.md) | [mockup](mockups/user-profile.html) | JOOXF-166 | `web/e2e/user-profile.spec.ts` |
| `admin-shell` | admin | `/admin/*` (layout) | Admin shell, rail and navigation | [spec](views/admin-shell.md) | [mockup](mockups/admin-shell.html) | JOOXF-79 | `web/e2e/admin-shell.spec.ts` |
| `admin-overview` | admin | `/admin` | Overview | pending | pending | TBD | `web/e2e/admin-overview.spec.ts` |
| `admin-nodes` | admin | `/admin/nodes` | Cluster nodes | pending | pending | TBD | `web/e2e/admin-nodes.spec.ts` |
| `admin-node` | admin | `/admin/nodes/:id` | Cluster node | pending | pending | TBD | `web/e2e/admin-node.spec.ts` |
| `admin-quota` | admin | `/admin/quota` | API quota | pending | pending | TBD | `web/e2e/admin-quota.spec.ts` |
| `admin-metrics` | admin | `/admin/metrics` | Metrics | pending | pending | TBD | `web/e2e/admin-metrics.spec.ts` |
| `admin-events` | admin | `/admin/events` | Events and audit | pending | pending | TBD | `web/e2e/admin-events.spec.ts` |
| `admin-issues` | admin | `/admin/issues` | Issues | pending | pending | TBD | `web/e2e/admin-issues.spec.ts` |
| `admin-pipeline` | admin | `/admin/pipeline` | Crawl pipeline | pending | pending | TBD | `web/e2e/admin-pipeline.spec.ts` |
| `admin-downloads` | admin | `/admin/downloads` | Downloads | pending | pending | TBD | `web/e2e/admin-downloads.spec.ts` |
| `admin-sync` | admin | `/admin/sync` | Cloud sync | pending | pending | TBD | `web/e2e/admin-sync.spec.ts` |
| `admin-ai` | admin | `/admin/ai` | AI index | pending | pending | TBD | `web/e2e/admin-ai.spec.ts` |
| `admin-users` | admin | `/admin/users` | Users | pending | pending | TBD | `web/e2e/admin-users.spec.ts` |
| `admin-defaults` | admin | `/admin/defaults` | Defaults | pending | pending | TBD | `web/e2e/admin-defaults.spec.ts` |
| `admin-sharing` | admin | `/admin/sharing` | Sharing | pending | pending | TBD | `web/e2e/admin-sharing.spec.ts` |
| `admin-log` | admin | `/admin/log` | API call log | pending | pending | TBD | `web/e2e/admin-log.spec.ts` |
| `user-auth` | user | `/login`, `/register`, `/login?reason=expired` | Login, register and session expired | [spec](views/user-auth.md) | [mockup](mockups/user-auth.html) | JOOXF-166 | `web/e2e/user-auth.spec.ts` |
| `public-setup` | public | `/setup` | Setup wizard | pending | pending | TBD | `web/e2e/public-setup.spec.ts` |
| `public-not-found` | public | `*` | Page not found | pending | pending | TBD | `web/e2e/public-not-found.spec.ts` |
| `public-forbidden` | public | `/forbidden` | Access denied | pending | pending | TBD | `web/e2e/public-forbidden.spec.ts` |
| `user-accounts` | user | `/accounts` | Flickr accounts | pending | pending | TBD | `web/e2e/user-accounts.spec.ts` |
| `user-account-connect` | user | `/accounts/connect` | Connect Flickr account | pending | pending | TBD | `web/e2e/user-account-connect.spec.ts` |
| `user-sessions` | user | `/sessions` | Sessions | pending | pending | TBD | `web/e2e/user-sessions.spec.ts` |

## Overlays

Overlays are not routes. They are specified with the components they use and
are covered by the E2E spec of the view that opens them.

- Lightbox (photo viewer)
- Modals: add-key, move-key, new-download
- Drawers: crawl-job, download-job, upload-process
- Notifications panel
- Tray (live activity)
