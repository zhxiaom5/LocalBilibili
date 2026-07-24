import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { ScanEvent } from './types';

export function useRealtimeEvents() {
  const queryClient = useQueryClient();

  useEffect(() => {
    const source = new EventSource('/api/events', { withCredentials: true });
    source.addEventListener('scan', (message) => {
      let event: ScanEvent | null = null;
      try {
        event = JSON.parse((message as MessageEvent).data) as ScanEvent;
      } catch {
        event = null;
      }
      queryClient.invalidateQueries({ queryKey: ['scan-status'] });
      queryClient.invalidateQueries({ queryKey: ['directories'] });
      if (event?.type === 'creator_completed' || event?.type === 'completed') {
        queryClient.invalidateQueries({ queryKey: ['creators'] });
        queryClient.invalidateQueries({ queryKey: ['feed'] });
      }
    });
    source.addEventListener('update', () => {
      queryClient.invalidateQueries({ queryKey: ['update-status'] });
      queryClient.invalidateQueries({ queryKey: ['update-jobs'] });
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
      queryClient.invalidateQueries({ queryKey: ['directories'] });
      queryClient.invalidateQueries({ queryKey: ['creators'] });
      queryClient.invalidateQueries({ queryKey: ['feed'] });
    });
    source.addEventListener('subtitle', () => {
      queryClient.invalidateQueries({ queryKey: ['subtitle-status'] });
      queryClient.invalidateQueries({ queryKey: ['subtitle-jobs'] });
      queryClient.invalidateQueries({ queryKey: ['subtitle-queues'] });
      queryClient.invalidateQueries({ queryKey: ['creators'] });
    });
    return () => source.close();
  }, [queryClient]);
}
