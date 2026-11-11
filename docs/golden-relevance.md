# Golden relevance evaluator

The package-local evaluator calculates macro-average recall@3 for fixed search
fixtures. For each query, it divides the unique relevant IDs among the first
three results by the number of expected IDs, then averages those per-query
recalls. The first three result positions are selected before duplicate IDs are
removed; an ID can count as a hit only once. Fewer than three results are valid.

The test passes when the macro-average is at least `0.8`. This is a fixed test
criterion, not an administrator setting or runtime search configuration.

`backend/internal/search/golden/testdata/golden/sample.json` contains two
queries to check the evaluator and threshold behavior. It is a small sample,
not the planned 50-photo, 20-query golden set, and it does not measure real
search quality. It contains no generated or precomputed embeddings.

Run the package tests from `backend`:

```sh
go test -p 1 ./internal/search/golden
```

The real relevance test depends on JOOXF-137 implementing the hybrid query
path. That later slice will provide the full fixture and exercise search with
precomputed embeddings; this evaluator does not call Elasticsearch or the
search application.
