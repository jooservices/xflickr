/** Bodies shaped like the OpenAPI contract, so mocked routes pass response validation. */
export const apiKeyBody = (overrides: Record<string, unknown> = {}) => ({
  id: 1,
  label: 'Main',
  key_public: 'abc123************',
  per_hour_limit: 3600,
  per_second_limit: 1,
  burst: 10,
  status: 'valid',
  created_at: '2026-01-01T00:00:00Z',
  ...overrides,
});

export const flickrAccountBody = (overrides: Record<string, unknown> = {}) => ({
  id: 1,
  key_id: 11,
  nsid: '123@N01',
  username: 'anna',
  status: 'ok',
  created_at: '2026-01-01T00:00:00Z',
  ...overrides,
});
