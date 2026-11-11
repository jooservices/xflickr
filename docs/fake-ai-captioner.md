# Fake AI Captioner

The fake Captioner in `backend/internal/ai/adapters/fake` is a deterministic,
network-free adapter for unit tests. Each instance receives one filename hint:

```go
captioner, err := fake.NewCaptioner("fixtures/cat-on-sofa.jpg")
caption, err := captioner.Describe(ctx, imageBytes)
```

The constructor normalizes backslashes to slashes, takes the basename, removes
the final extension, and trims the stem. It returns `Photo: cat-on-sofa` for
the example; `cat.on.sofa.tar.gz` returns `Photo: cat.on.sofa.tar`. Without an
extension it uses the full basename. A missing stem wraps
`app.ErrInvalidConfiguration`.

`Describe` requires a non-empty image and non-nil context; it returns
`app.ErrInvalidImage` for empty images and propagates cancellation. It never
reads files, inspects bytes, calls providers, or uses time or randomness. Its
result has empty `Objects`, `Model: "fake"`, and `Version: "1"`.

This adapter is test-only and not selected by runtime configuration; it has no
`AI_PROVIDER=fake` option. A fake Embedder is outside this slice.
