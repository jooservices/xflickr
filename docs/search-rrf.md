# Search ranking fusion

The search application package combines ranked BM25 and vector candidate IDs
with Reciprocal Rank Fusion. For each ID, its score is the sum of
`1 / (60 + rank)` from each branch where it appears. Rank starts at 1; a branch
that does not contain the ID contributes no score and is reported as rank 0.

Results are ordered by descending score. Equal scores are ordered by ID in
ascending Go string order, which compares the underlying bytes. Each ID appears
once in the merged result. Empty IDs and repeated IDs within one branch are
invalid; the same ID in both branches is valid.

Callers provide already-ranked, bounded candidate lists. This function does not
retrieve candidates or choose their window size. This library slice is not yet
connected to an endpoint, an indexer, an embedder, or the user interface.
