# Browser E2E on the real stack

The earlier browser specs mocked every API route, so a screen could pass while the backend did not serve the
endpoint it called. `web/e2e` now drives headless Chrome against the real backend, the real PostgreSQL/Redis
worktree database and `fakeflickr` as the Flickr upstream. Nothing in it mocks a route.
(The mocked specs moved to `web/e2e-mocked`; `npm run e2e:mocked` runs them. They check UI states only.)

## Run it

```sh
make e2e                       # build, start the stack, run everything, exit non-zero on any failure
E2E_SKIP_BUILD=1 make e2e      # reuse the last build while iterating on specs
./docker/scripts/e2e.sh e2e/04 # extra arguments go to `playwright test` (a file prefix or -g "title")
E2E_HOLD=1 ./docker/scripts/e2e.sh   # start the stack and keep it up for manual debugging
```

`docker/scripts/e2e.sh` needs the generated worktree `.env` and the shared services (`make dev-up`). It:

1. builds `xflickr` and `fakeflickr` through the shared Go runtime and the web bundle through the Node runtime;
2. resets the worktree database (`DROP SCHEMA public`, then `migrate up`); it refuses any database whose name
   does not start with `xflickr_wt`, so the live `xflickr` database is never touched;
3. starts `fakeflickr` and the app (roles `api,scheduler,crawl`) on free local ports. The Redis prefix is unique
   per run, the master key is random, and `FLICKR_UPSTREAM_URL` points the app at `fakeflickr`;
4. runs Playwright on the host with Google Chrome (Playwright never runs in Docker; `docker/scripts/playwright.sh`).

The first registered user is the admin. The stack hands the specs `E2E_BASE_URL`, `E2E_FAKEFLICKR_URL`,
`E2E_ADMIN_EMAIL` and `E2E_ADMIN_PASSWORD`. The sign-in form allows five attempts per quarter hour, so
`01-auth` saves the admin session to `web/.e2e-state/` and the other specs reuse it (`ADMIN_STATE`).

## What runs

| File | Covers |
|---|---|
| `00-monitor.spec.ts` | Seeded failures: the monitor must fail on HTML-instead-of-JSON, error statuses, console errors, uncaught exceptions and failed requests. |
| `01-auth.spec.ts` | Register the first user, sign out and in, wrong password (clear error, email kept), session expiry. |
| `02-keys.spec.ts` | Empty state, an invalid key (fakeflickr answers error 100; the dialog stays open with the input kept), a valid key and its usage. |
| `03-flickr-account.spec.ts` | Connect through fakeflickr OAuth: authorize, callback, the account is shown. |
| `04-crawl.spec.ts` | Progress by depth, the connected account counted, change depth (survives a reload), pause and resume. |
| `05-profile.spec.ts` | The profile page. |
| `99-route-sweep.spec.ts` | Every route at desktop and 390 px; sign-in, keys and crawl in light and dark at both widths (no sideways scroll, no overlapping controls). |

The Crawl screen has no start, pause or resume control yet, and the contract has no start endpoint: the crawl
starts when the account is connected, and pause/resume are exercised through `PUT /me/crawl/pause|resume`.
When the controls land, add their clicks to `04-crawl.spec.ts`.

## The monitor

`support/monitor.ts` watches a page and collects: uncaught exceptions, `console.error`, failed requests,
responses with status >= 400, fetch/XHR responses served as `text/html`, and the router error page
(`global-error`, "Unexpected Application Error"). Every journey ends with `watch.expectClean(screen)`.

A response that must be tolerated goes in `support/allowlist.json` with a reason; only 401 and 404 are accepted
there, for a screen of a feature that is not built yet. A failure a journey provokes on purpose (a rejected key)
is declared in the test with `watch.expectFailure(method, path, status)`.

## The route sweep

`support/routes.ts` reads the routes from `src/app/router.tsx` and `src/app/shells/nav.ts` (Node cannot import
the router: it pulls in CSS and `import.meta.env`), so a new route is swept without touching the tests.
`:param` segments get the sample value `1`, and the catch-all `*` is visited at `/__e2e-unknown` and must show
the not-found screen. Each route is visited as the signed-in admin; the sweep waits for network idle and fails on
anything the monitor collects. The `/login` and `/register` screens are visited anonymously.

## Add a route or a journey

- **A route:** add it to the router (or the nav table). The sweep picks it up. If the screen calls an endpoint the
  backend does not serve yet, the sweep fails; build the screen against `contracts/openapi`, or list the 401/404 in
  `allowlist.json` with the ticket that removes it.
- **A journey:** add `NN-name.spec.ts` in `web/e2e`. Files run in name order on one worker and share the database,
  so a journey may rely on the state the earlier ones left (the admin, a key, a connected account). Use
  `test.use({ storageState: ADMIN_STATE })`, `monitor(page)` and `expectNoErrorPage`. Use `setFlickrScenario` to
  change what `fakeflickr` answers (see [fake-flickr.md](fake-flickr.md)).

## Deploy smoke

`docker/scripts/develop-smoke.sh` (run after every `make develop-up`) ends with the route sweep against the
deployed container, so a deploy with a broken screen fails. Set `XFLICKR_SMOKE_EMAIL` and
`XFLICKR_SMOKE_PASSWORD` (an existing admin of the develop app) for the signed-in sweep of every screen. Without
them it sweeps as a visitor: the public screens, and the other routes with their `401` answers tolerated. The
sweep only reads; it creates nothing in the live database.
