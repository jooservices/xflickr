import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { PhotoViewer } from '../src/features/photo-viewer';
import { I18nProvider } from '../src/shared/i18n/I18nProvider';
import '../src/shared/ui/tokens.css';

function PhotoViewerHarness() {
  const [open, setOpen] = useState(false);
  const [client] = useState(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  );
  const [opener, setOpener] = useState<HTMLButtonElement | null>(null);

  return (
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="en">
        <main className="gallery-harness" style={{ minHeight: '180vh', padding: 24 }}>
          <h1 tabIndex={-1}>Mock gallery</h1>
          <p>The viewer is mounted only for this test harness.</p>
          <button
            ref={setOpener}
            type="button"
            onClick={() => setOpen(true)}
            data-testid="gallery-open"
          >
            Open photo viewer
          </button>
        </main>
        {open ? (
          <PhotoViewer
            photoIds={[101, 202]}
            initialIndex={0}
            opener={opener}
            onClose={() => setOpen(false)}
          />
        ) : null}
      </I18nProvider>
    </QueryClientProvider>
  );
}

const target = document.getElementById('photo-viewer-harness');
if (!target) throw new Error('Photo viewer harness root is missing');
createRoot(target).render(<PhotoViewerHarness />);
