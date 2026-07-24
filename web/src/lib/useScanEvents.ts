import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { ScanEvent } from './types';

export function useScanEvents(onEvent?: (event: ScanEvent) => void) {
  const queryClient = useQueryClient();

  useEffect(() => {
    const source = new EventSource('/api/scan/events');
    source.addEventListener('scan', (message) => {
      const event = JSON.parse((message as MessageEvent).data) as ScanEvent;
      onEvent?.(event);
      queryClient.invalidateQueries({ queryKey: ['scan-status'] });
      queryClient.invalidateQueries({ queryKey: ['directories'] });
      if (event.type === 'creator_completed' || event.type === 'completed') {
        queryClient.invalidateQueries({ queryKey: ['creators'] });
        queryClient.invalidateQueries({ queryKey: ['feed'] });
      }
    });
    return () => source.close();
  }, [onEvent, queryClient]);
}
