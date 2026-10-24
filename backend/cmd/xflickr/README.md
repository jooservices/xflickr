This directory contains the `xflickr` command line entry point, which selects the requested application roles and coordinates their lifecycle.

## Re-wrap encrypted data keys

`xflickr crypto rotate` re-wraps JSONB `Sealed` values in committed batches.
Pass one target for each sealed column, using `table,id-column,sealed-column`:

```sh
xflickr crypto rotate --target app_settings,id,secret --batch-size 100
```

Repeat `--target` for additional columns. The table can be schema-qualified.
Rows already using `XFLICKR_MASTER_KEY_ACTIVE` are skipped, so running the same
command again resumes after an interrupted rotation. The command reads master
keys from `XFLICKR_MASTER_KEYS` as comma-separated `id:base64` values and the
active ID from `XFLICKR_MASTER_KEY_ACTIVE`.
