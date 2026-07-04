import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';
import { api } from '../lib/api';
import { FeedViewer } from '../components/FeedViewer';

const FEED_ORDER_KEY = 'localbilibili.feedOrder';

type FeedOrder = 'random' | 'desc' | 'asc';

function readFeedOrder(): FeedOrder {
  try {
    const value = window.localStorage?.getItem(FEED_ORDER_KEY);
    if (value === 'desc' || value === 'asc' || value === 'random') return value;
  } catch {
    // localStorage can be unavailable in some test environments.
  }
  return 'random';
}

export function FeedPage() {
  const params = useParams();
  const [searchParams] = useSearchParams();
  const creatorId = parsePositiveInt(params.creatorId);
  const favoriteFolderId = parsePositiveInt(params.favoriteFolderId);
  const initialWorkId = parsePositiveInt(searchParams.get('work_id'));
  const [search, setSearch] = useState('');
  const [order, setOrder] = useState<FeedOrder>(readFeedOrder);
  const [currentWorkId, setCurrentWorkId] = useState<number | undefined>(initialWorkId);
  const feed = useQuery({
    queryKey: ['feed', creatorId ?? 'random', favoriteFolderId ?? 'all', search, order],
    queryFn: () => api.feed({ creatorId, favoriteFolderId, search, order, limit: 40 }),
  });

  const handleOrderChange = (nextOrder: FeedOrder) => {
    setOrder(nextOrder);
    try {
      window.localStorage?.setItem(FEED_ORDER_KEY, nextOrder);
    } catch {
      // localStorage can be unavailable in some test environments.
    }
  };

  return (
    <div className="page feed-page">
      {feed.isLoading ? (
        <div className="empty-state">加载中...</div>
      ) : feed.isError ? (
        <div className="empty-state">Feed 加载失败：{feed.error instanceof Error ? feed.error.message : '请稍后重试'}</div>
      ) : (
        <FeedViewer
          works={feed.data?.works ?? []}
          search={search}
          onSearch={setSearch}
          enableFavorites
          initialWorkId={currentWorkId ?? initialWorkId}
          order={order}
          onOrderChange={handleOrderChange}
          onCurrentWorkChange={setCurrentWorkId}
        />
      )}
    </div>
  );
}

function parsePositiveInt(value: string | null | undefined) {
  if (!value) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}
