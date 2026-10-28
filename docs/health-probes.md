# Health and readiness probes

XFlickr exposes probes on the listener configured by OBS_HTTP_PORT (default 8081). The listener is shared by all roles selected in one process, so a multi-role process binds it once. The API's normal listener also serves the same endpoints.

- GET /healthz is liveness. It returns HTTP 200 with the process version while the HTTP server is responding. It does not depend on database or storage availability.
- GET /readyz is readiness. It reports every selected role's required dependencies as ok or error; it returns HTTP 503 while any role is still starting, has exited, or has an unavailable required dependency. Dependency details are deliberately omitted from the response.

Readiness checks use bounded requests to PostgreSQL and Redis. Roles that use blob storage also perform a metadata lookup for a reserved probe key; a missing key means the storage service is reachable. Flickr is not a readiness dependency because the application retries upstream requests. The indexer, node-agent, and AI worker roles currently have no external startup dependencies and report process readiness once their role starts.

The probe listener should be reachable by container orchestration within the application network. Keep OBS_HTTP_PORT separate from HTTP_PORT when the API role runs in the same process.
