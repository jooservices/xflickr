# Admin AI index

## Purpose

Give ADMIN a fleet-wide, aggregate view of captioning, embedding, model, worker and search-index health. Follow the approved POC while making degraded search behavior and model-version changes understandable. This view does not expose photo content or edit model configuration.

## Route and access

- Route `/admin/ai` inside the ADMIN shell; dark control-room theme by default and the existing light theme as an option.
- Require an authenticated ADMIN session before loading. A 401 follows shell sign-in. A 403 clears cached ADMIN data and displays the shared forbidden state.
- The shell owns navigation, theme, language and global live controls. This view owns Refresh, the model/queue/index panels, the re-enrichment confirmation and freshness.

## Layout

- Header: “AI index”, Live/Stale timestamp and Refresh.
- Summary cards: caption backlog and completed captions, captions/minute, embedding backlog and completed embeddings, embeddings/second, and indexed documents.
- Model cards show the configured caption and embedding model names and versions, loaded/offline state, device, memory, worker concurrency and measured speed. POC model names are examples only; render configured values from the API.
- Worker panel shows state, active/max workers, in-flight work, oldest queued age and last heartbeat.
- Index panel shows health, document count, size, shards, metadata/caption coverage, refresh interval, query p95 and current search mode.
- Re-enrichment panel identifies stale items by enrichment kind and model version. “Review queue” opens a confirmation with kind, target version, eligible count and capacity note; confirmation submits the bounded server operation.
- Offline preview shows the degraded state: when Ollama cannot produce caption or query embeddings, new enrichment waits and search falls back to keyword-only. Existing captions, vectors and indexed records remain intact.
- Do not show photo IDs, titles, thumbnails, captions, prompts, raw model/provider responses, credentials, Ollama URLs, arbitrary worker payloads or user-level data.

## Data

The dashboard is aggregate and ADMIN-only. Display UTC observation time and freshness with each panel. Throughput uses the trailing five minutes; queue, model and index snapshots use their own `observed_at`. An unknown, stale or zero-denominator value is rendered “—”, never as zero or healthy. The mockup's model names and counts are synthetic examples.

## Data and API

All paths are relative to `/api/v1`. **Contract request** means JOOXF-185 must wait for the API owner to add an ADMIN-authorized OpenAPI contract; this design ticket makes no OpenAPI or application changes.

| Content | Source, fields and action |
| --- | --- |
| Existing operational status | ADMIN-only `GET /admin/ops/status` exists, but is not the view contract for model versions, AI queue counts, enrichment throughput or index coverage. Do not infer missing values from generic process health. |
| AI summary | **Contract request:** ADMIN-only `GET /admin/ai/status`; `generated_at, window_start, window_end, caption_backlog, captions_completed, captions_per_minute, embedding_backlog, embeddings_completed, embeddings_per_second, worker{state,active_workers,max_workers,in_flight,oldest_queued_seconds,last_heartbeat_at}, models[]`, and `index{state,document_count,size_bytes,shard_count,metadata_coverage_percent,caption_coverage_percent,refresh_interval,query_p95_ms,search_mode,observed_at}`. Models include `kind, name, model_version, version_observed_at, load_state, device, memory_bytes, concurrency, throughput, throughput_unit, observed_at`. Use the endpoint for cards and model/index panels; Refresh re-fetches it. |
| Stale enrichment counts | **Contract request:** include `stale_enrichment[]: {kind, model_name, model_version, eligible_count}` in the summary. The action opens confirmation only for a loaded target model/version with eligible work. |
| Re-enrichment | **Contract request:** ADMIN-only `POST /admin/ai/re-enrichment`, with `Idempotency-Key` and body `{kind, model_name, model_version}`; response `{operation_id, status, accepted_count, skipped_count, submitted_at}`. It queues bounded batches for assets missing the selected current version; it never deletes existing captions/vectors or bypasses queue capacity. Reject stale model versions, duplicate active operations or insufficient capacity with stable error codes. The UI confirms once and displays the accepted result; it does not start workers. |
| Re-enrichment progress | **Contract request:** add operation progress to the ADMIN AI event stream: `operation_id, kind, status, processed_count, total_count, failed_count, observed_at`. Status is `queued, running, completed, failed`. |
| Existing events | Authenticated `GET /events` exists and is USER-scoped. It is not a source for fleet-wide ADMIN AI updates. |
| ADMIN live events | **Contract request:** ADMIN-only `GET /admin/ai/events`, resumable with `Last-Event-ID`; event types `ai.queue.progress`, `ai.model.state`, `ai.worker.state`, `ai.index.state`, `ai.search.mode` and `ai.re-enrichment.progress`. Each event carries its observation time and only aggregate fields. |
| Offline behavior | No production endpoint simulates failure. A test/MSW scenario supplies the offline fixture; production reads the worker/model health reported by the server. Map `ollama_unavailable`, `model_load_failed`, `queue_capacity_reached` and `re_enrichment_already_running` to localized copy. Never render server messages or provider bodies. |

The backend owns Ollama and search-provider calls. Do not call Ollama from the browser or assume OpenSearch: the repository environment routes search through the SearchIndex port with Elasticsearch and pgvector implementations.

## Actions

- Refresh reloads the authorized summary and re-enrichment progress while preserving the selected language and panel state.
- Selecting “Review queue” shows enrichment kind, model/version, eligible count and the fact that existing results remain available. “Confirm enqueue” requires explicit confirmation and an idempotency key. Disable it when the model is unloaded, the count is zero/unknown, an operation is active or capacity is insufficient.
- After acceptance, show operation reference and progress; offer no cancel, clear-index, delete-vector, worker restart, offline toggle or model-configuration control.
- “Simulate Ollama offline” exists only as a local mock/MSW scenario to review the degraded UI. It must never call an endpoint or change a real service.

## Interactions

A model-version change marks eligible records stale and exposes the re-enrichment review. A successful enqueue does not replace prior results until a newer enrichment succeeds. When Ollama is unavailable, caption/embedding queues wait and new semantic query embeddings are skipped; retain keyword search and all existing indexed content. No user or photo identity is needed to operate this view.

## States

### Ready

Show current aggregate metrics, model versions, worker state and index health. Show “Keyword-only” when query embeddings are unavailable. A healthy model does not imply an empty backlog is an error.

### Loading

Show labeled skeletons for cards and panels with `aria-busy="true"`. During refresh keep last-good values and identify the panel as updating.

### Empty

If no eligible enrichment or queued work exists, show the successful zero-work state and explain that new records will appear as crawl/search work arrives. Hide the enqueue action when no items are stale.

### Error

A failed initial summary shows localized copy and Retry. A panel error leaves other successful panels usable. A failed re-enrichment request keeps existing data and provides a safe retry only after refreshing operation state.

### Partial and stale

Keep successful panels visible if another panel or SSE fails. Mark only affected data “Stale · updated {time}”, retain last-good values, and offer Refresh. Never present an old model/index snapshot as healthy.

### Forbidden

For 403, clear the response and operation data, close confirmation and show the shared forbidden state without exposing queue or model details.

### Ollama offline

Show the captioner/embedding model and worker as unavailable, pause new AI work, preserve all completed assets, and label search “Keyword-only”. The preview is a local test fixture; no production toggle is present.

## Live updates

Fetch an authorized snapshot before opening the ADMIN AI stream. Show Live only after SSE connects and resume with `Last-Event-ID`. Apply events by ID; refetch a panel after malformed or out-of-order events. If SSE is unavailable, poll visible panels and an active re-enrichment operation every 10 seconds, backing off to 30 seconds on errors; pause in hidden tabs and refresh on return. Mark data stale after 30 seconds without an observation. Never use USER `GET /events` for fleet data.

## Responsive

At 1440px show summary cards, two model cards, worker/index panels and the re-enrichment panel in a dense control-room grid. At 390px stack panels and cards, keep progress labels with values, and make confirmation full-screen. No page-level horizontal scrolling.

## Accessibility

Use one h1, semantic headings, labeled controls, keyboard-operable confirmation and visible focus. Return focus to “Review queue” when the dialog closes. Announce model/worker state and operation completion politely, not every throughput tick. Set `aria-busy` during requests; pair status text with icons; preserve token contrast and honor reduced motion.

## i18n

Every label, model/worker/index state, offline explanation, confirmation, empty state and error has an EN / VI copy key. Interpolate `{count}`, `{version}`, `{time}`, `{done}` and `{total}`. Localize stable codes; never show raw API text.

| Key | EN / VI |
| --- | --- |
| admin.ai.title | AI index / Chỉ mục AI |
| admin.ai.description | Captioning, embeddings, models and search-index health. / Tình trạng chú thích, vector, mô hình và chỉ mục tìm kiếm. |
| admin.ai.action.refresh | Refresh / Làm mới |
| admin.ai.live | Live / Đang cập nhật |
| admin.ai.stale | Stale · updated {time} / Dữ liệu cũ · cập nhật lúc {time} |
| admin.ai.metric.captionBacklog | Caption backlog / Hàng đợi chú thích |
| admin.ai.metric.captioned | Captions completed / Chú thích đã tạo |
| admin.ai.metric.captionSpeed | Captions per minute / Chú thích mỗi phút |
| admin.ai.metric.embeddingBacklog | Embedding backlog / Hàng đợi tạo vector |
| admin.ai.metric.embedded | Embeddings completed / Vector đã tạo |
| admin.ai.metric.embeddingSpeed | Embeddings per second / Vector mỗi giây |
| admin.ai.metric.documents | Index documents / Tài liệu trong chỉ mục |
| admin.ai.model.caption | Caption model / Mô hình chú thích |
| admin.ai.model.embedding | Embedding model / Mô hình tạo vector |
| admin.ai.model.version | Version {version} / Phiên bản {version} |
| admin.ai.model.loaded | Loaded / Đã tải |
| admin.ai.model.offline | Offline / Ngoại tuyến |
| admin.ai.model.device | Device / Thiết bị |
| admin.ai.model.memory | Memory / Bộ nhớ |
| admin.ai.model.speed | Speed / Tốc độ |
| admin.ai.worker.title | AI worker / Worker AI |
| admin.ai.worker.state | Worker state / Trạng thái worker |
| admin.ai.worker.ready | Ready / Sẵn sàng |
| admin.ai.worker.busy | Busy / Đang xử lý |
| admin.ai.worker.unavailable | Unavailable / Không khả dụng |
| admin.ai.worker.concurrency | {count} of {total} workers / {count} trên {total} worker |
| admin.ai.worker.heartbeat | Last heartbeat · {time} / Nhịp cuối · {time} |
| admin.ai.index.title | Search index / Chỉ mục tìm kiếm |
| admin.ai.index.health | Index health / Tình trạng chỉ mục |
| admin.ai.index.documents | Documents / Tài liệu |
| admin.ai.index.size | Index size / Kích thước chỉ mục |
| admin.ai.index.metadataCoverage | Metadata coverage / Mức bao phủ siêu dữ liệu |
| admin.ai.index.captionCoverage | Caption coverage / Mức bao phủ chú thích |
| admin.ai.index.queryP95 | Query p95 / Truy vấn p95 |
| admin.ai.search.semantic | Semantic search / Tìm kiếm ngữ nghĩa |
| admin.ai.search.keywordOnly | Keyword-only search / Chỉ tìm kiếm từ khóa |
| admin.ai.reEnrichment.title | Re-enrichment / Tạo lại dữ liệu bổ sung |
| admin.ai.reEnrichment.stale | {count} items need model {version} / {count} mục cần mô hình {version} |
| admin.ai.reEnrichment.review | Review queue / Xem lại hàng đợi |
| admin.ai.reEnrichment.confirm | Enqueue re-enrichment / Đưa vào hàng đợi tạo lại |
| admin.ai.reEnrichment.confirmCopy | Queue {count} eligible items with model {version}? Existing results stay available. / Đưa {count} mục đủ điều kiện vào hàng đợi bằng mô hình {version}? Kết quả hiện có vẫn được giữ. |
| admin.ai.reEnrichment.accepted | Re-enrichment queued · {count} items / Đã đưa vào hàng đợi tạo lại · {count} mục |
| admin.ai.reEnrichment.cancel | Keep current data / Giữ dữ liệu hiện tại |
| admin.ai.offline.title | Ollama offline / Ollama ngoại tuyến |
| admin.ai.offline.body | New AI work is paused. Search is using keywords only; existing captions and vectors are preserved. / Tác vụ AI mới đang tạm dừng. Tìm kiếm chỉ dùng từ khóa; chú thích và vector hiện có được giữ nguyên. |
| admin.ai.offline.preview | Offline preview · test only / Xem trước ngoại tuyến · chỉ dùng để thử nghiệm |
| admin.ai.empty.title | No enrichment backlog / Không có hàng đợi bổ sung dữ liệu |
| admin.ai.empty.body | New work will appear as records need captions or embeddings. / Tác vụ mới sẽ xuất hiện khi bản ghi cần chú thích hoặc vector. |
| admin.ai.loading | Loading AI status / Đang tải trạng thái AI |
| admin.ai.error.load | Could not load AI status / Không tải được trạng thái AI |
| admin.ai.error.retry | Refresh and try again / Hãy làm mới rồi thử lại |
| admin.ai.error.capacity | Queue capacity is unavailable; no work was submitted. / Chưa rõ sức chứa hàng đợi; chưa gửi tác vụ nào. |
| admin.ai.error.operation | An AI operation is already running / Đang có một tác vụ AI |
| admin.ai.error.model | The selected model version is no longer current / Phiên bản mô hình đã chọn không còn hiện hành |
| admin.ai.error.forbidden | Admin access required / Cần quyền quản trị viên |

## Test IDs

`admin-ai-page, admin-ai-live-status, admin-ai-refresh, admin-ai-summary, admin-ai-metric-caption-backlog, admin-ai-metric-captioned, admin-ai-metric-caption-speed, admin-ai-metric-embedding-backlog, admin-ai-metric-embedding-speed, admin-ai-models, admin-ai-model-{kind}, admin-ai-worker, admin-ai-index, admin-ai-search-mode, admin-ai-reenrichment, admin-ai-reenrichment-review, admin-ai-reenrichment-confirmation, admin-ai-reenrichment-submit, admin-ai-operation, admin-ai-offline-preview, admin-ai-loading, admin-ai-empty, admin-ai-error, admin-ai-stale, admin-ai-forbidden`.

## Test hooks

The page root exposes `data-testid="admin-ai-page"`; metrics, model cards, worker/index panels, confirmation and all states use the stable hooks listed above.

## Out of scope

Application or OpenAPI implementation, model installation/configuration, direct Ollama controls, production failure simulation, worker restart/stop, manual index deletion/rebuild, queue purge, per-photo inspection, caption/prompt display, embedding export, and changes to USER search behavior.
