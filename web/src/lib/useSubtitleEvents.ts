import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';

export function useSubtitleEvents() {
  const queryClient = useQueryClient();

  useEffect(() => {
    const source = new EventSource('/api/subtitles/events', { withCredentials: true });
    source.addEventListener('subtitle', () => {
      queryClient.invalidateQueries({ queryKey: ['subtitle-status'] });
      queryClient.invalidateQueries({ queryKey: ['subtitle-jobs'] });
      queryClient.invalidateQueries({ queryKey: ['subtitle-queues'] });
      queryClient.invalidateQueries({ queryKey: ['creators'] });
    });
    return () => source.close();
  }, [queryClient]);
}
