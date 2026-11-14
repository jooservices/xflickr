# Browser E2E on the real stack

The earlier browser specs mocked every API route, so a screen could pass while the backend did not serve the
endpoint it called. `web/e2e` now drives headless Chrome against the real backend, the real PostgreSQL/Redis
worktree database and `fakeflickr` as the Flickr upstream. Nothing in it mocks a route.
(The mocked specs moved to `web/e2e-mocked`; `npm run e2e:mocked` runs them. They check UI states only.
`language-switch.spec.ts` switches to Vietnamese, walks login/register/keys/crawl/profile, and switches back.)

## Three layers

Every E2E test is written in three layers under `web/e2e`; `node scripts/check-e2e-layers.mjs` (part of
`npm run lint`) fails when a file breaks them.

| Layer    | Directory    | What it is                                                                             | May call              |
| -------- | ------------ | -------------------------------------------------------------------------------------- | --------------------- |
| Step     | `steps/`     | One small action or one check on the page (open a page, fill a field, expect a banner) | the page, `support/`  |
| Suite    | `suites/`    | Steps combined into one feature (sign in, add a key, revoke sessions)                  | steps, `support/`     |
| Scenario | `scenarios/` | One complete test (`test(...)`) that calls suites                                      | suites, `support/env` |

`support/` is infrastructure without a layer: the environment (`env.ts`), data factories that seed and query the
worktree database (`factories.ts`, `worktree.ts`, `catalog.ts`) and the route reader (`routes.ts`). A scenario or a
suite never touches the page directly; it calls a step. Playwright runs `scenarios/` in file order, so the numbered
files build on each other's state (first user, keys, account).

Third parties are mocked and the project's own parts run for real: `fakeflickr` stands in for Flickr (its
authorize endpoint always succeeds) and the data comes from factories and the crawl of `fakeflickr`'s dataset. See
the `dev-check` skill for the rules every project follows.

The mocked sign-in test in `web/e2e-mocked` reuses the `signInAs` suite; it proves UI composition with fake API
responses and does not replace real-stack evidence.

### Data the scenarios seed

Every scenario runs against the real backend. The library scenario seeds 501 favorites of the connected account, the
admin defaults scenario seeds a plan and changes a member's role for one visit, and the admin nodes scenario relabels
the existing key; each one puts the database back (`support/factories.ts`). Only `fakeflickr` stands in for a third
party.

### First-slice status and deferred work (JOOXF-187)

This slice partially delivers ticket AC1–AC2: typed scenario execution and
composition of the existing sign-in step. Additional steps (add key, connect
account, browse, search, request/fetch download and admin actions), their tests,
and composition of the remaining journeys are deferred. Individual product
scenarios belong to JOOXF-188–190.

Ticket AC3–AC4 remain deferred: a verified shared-services/worktree-database
harness with fake Flickr and the browser on vm-tools, failure reports and
screenshots, every-route scenarios with zero console errors, and CI integration.
The existing runner below resets the schema with `DROP SCHEMA`, uses Docker
PostgreSQL, and describes a host browser. It is unchanged and must not be run
until its worktree safety and vm-tools execution have been verified. The run
instructions below describe that existing harness, not a verified safe command
for this slice. `docs/ENVIRONMENT.md` takes precedence: shared services and the
worktree database, without Compose or testcontainers.

## Run it

```sh
make e2e                       # build, start the stack, run everything, exit non-zero on any failure
E2E_SKIP_BUILD=1 make e2e      # reuse the last build while iterating on specs
./docker/scripts/e2e.sh e2e/04 # extra arguments go to `playwright test` (a file prefix or -g "title")
E2E_HOLD=1 ./docker/scripts/e2e.sh   # start the stack and keep it up for manual debugging
```

`docker/scripts/e2e.sh` needs the generated worktree `.env` and the shared services (`make dev-up`). It:

1. builds `xflickr` and `fakeflickr` through the shared Go runtime and the web bundle through the Node runtime;
2. resets the worktree database (`DROP SCHEMA public`, then `migrate up`; through the shared PostgreSQL container when it exists, otherwise with the host `psql` on 127.0.0.1:15435); it refuses any database whose name
   does not start with `xflickr_wt`, so the live `xflickr` database is never touched;
3. starts `fakeflickr` and the app (roles `api,scheduler,crawl,media`; the media role consumes the sizes-resolved event that moves waiting downloads on) on free local ports. The Redis prefix is unique
   per run, the master key is random, and `FLICKR_UPSTREAM_URL` points the app at `fakeflickr`;
4. runs Playwright with the browser on the workspace E2E server (`E2E_HOST` in the Workspaces `.env`, through `scripts/dev/e2e-browser`; the app and fakeflickr are reached by reverse tunnels). Outside a Workspaces checkout it uses the local Google Chrome (`docker/scripts/playwright.sh`). Playwright never runs in Docker.

The first registered user is the admin. The stack hands the specs `E2E_BASE_URL`, `E2E_FAKEFLICKR_URL`,
`E2E_ADMIN_EMAIL` and `E2E_ADMIN_PASSWORD`. The sign-in form allows five attempts per quarter hour, so
`01-auth` saves the admin session to `web/.e2e-state/` and the other specs reuse it (`ADMIN_STATE`).

## What runs

| File                        | Covers                                                                                                                                                                                                                                                                                                                                                |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `00-monitor.spec.ts`        | Seeded failures: the monitor must fail on HTML-instead-of-JSON, error statuses, console errors, uncaught exceptions and failed requests.                                                                                                                                                                                                              |
| `01-auth.spec.ts`           | Register the first user, sign out and in, wrong password (clear error, email kept), session expiry.                                                                                                                                                                                                                                                   |
| `02-keys.spec.ts`           | Empty state, an invalid key (fakeflickr answers error 100; the dialog stays open with the input kept), a valid key and its usage.                                                                                                                                                                                                                     |
| `03-flickr-account.spec.ts` | Connect through fakeflickr OAuth: authorize, callback, the account is shown.                                                                                                                                                                                                                                                                          |
| `04-crawl.spec.ts`          | Confirms the connected account and completed crawl work appear in Crawl status, changes depth (survives a reload), and verifies pause/resume responses.                                                                                                                                                                                               |
| `05-profile.spec.ts`        | Profile display, revoke one session, sign out other sessions, change and restore the password, and update and restore sharing through the REST resource.                                                                                                                                                                                              |
| `06-photo-sizes.spec.ts`    | A provisional photo remains unconfirmed until an explicit download request confirms sizes. The catalogue fixture adds an observation for the connected account in the guarded worktree database so the normal visibility policy applies. This verifies confirmation, not ZIP completion, and uses the VM/Ubuntu PostgreSQL client at `/usr/bin/psql`. |
| `06-deleted-entity.spec.ts` | Missing-person crawl failure persists one gone marker; reopening work completes safely and Crawl remains usable. Uses the guarded native worktree SQL fixture helper.                                                                                                                                                                                 |
| `99-route-sweep.spec.ts`    | Every route at desktop and 390 px; sign-in, keys and crawl in light and dark at both widths (no sideways scroll, no overlapping controls).                                                                                                                                                                                                            |

The Crawl screen has no start, pause or resume control yet, and the contract has no start endpoint. Connecting an
account starts its crawl; the journey checks the account at depth 0 and completed work in the real crawl status.
Contact pause and resume are covered through the existing `PUT /me/crawl/pause|resume` contract. When screen
controls are delivered, move these actions into `04-crawl.spec.ts` through the UI.

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

## View coverage

`make e2e` first runs `docker/scripts/ui-e2e-coverage.sh`, which maps
every catalogue view ([ui/README.md](ui/README.md)) to its real-stack
scenario in [ui/e2e-coverage.md](ui/e2e-coverage.md). The checker is
a ratchet over the baseline
([ui/e2e-coverage-baseline.txt](ui/e2e-coverage-baseline.txt), one
view slug per line): missing views listed there are reported as
`known gap` and do not fail, while a missing view not in the baseline
(a new catalogue view, or a view that lost its scenario) fails. When
you add the scenario for a baseline view, swap `MISSING` for its real
file in the coverage map and delete its baseline line; the checker
fails with `Baseline view now covered: <slug> (delete its baseline
line)` until you do, so the baseline can only shrink. A baseline
slug that is not in the catalogue also fails as a stale line. Today
the baseline lists 40 known gaps. Run `UI_E2E_COVERAGE=warn make e2e`
to print the uncovered list without failing. A new catalogue view
adds its row first; a new scenario swaps `MISSING` for its real
file.

## Deploy smoke

`docker/scripts/develop-smoke.sh` (run after every `make develop-up`) ends with the signed-in route sweep against the
deployed container, so a deploy with a broken screen fails. Set `XFLICKR_SMOKE_EMAIL` and
`XFLICKR_SMOKE_PASSWORD` to an existing admin of the develop app before running the smoke; it fails loudly when
either value is missing. The sweep only reads; it creates nothing in the live database.
