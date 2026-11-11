# AI embedding

The AI application exposes an `Embedder` port that accepts one batch of 1–16
non-empty texts. The Ollama adapter returns one 1,024-dimensional vector per
input, in input order, with the model name and version used for inference.

The first adapter slice uses the fixed `bge-m3` model. For each call it reads
`modified_at` from `/api/show`, then sends the original input strings to
`/api/embed` with truncation disabled. Inputs beyond the model context window
return a non-retryable invalid-text error instead of an embedding for truncated
text. These input errors do not count against the shared Ollama circuit breaker,
so oversized embedding inputs do not interrupt caption requests. Other provider
failures, malformed responses, and incorrect vector counts or dimensions return
an error without partial results.

Embedding and caption requests on the same Ollama client share its timeout,
concurrency semaphore, and circuit breaker. Caller cancellation does not count
as a provider failure. Adapter tests use a local fake HTTP server and do not
contact Ollama.

This slice defines the port and provider adapter only. It does not add automatic
batch splitting, model settings, role wiring, workers, or UI integration.
