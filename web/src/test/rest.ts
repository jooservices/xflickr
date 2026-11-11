type Links = Record<string, { href: string; method: string }>;
export type RestUser = {
  id: number;
  email: string;
  role: 'admin' | 'user';
  locale: string;
  created_at: string;
  links?: Links;
};
type Session = {
  id: string;
  current: boolean;
  user_agent_label: string;
  ip_masked: string;
  last_seen_at: string;
  created_at: string;
  expires_at: string;
  links?: Links;
};

export function restEnvelope<T>(data: T, code = 200) {
  return {
    success: true as const,
    code,
    message: code === 201 ? 'Created' : 'OK',
    data,
    meta: {},
    errors: null,
    trace_id: 'test-trace',
  };
}

export function restProblem(
  type: string,
  status: number,
  errors: Record<string, string[]> | null = null,
) {
  return {
    type,
    title: 'Request failed',
    status,
    detail: 'The request could not be completed',
    instance: '/api/v1/test',
    trace_id: 'test-trace',
    errors,
  };
}

const profileLinks: Links = {
  self: { href: '/api/v1/me', method: 'GET' },
  update: { href: '/api/v1/me', method: 'PATCH' },
  sessions: { href: '/api/v1/me/sessions', method: 'GET' },
  password: { href: '/api/v1/me/password', method: 'PUT' },
  sharing: { href: '/api/v1/me/sharing', method: 'GET' },
};

export function restUser(user: RestUser) {
  return { ...user, links: user.links ?? profileLinks };
}

export function userEnvelope(user: RestUser, code = 200) {
  return restEnvelope(restUser(user), code);
}

export function authenticationOptions(
  options: Partial<{
    registration_open: boolean;
    passkey_enabled: boolean;
    terms_url: string;
    privacy_url: string;
    password_min: number;
    password_max: number;
  }> = {},
) {
  return restEnvelope({
    registration_open: true,
    passkey_enabled: false,
    password_min: 8,
    password_max: 128,
    ...options,
    links: { self: { href: '/api/v1/authentication-options', method: 'GET' } },
  });
}

function restSession(session: Partial<Session> & Pick<Session, 'id'>): Session {
  return {
    current: false,
    user_agent_label: 'Firefox on Linux',
    ip_masked: '203.0.113.x',
    last_seen_at: '2026-10-25T10:00:00Z',
    created_at: '2026-10-20T10:00:00Z',
    expires_at: '2026-11-20T10:00:00Z',
    ...session,
    links: session.links ?? {
      self: { href: `/api/v1/sessions/${session.id}`, method: 'GET' },
      revoke: { href: `/api/v1/me/sessions/${session.id}`, method: 'DELETE' },
    },
  };
}

export function sessionEnvelope(
  session: Partial<Session> & Pick<Session, 'id'>,
  user: RestUser = {
    id: 1,
    email: 'a@example.test',
    role: 'user',
    locale: 'en',
    created_at: '2026-10-01T00:00:00Z',
  },
  code = 201,
) {
  return restEnvelope({ ...restSession(session), user: restUser(user) }, code);
}

export function sessionCollectionEnvelope(sessions: Array<Partial<Session> & Pick<Session, 'id'>>) {
  return restEnvelope(sessions.map(restSession));
}
