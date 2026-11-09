# XFlickr web

React 19 + TypeScript UI for XFlickr. English is the default language; Vietnamese
is available from the language switcher. See [`docs/i18n.md`](../docs/i18n.md)
for catalogs, glossary rules, and CI guards.

## Commands

```sh
npm ci
npm run lint          # check-i18n + ESLint + Prettier
npm run build
npm test              # Vitest with coverage
npm run e2e:mocked    # Playwright against mocked APIs
npm run e2e           # real-stack suite (see docs/e2e.md)
```

## Layout

`src/app`, `src/pages`, `src/features`, `src/entities`, `src/shared` (FSD-style).
API client code is generated into `src/shared/api/gen`.
