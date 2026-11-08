import { screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { beforeEach, describe, expect, it } from 'vitest';
import { renderApp, userSession } from '../test/render';
import { server } from '../test/server';
import { i18nInstance } from '../shared/i18n';

const signedOut = { status: 'unauthenticated' as const };

describe('page copy in English and Vietnamese', () => {
  beforeEach(async () => {
    await i18nInstance.changeLanguage('en');
    server.use(
      http.get('*/keys', () => HttpResponse.json([])),
      http.get('*/flickr-accounts', () => HttpResponse.json([])),
      http.get('*/me/crawl', () =>
        HttpResponse.json({
          status: 'idle',
          depth: 0,
          depth_max: 3,
          depth_default: 0,
          accounts: [],
        }),
      ),
      http.get('*/me/crawl/settings', () =>
        HttpResponse.json({ depth: 0, depth_max: 3, depth_default: 0 }),
      ),
      http.get('*/me/crawl/queue', () => HttpResponse.json({ lanes: [], depths: [] })),
      http.get('*/auth/config', () =>
        HttpResponse.json({
          registration_open: true,
          passkey_enabled: false,
          password_min: 8,
          password_max: 128,
        }),
      ),
    );
  });

  it.each([
    {
      path: '/login',
      session: signedOut,
      testId: 'auth-login-title',
      english: 'Sign in',
      vietnamese: 'Đăng nhập',
    },
    {
      path: '/register',
      session: signedOut,
      testId: 'auth-register-title',
      english: 'Create your account',
      vietnamese: 'Tạo tài khoản của bạn',
    },
    {
      path: '/keys',
      session: userSession,
      testId: 'keys-title',
      english: 'Keys & quota',
      vietnamese: 'Khóa & hạn mức',
    },
    {
      path: '/crawl',
      session: userSession,
      testId: 'user-crawl-title',
      english: 'Crawl',
      vietnamese: 'Thu thập',
    },
    {
      path: '/profile',
      session: userSession,
      testId: 'profile-title',
      english: 'Profile',
      vietnamese: 'Hồ sơ',
    },
  ])(
    'renders $path labels in both languages',
    async ({ path, session, testId, english, vietnamese }) => {
      const en = renderApp({ session, path, locale: 'en' });
      expect(await screen.findByTestId(testId)).toHaveTextContent(english);
      en.unmount();

      await i18nInstance.changeLanguage('vi');
      renderApp({ session, path, locale: 'vi' });
      expect(await screen.findByTestId(testId)).toHaveTextContent(vietnamese);
    },
  );
});
