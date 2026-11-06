# Coverage gates

XFlickr enforces Go statement coverage at two levels, both at 85 percent.

- The package gate (`docker/scripts/coverage_gate.py` without file flags)
  measures statement coverage per Go package from a cover profile and
  fails when a measured package is below the threshold. Packages listed
  in `docker/scripts/coverage-exclude.txt` are skipped with the reason
  recorded there.
- The file gate (`--check-files`) measures statement coverage per
  source file, merging duplicate cover blocks, and fails when a Go
  file changed against the base ref (`origin/develop` by default, as
  `git diff --name-only origin/develop...HEAD`) is below 85 percent.
  The report names every failing file with its percentage. Test files
  (`*_test.go`), generated files (paths containing `gen` as a directory
  component, or ending in `.gen.go`), and files excluded by
  `docker/scripts/coverage-exclude.txt` are ignored. A changed file
  with no profile entry that still exists on disk counts as 0 percent
  when it declares a function or method (a line starting with `func `,
  outside comments); a changed file with only const, type, or var
  declarations and no `func` declaration has no executable statements,
  is reported as `no statements`, and passes. A deleted file is
  skipped.

## Baseline for pre-existing files

Files that were already below 85 percent when the file gate was
introduced are listed in `docker/scripts/coverage-file-baseline.txt`,
one `path percent` line per file (profile-relative path, percent
floored to one decimal). The baseline only shrinks:

- A baseline file may change only if its coverage does not drop below
  its recorded percent. A drop fails the gate, whether or not the
  file is in the changed list.
- A changed baseline file at or above its recorded percent but below
  85 percent passes with status `BASELINE`.
- When a baseline file reaches 85 percent the gate passes and prints
  a message asking to delete its baseline line; it does not fail.

Regenerate the baseline from a current profile (for example after a
deliberate baseline review, never to hide a drop) with:

```sh
python3 docker/scripts/coverage_gate.py <profile> \
  --exclude docker/scripts/coverage-exclude.txt \
  --write-baseline docker/scripts/coverage-file-baseline.txt
```

## Wiring

`docker/scripts/coverage-gate.sh` runs the backend tests, writes the
profile to `.tmp/xflickr-coverage.out`, and runs the package gate.
`docker/scripts/test.sh` and the `go-coverage` job in
`.github/workflows/ci.yml` then run the file gate against that same
profile. The CI checkout uses `fetch-depth: 0` so the diff against
`origin/develop` is available. Locally, `COVERAGE_BASE` overrides the
base ref; ensure the ref is fetched before running `make test`.

Unit tests for the gate live in `docker/scripts/test_coverage_gate.py`
and run with `python3 docker/scripts/test_coverage_gate.py`.
