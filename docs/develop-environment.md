# Develop environment

The develop runtime runs XFlickr as one Docker container on the Workspaces WSL
host. PostgreSQL, Redis, and Elasticsearch remain on the shared
`workspaces-shared` network. The application uses the live `xflickr` database
and Redis database 7; worktree and test environments continue to use their own
generated `.env` files.

## Start and access

Run `make develop-up` from the XFlickr checkout. The helper creates the ignored
`.env.develop` with `scripts/docker/shared env-project xflickr --mode live`,
checks that it targets the live project, and adds the application's encryption
key on first use. Keep this file private and do not replace or delete it while
XFlickr contains encrypted credentials.

The web application and API are available at `http://192.168.2.10:18490` on the
LAN and `http://127.0.0.1:18490` on the workstation. The API container applies
pending migrations before starting its configured roles. A migration failure
ends container startup.

Use `make develop-status` to inspect container health and `make develop-logs`
to follow application logs. `docker/scripts/develop-smoke.sh` checks container
health, `/healthz`, applied migrations, and the index and registration pages.

## Update and stop

After changing the checkout, run `make develop-up` again. It rebuilds the image
and recreates the service with the updated binary and web build. Run
`make develop-down` to stop and remove the container while retaining the named
blob data volume and `.env.develop`. It does not stop or alter shared services.

There is no destructive reset command. To start the app again, use
`make develop-up`; its data and encryption key remain available.

## Optional Ollama worker

Ollama runs natively on the WSL host. To add the AI role, edit the ignored
`.env.develop`: append `,ai-worker` to `XFLICKR_ROLES` and set `OLLAMA_URL` to
`http://host.docker.internal:11434` (or the configured host endpoint). The
default role list does not start the AI worker.
