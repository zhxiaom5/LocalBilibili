import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';

export function useUpdateEvents() {
  const queryClient = useQueryClient();

  useEffect(() => {
    const source = new EventSource('/api/update/events', { withCredentials: true });
    source.addEventListener('update', () => {
      queryClient.invalidateQueries({ queryKey: ['update-status'] });
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
      queryClient.invalidateQueries({ queryKey: ['directories'] });
      queryClient.invalidateQueries({ queryKey: ['creators'] });
      queryClient.invalidateQueries({ queryKey: ['feed'] });
    });
    return () => source.close();
  }, [queryClient]);
}
