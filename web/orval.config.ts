import { defineConfig } from 'orval';

export default defineConfig({
  xflickr: {
    input: {
      target: '../.tmp/openapi.bundle.yaml',
    },
    output: {
      mode: 'tags-split',
      target: './src/shared/api/gen/endpoints.ts',
      schemas: './src/shared/api/gen/models',
      client: 'react-query',
      httpClient: 'fetch',
      clean: true,
      formatter: 'prettier',
      indexFiles: true,
      override: {
        mutator: {
          path: './src/shared/api/mutator.ts',
          name: 'customFetch',
        },
      },
    },
  },
  xflickrZod: {
    input: {
      target: '../.tmp/openapi.bundle.yaml',
    },
    output: {
      mode: 'single',
      target: './src/shared/api/zod/schemas.ts',
      client: 'zod',
      clean: true,
      formatter: 'prettier',
    },
  },
});
