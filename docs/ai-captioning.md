# AI captioning

The AI application exposes a `Captioner` port. A caption contains concise text,
the detected object names, the configured model name, and the model's
`modified_at` value returned by Ollama's `/api/show` endpoint. An empty object
list is valid; missing text or model provenance is not.

The Ollama adapter sends the image as base64 to `/api/chat` with a fixed prompt,
JSON schema output, and temperature zero. It rejects malformed, incomplete, or
unexpected response fields and retries invalid caption JSON once. Provider and
transport errors are returned to the caller.

The adapter constructor receives the Ollama URL, vision model, request timeout,
and concurrency limit from its composition settings. The timeout covers both
waiting for a concurrency slot and the provider requests. The circuit breaker
opens after five consecutive provider failures and permits one probe after a
60-second cooldown. A successful request closes it. Cancellation by the caller
does not count as a provider failure.

Adapter tests use a local fake HTTP server and do not contact a running Ollama
service.
