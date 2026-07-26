import { useQuery } from '@tanstack/react-query';
import { Search } from 'lucide-react';
import { FormEvent, useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { api } from '../lib/api';
import type { Work } from '../lib/types';

export function SearchPage() {
  const [params, setParams] = useSearchParams();
  const query = (params.get('q') ?? '').trim();
  const [input, setInput] = useState(query);
  const results = useQuery({ queryKey: ['search-page', query], queryFn: () => api.feed({ search: query, order: 'desc', limit: 100 }), enabled: Boolean(query) });
  useEffect(() => setInput(query), [query]);
  const submit = (event: FormEvent) => { event.preventDefault(); const next = input.trim(); setParams(next ? { q: next } : {}); };
  return <div className="page search-page">
    <header className="page-header search-page-header"><div><h1>搜索本地作品</h1><p>搜索 UP主、视频标题、图文、描述、标签或音乐。</p></div>
      <form className="search-page-form" onSubmit={submit}><Search size={20} /><input aria-label="搜索本地作品" value={input} maxLength={200} onChange={(e) => setInput(e.target.value)} placeholder="搜索本地作品" /><button type="submit" disabled={!input.trim()}>搜索</button></form>
    </header>
    {!query ? <div className="empty-state search-empty-state">输入关键词开始搜索本地作品。</div>
      : results.isLoading ? <div className="empty-state">正在搜索...</div>
      : results.isError ? <div className="empty-state error-state"><span>搜索失败：{results.error instanceof Error ? results.error.message : '请稍后重试'}</span><button type="button" onClick={() => void results.refetch()}>重试</button></div>
      : (results.data?.works.length ?? 0) === 0 ? <div className="empty-state search-empty-state">没有找到与“{query}”相关的作品。</div>
      : <><div className="search-result-summary"><strong>共 {results.data!.works.length} 条结果</strong></div><div className="search-result-list">{results.data!.works.map((work) => <SearchWork key={work.id} work={work} query={query} />)}</div></>}
  </div>;
}

function SearchWork({ work, query }: { work: Work; query: string }) {
  const isVideo = work.media.some((media) => media.type === 'video');
  const kind = isVideo ? (work.work_type === 'video_short' ? '短视频' : '视频') : '图文';
  const target = `/search/play?${new URLSearchParams({ q: query, work_id: String(work.id) })}`;
  return <Link className="search-work-row" to={target} aria-label={`${work.title}，@${work.creator_name}`}><span className="search-work-type">{kind}</span><strong>{work.title || work.description || '未命名作品'}</strong><span className="search-work-creator">@{work.creator_name}</span><span>{work.description}</span></Link>;
}
