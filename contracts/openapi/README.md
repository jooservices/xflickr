This directory contains the OpenAPI 3.1 contract shared by the XFlickr backend and web client.

`openapi.yaml` is the root document. Each module adds `paths/<module>.yaml` and `schemas/<module>.yaml`, and the root references those files with `$ref`. `make gen` bundles the root with Redocly CLI, then generates the Go server and the TypeScript client.
