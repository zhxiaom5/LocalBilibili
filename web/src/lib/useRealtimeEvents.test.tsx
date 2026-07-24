import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useRealtimeEvents } from './useRealtimeEvents';

type ListenerMap = Record<string, Array<(event: MessageEvent) => void>>;

let listeners: ListenerMap;
let createdSources: Array<{ url: string; close: ReturnType<typeof vi.fn> }>;

function RealtimeProbe() {
  useRealtimeEvents();
  return null;
}

function renderProbe(client: QueryClient) {
  return render(
    <QueryClientProvider client={client}>
      <RealtimeProbe />
    </QueryClientProvider>,
  );
}

function emitRealtime(type: string, data: unknown) {
  for (const listener of listeners[type] ?? []) {
    listener(new MessageEvent(type, { data: JSON.stringify(data) }));
  }
}

beforeEach(() => {
  listeners = {};
  createdSources = [];
  vi.stubGlobal(
    'EventSource',
    class {
      close = vi.fn();

      constructor(url: string | URL) {
        createdSources.push({ url: String(url), close: this.close });
      }

      addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
        const callback =
          typeof listener === 'function' ? listener : listener.handleEvent.bind(listener);
        listeners[type] = [...(listeners[type] ?? []), callback as (event: MessageEvent) => void];
      }
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

it('opens one unified realtime stream and invalidates scan queries', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidateSpy = vi.spyOn(client, 'invalidateQueries');
  const view = renderProbe(client);

  expect(createdSources.map((source) => source.url)).toEqual(['/api/events']);

  emitRealtime('scan', { type: 'creator_completed' });

  await waitFor(() => {
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['scan-status'] });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['directories'] });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['creators'] });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['feed'] });
  });

  view.unmount();
  expect(createdSources[0].close).toHaveBeenCalled();
});

it('invalidates update and subtitle query groups from the unified stream', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidateSpy = vi.spyOn(client, 'invalidateQueries');
  renderProbe(client);

  emitRealtime('update', { type: 'updated' });
  emitRealtime('subtitle', { type: 'completed' });

  await waitFor(() => {
    for (const key of [
      'update-status',
      'update-jobs',
      'update-queues',
      'directories',
      'creators',
      'feed',
      'subtitle-status',
      'subtitle-jobs',
      'subtitle-queues',
    ]) {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: [key] });
    }
  });
});
