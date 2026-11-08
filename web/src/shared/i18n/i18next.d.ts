import 'i18next';
import type { resources } from './init';

declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'common';
    // Resources document the catalog shape for editors; runtime keys go through
    // the MessageKey wrapper (keySeparator is false).
    resources: (typeof resources)['en'];
    returnNull: false;
  }
}
