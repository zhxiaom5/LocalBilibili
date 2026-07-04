import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { CheckSquare, FolderSearch, Link2, RefreshCw, Search, Square, Trash2, UserPlus, UserRound, X } from 'lucide-react';
import { DirectoryBrowser } from '../components/DirectoryBrowser';
import { api } from '../lib/api';
import type { BulkImportItem, Creator, Directory, UpdateQueue, User } from '../lib/types';

type SubscribeTarget = Creator | 'batch' | { type: 'bulk'; ids: number[] } | null;
type Notice = { type: 'success' | 'error'; text: string } | null;

export function CreatorsPage({ currentUser }: { currentUser: User }) {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [batchMode, setBatchMode] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  const [subscribeTarget, setSubscribeTarget] = useState<SubscribeTarget>(null);
  const [bulkOpen, setBulkOpen] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const [linkTarget, setLinkTarget] = useState<Creator | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Creator | null>(null);
  const [sort, setSort] = useState<'updated_at' | 'name' | 'views'>('updated_at');
  const [order, setOrder] = useState<'asc' | 'desc'>('desc');
  const [filterByWorkCount, setFilterByWorkCount] = useState(true);
  const [minWorkCount, setMinWorkCount] = useState(1);
  const creators = useQuery({
    queryKey: ['creators', search, sort, order, filterByWorkCount, minWorkCount],
    queryFn: () => api.creators(search, { sort, order, minWorkCount: filterByWorkCount ? minWorkCount : undefined }),
  });
  const directories = useQuery({ queryKey: ['directories'], queryFn: api.directories, enabled: currentUser.is_admin });
  const queues = useQuery({ queryKey: ['update-queues'], queryFn: api.updateQueues, enabled: currentUser.is_admin });
  const creatorList = creators.data?.creators ?? [];
  const directoryList = directories.data?.directories ?? [];
  const queueList = queues.data?.queues ?? [];
  const selectedCreators = subscribeTarget === 'batch' ? selected : subscribeTarget && 'type' in subscribeTarget ? subscribeTarget.ids : subscribeTarget ? [subscribeTarget.id] : [];

  const immediate = useMutation({
    mutationFn: (ids: number[]) => api.enqueueImmediateUpdate(ids),
    onSuccess: (_data, ids) => {
      setNotice({ type: 'success', text: `已加入立即更新队列：${ids.length} 个UP主` });
      setSelected([]);
      setBatchMode(false);
      queryClient.invalidateQueries({ queryKey: ['update-status'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const subtitleImmediate = useMutation({
    mutationFn: (ids: number[]) => api.enqueueImmediateSubtitles(ids),
    onSuccess: (_data, ids) => {
      setNotice({ type: 'success', text: `已加入立即转字幕队列：${ids.length} 个UP主` });
      queryClient.invalidateQueries({ queryKey: ['subtitle-status'] });
      queryClient.invalidateQueries({ queryKey: ['subtitle-jobs'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const copyCreatorSubtitles = useMutation({
    mutationFn: api.creatorSubtitleLink,
    onSuccess: async (data) => {
      await copyText(absoluteURL(data.url));
      setNotice({ type: 'success', text: '字幕合集链接已复制；如果字幕缺失，打开链接会自动加入字幕队列。' });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const subscribe = useMutation({
    mutationFn: async ({ queueIds, creatorIds }: { queueIds: number[]; creatorIds: number[] }) => {
      await Promise.all(queueIds.map((queueId) => api.addUpdateQueueCreators(queueId, creatorIds)));
      return { ok: true };
    },
    onSuccess: (_data, variables) => {
      setSubscribeTarget(null);
      setSelected([]);
      setBatchMode(false);
      setNotice({ type: 'success', text: `已保存订阅：${variables.creatorIds.length} 个UP主加入 ${variables.queueIds.length} 个队列` });
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const deleteCreator = useMutation({
    mutationFn: ({ id, deleteMedia }: { id: number; deleteMedia: boolean }) => api.deleteCreator(id, deleteMedia),
    onSuccess: (_data, variables) => {
      const name = deleteTarget?.name ?? 'UP主';
      setDeleteTarget(null);
      setSelected((current) => current.filter((id) => id !== variables.id));
      setNotice({ type: 'success', text: `已删除UP主：${name}` });
      queryClient.invalidateQueries({ queryKey: ['creators'] });
      queryClient.invalidateQueries({ queryKey: ['feed'] });
      queryClient.invalidateQueries({ queryKey: ['favorite-creators'] });
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
      queryClient.invalidateQueries({ queryKey: ['subtitle-queues'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });

  return (
    <div className="page narrow-page creators-page">
      <header className="page-header">
        <h1>关注</h1>
        <p>来自已保存目录的UP主列表</p>
      </header>
      <div className="creator-toolbar">
        <label className="page-search">
          <Search size={18} />
          <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索UP主" aria-label="搜索UP主" />
        </label>
        {currentUser.is_admin && (
          <div className="creator-batch-actions">
            <button type="button" onClick={() => setBatchMode((value) => !value)}>
              {batchMode ? <CheckSquare size={16} /> : <Square size={16} />} 批量选择
            </button>
            <button type="button" disabled={selected.length === 0 || immediate.isPending} onClick={() => immediate.mutate(selected)}>
              <RefreshCw size={16} /> 立即更新
            </button>
            <button type="button" disabled={selected.length === 0} onClick={() => setSubscribeTarget('batch')}>
              批量订阅
            </button>
            <button type="button" onClick={() => setBulkOpen(true)}>
              <UserPlus size={16} /> 批量新增关注
            </button>
          </div>
        )}
      </div>
      <div className="creator-list-controls">
        <label>
          排序
          <select value={sort} onChange={(event) => setSort(event.target.value as typeof sort)} aria-label="UP主排序字段">
            <option value="updated_at">最近更新时间</option>
            <option value="name">UP主名</option>
            <option value="views">UP主访问量</option>
          </select>
        </label>
        <button type="button" onClick={() => setOrder((value) => (value === 'desc' ? 'asc' : 'desc'))}>
          {order === 'desc' ? '倒序' : '正序'}
        </button>
        <label className="inline-check">
          <input type="checkbox" checked={filterByWorkCount} onChange={(event) => setFilterByWorkCount(event.target.checked)} />
          按作品数过滤
        </label>
        <input
          className="compact-number"
          type="number"
          min={0}
          value={minWorkCount}
          disabled={!filterByWorkCount}
          onChange={(event) => setMinWorkCount(Math.max(0, Number(event.target.value) || 0))}
          aria-label="最少作品数"
        />
      </div>
      {notice && (
        <p className={notice.type === 'success' ? 'success-line' : 'error-line'} role="status">
          {notice.text}
        </p>
      )}
      <div className="creator-list">
        {creatorList.map((creator) => (
          <article className="creator-card creator-card-with-actions" key={creator.id}>
            {batchMode && (
              <input
                aria-label={`选择-${creator.name}`}
                type="checkbox"
                checked={selected.includes(creator.id)}
                onChange={(event) =>
                  setSelected((current) => (event.target.checked ? [...current, creator.id] : current.filter((id) => id !== creator.id)))
                }
              />
            )}
            <Link className="creator-card-main" to={`/creator/${creator.id}`}>
              <div className="avatar">
                <CreatorAvatar creator={creator} />
              </div>
              <div>
                <strong>{creator.name}</strong>
                <span>
                  {creator.work_count} 个作品 · {creator.subtitle_count ?? 0} 个字幕 · 播放 {formatCompactCount(creator.view_count ?? 0)} · {creator.online_identity_status === 'ready' ? '可在线更新' : '需补充主页信息'}
                </span>
                {creator.bio && <small className="creator-bio">{creator.bio}</small>}
                {creator.linked_creators && creator.linked_creators.length > 0 && (
                  <small>已关联 {creator.linked_creators.map((item) => item.name).join('、')}</small>
                )}
              </div>
            </Link>
            {currentUser.is_admin && (
              <div className="creator-update-actions creator-update-actions-two-rows">
                <button type="button" onClick={() => immediate.mutate([creator.id])} disabled={immediate.isPending}>
                  立即更新
                </button>
                <button type="button" onClick={() => setSubscribeTarget(creator)}>
                  订阅更新
                </button>
                <button type="button" onClick={() => setLinkTarget(creator)}>
                  <Link2 size={15} /> 关联创作者
                </button>
                <button type="button" onClick={() => subtitleImmediate.mutate([creator.id])} disabled={subtitleImmediate.isPending}>
                  立即转字幕
                </button>
                <button type="button" onClick={() => copyCreatorSubtitles.mutate(creator.id)} disabled={copyCreatorSubtitles.isPending || creator.work_count === 0}>
                  获取字幕
                </button>
                <button type="button" className="danger" onClick={() => setDeleteTarget(creator)} disabled={deleteCreator.isPending}>
                  <Trash2 size={15} /> 删除
                </button>
              </div>
            )}
          </article>
        ))}
      </div>
      {!creators.isLoading && creatorList.length === 0 && <div className="empty-state">{search.trim() ? '没有匹配的UP主。' : '扫描成功后，UP主会出现在这里。'}</div>}
      {subscribeTarget && (
        <SubscribeDialog
          queues={queueList}
          creatorIds={selectedCreators}
          onClose={() => setSubscribeTarget(null)}
          onSubmit={(queueIds) => subscribe.mutate({ queueIds, creatorIds: selectedCreators })}
          pending={subscribe.isPending}
        />
      )}
      {bulkOpen && (
        <BulkImportDialog
          directories={directoryList}
          onClose={() => setBulkOpen(false)}
          onImported={(ids) => {
            queryClient.invalidateQueries({ queryKey: ['creators'] });
            queryClient.invalidateQueries({ queryKey: ['directories'] });
            return ids;
          }}
          onImmediateAsync={(ids) => immediate.mutateAsync(ids)}
          onSubscribe={(ids) => {
            setBulkOpen(false);
            setSubscribeTarget({ type: 'bulk', ids });
          }}
        />
      )}
      {linkTarget && (
        <CreatorLinksDialog
          creator={linkTarget}
          onClose={() => setLinkTarget(null)}
          onSaved={() => {
            queryClient.invalidateQueries({ queryKey: ['creators'] });
            queryClient.invalidateQueries({ queryKey: ['feed'] });
          }}
        />
      )}
      {deleteTarget && (
        <DeleteCreatorDialog
          creator={deleteTarget}
          pending={deleteCreator.isPending}
          onClose={() => setDeleteTarget(null)}
          onConfirm={(deleteMedia) => deleteCreator.mutate({ id: deleteTarget.id, deleteMedia })}
        />
      )}
    </div>
  );
}

function CreatorAvatar({ creator }: { creator: Pick<Creator, 'name' | 'avatar_url'> }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [creator.avatar_url]);
  if (creator.avatar_url && !failed) {
    return <img className="creator-avatar-img" src={creator.avatar_url} alt={`${creator.name} 头像`} loading="lazy" decoding="async" referrerPolicy="no-referrer" onError={() => setFailed(true)} />;
  }
  return <UserRound size={24} />;
}

function formatCompactCount(value: number) {
  if (value >= 1_000_000_000) return `${trimCompact(value / 1_000_000_000)}B`;
  if (value >= 1_000_000) return `${trimCompact(value / 1_000_000)}M`;
  if (value >= 1_000) return `${trimCompact(value / 1_000)}K`;
  return String(value);
}

function trimCompact(value: number) {
  return value >= 10 ? value.toFixed(0) : value.toFixed(1).replace(/\.0$/, '');
}

function absoluteURL(path: string) {
  if (/^https?:\/\//i.test(path)) return path;
  return `${window.location.origin}${path.startsWith('/') ? path : `/${path}`}`;
}

async function copyText(value: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const input = document.createElement('textarea');
  input.value = value;
  input.style.position = 'fixed';
  input.style.opacity = '0';
  document.body.appendChild(input);
  input.select();
  document.execCommand('copy');
  input.remove();
}

function BulkImportDialog({
  directories,
  onClose,
  onImported,
  onImmediateAsync,
  onSubscribe,
}: {
  directories: Directory[];
  onClose: () => void;
  onImported: (ids: number[]) => number[];
  onImmediateAsync: (ids: number[]) => Promise<unknown>;
  onSubscribe: (ids: number[]) => void;
}) {
  const queryClient = useQueryClient();
  const [directoryID, setDirectoryID] = useState<number>(directories[0]?.id ?? 0);
  const [directoryPath, setDirectoryPath] = useState('');
  const [browserOpen, setBrowserOpen] = useState(false);
  const [savedDirectories, setSavedDirectories] = useState<Directory[]>([]);
  const [text, setText] = useState('');
  const [items, setItems] = useState<BulkImportItem[]>([]);
  const [importedCreators, setImportedCreators] = useState<Creator[]>([]);
  const [actionMessage, setActionMessage] = useState<Notice>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const allDirectories = [...savedDirectories, ...directories.filter((directory) => !savedDirectories.some((saved) => saved.id === directory.id))];
  const selectedDirectory = allDirectories.find((directory) => directory.id === directoryID);
  const validProfileURLs = items.filter((item) => item.status === 'ready' && item.bilibili_space_url).map((item) => item.bilibili_space_url as string);
  const importedIDs = importedCreators.map((creator) => creator.id);

  useEffect(() => {
    if (!directoryID && directories[0]) {
      setDirectoryID(directories[0].id);
    }
  }, [directories, directoryID]);

  useEffect(() => {
    dialogRef.current?.focus();
  }, []);

  const saveDirectory = useMutation({
    mutationFn: api.saveDirectory,
    onSuccess: (data) => {
      setSavedDirectories((current) => [data.directory, ...current.filter((directory) => directory.id !== data.directory.id)]);
      setDirectoryID(data.directory.id);
      setDirectoryPath(data.directory.path);
      queryClient.invalidateQueries({ queryKey: ['directories'] });
    },
  });

  const preview = useMutation({
    mutationFn: () => api.previewBulkImportCreators(directoryID, text),
    onSuccess: (data) => {
      setItems(data.items);
      setImportedCreators([]);
    },
  });
  const submit = useMutation({
    mutationFn: () => api.bulkImportCreators(directoryID, validProfileURLs),
    onSuccess: (data) => {
      setImportedCreators(data.creators);
      onImported(data.creators.map((creator) => creator.id));
    },
  });
  async function enqueueImported() {
    setActionMessage(null);
    try {
      await onImmediateAsync(importedIDs);
      setActionMessage({ type: 'success', text: `已加入立即更新队列：${importedIDs.length} 个UP主` });
    } catch (cause) {
      setActionMessage({ type: 'error', text: errorMessage(cause) });
    }
  }

  return (
    <div
      ref={dialogRef}
      className="modal-backdrop"
      role="dialog"
      aria-label="批量新增关注"
      tabIndex={-1}
      onKeyDown={(event) => {
        if (event.key === 'Escape') onClose();
      }}
    >
      <div className="modal bulk-import-modal">
        <div className="modal-head">
          <div>
            <h2>批量新增关注</h2>
            <p>粘贴B站主页或分享文案，解析后可从 0 加入更新队列。</p>
          </div>
          <button type="button" className="icon-button" onClick={onClose} title="关闭" aria-label="关闭弹窗">
            <X size={18} />
          </button>
        </div>
        <div className="bulk-directory-picker">
          <label className="field-block">
            <span>保存目录路径</span>
            <input
              aria-label="保存目录路径"
              value={directoryPath}
              onChange={(event) => setDirectoryPath(event.target.value)}
              placeholder="/Users/ted/Documents/LocalBilibili/bilibili"
            />
          </label>
          <button type="button" onClick={() => setBrowserOpen(true)} title="浏览目录">
            <FolderSearch size={18} /> 浏览
          </button>
          <button type="button" disabled={!directoryPath.trim() || saveDirectory.isPending} onClick={() => saveDirectory.mutate(directoryPath.trim())}>
            保存目录
          </button>
        </div>
        {saveDirectory.error && <p className="error-line">{saveDirectory.error.message}</p>}
        <div className="bulk-selected-directory">
          当前目录：{selectedDirectory ? `${selectedDirectory.name} · ${selectedDirectory.path}` : '请先保存或选择一个目录'}
        </div>
        <label className="field-block">
          <span>批量关注文本</span>
          <textarea
            className="bulk-import-textarea"
            aria-label="批量关注文本"
            value={text}
            onChange={(event) => setText(event.target.value)}
            placeholder="每行粘贴一个主页链接或B站分享文案"
          />
        </label>
        <div className="modal-actions">
          <button type="button" onClick={onClose}>取消</button>
          <button type="button" disabled={!directoryID || !text.trim() || preview.isPending} onClick={() => preview.mutate()}>解析</button>
          <button type="button" disabled={validProfileURLs.length === 0 || submit.isPending} onClick={() => submit.mutate()}>提交</button>
        </div>
        {items.length > 0 && (
          <div className="bulk-import-results">
            <div className="bulk-import-row bulk-import-head">
              <span>原始内容</span>
              <span>UP主昵称</span>
              <span>主页地址</span>
              <span>状态</span>
            </div>
            {items.map((item, index) => (
              <div className="bulk-import-row" key={`${item.input}-${index}`}>
                <span title={item.input}>{item.input}</span>
                <span title={item.bio || item.display_name || ''}>{item.display_name || '-'}</span>
                <span>{item.bilibili_space_url || '-'}</span>
                <span>{item.status === 'ready' ? (item.existing ? '已存在' : '可导入') : item.error}</span>
              </div>
            ))}
          </div>
        )}
        {importedCreators.length > 0 && (
          <div className="bulk-import-success">
            <strong>已导入 {importedCreators.length} 个UP主</strong>
            <p>下载完成后会自动入库，可在关注页或 Feed 中查看。</p>
            {actionMessage && <p className={actionMessage.type === 'success' ? 'success-line' : 'error-line'}>{actionMessage.text}</p>}
            <div className="modal-actions">
              <button type="button" onClick={enqueueImported}>立即更新</button>
              <button type="button" onClick={() => onSubscribe(importedIDs)}>添加到定时更新队列</button>
            </div>
          </div>
        )}
      </div>
      <DirectoryBrowser
        open={browserOpen}
        onClose={() => setBrowserOpen(false)}
        onSelect={(selected) => {
          setDirectoryPath(selected);
          setBrowserOpen(false);
        }}
      />
    </div>
  );
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : '操作失败';
}

function SubscribeDialog({
  queues,
  creatorIds,
  onClose,
  onSubmit,
  pending,
}: {
  queues: UpdateQueue[];
  creatorIds: number[];
  onClose: () => void;
  onSubmit: (queueIds: number[]) => void;
  pending: boolean;
}) {
  const defaultQueues = queues.filter((queue) => queue.is_default).map((queue) => queue.id);
  const [checked, setChecked] = useState<number[]>(defaultQueues.length > 0 ? defaultQueues : queues.slice(0, 1).map((queue) => queue.id));
  const dialogRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    dialogRef.current?.focus();
  }, []);

  return (
    <div
      ref={dialogRef}
      className="modal-backdrop"
      role="dialog"
      aria-label="订阅更新"
      tabIndex={-1}
      onKeyDown={(event) => {
        if (event.key === 'Escape') onClose();
      }}
    >
      <div className="modal">
        <div className="modal-head">
          <div>
            <h2>订阅更新</h2>
            <p>选择要加入的定时更新队列，共 {creatorIds.length} 个UP主。</p>
          </div>
          <button type="button" className="icon-button" onClick={onClose} title="关闭" aria-label="关闭弹窗">
            <X size={18} />
          </button>
        </div>
        <div className="queue-choice-list">
          {queues.map((queue) => (
            <label key={queue.id}>
              <input
                type="checkbox"
                checked={checked.includes(queue.id)}
                onChange={(event) => setChecked((current) => (event.target.checked ? [...current, queue.id] : current.filter((id) => id !== queue.id)))}
              />
              <span>{queue.name}</span>
              <small>{queue.cron}{queue.is_default ? ' · 默认' : ''}</small>
            </label>
          ))}
          {queues.length === 0 && <div className="empty-state">还没有定时队列。</div>}
        </div>
        <div className="modal-actions">
          <button type="button" onClick={onClose}>取消</button>
          <button type="button" disabled={checked.length === 0 || creatorIds.length === 0 || pending} onClick={() => onSubmit(checked)}>保存订阅</button>
        </div>
      </div>
    </div>
  );
}

function DeleteCreatorDialog({
  creator,
  pending,
  onClose,
  onConfirm,
}: {
  creator: Creator;
  pending: boolean;
  onClose: () => void;
  onConfirm: (deleteMedia: boolean) => void;
}) {
  const [deleteMedia, setDeleteMedia] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    dialogRef.current?.focus();
  }, []);

  return (
    <div
      ref={dialogRef}
      className="modal-backdrop"
      role="dialog"
      aria-label="删除UP主"
      tabIndex={-1}
      onKeyDown={(event) => {
        if (event.key === 'Escape' && !pending) onClose();
      }}
    >
      <div className="modal delete-creator-modal">
        <div className="modal-head">
          <div>
            <h2>删除UP主</h2>
            <p>确定删除UP主「{creator.name}」？该UP主当前有 {creator.work_count} 个作品。</p>
          </div>
          <button type="button" className="icon-button" onClick={onClose} title="关闭" aria-label="关闭弹窗" disabled={pending}>
            <X size={18} />
          </button>
        </div>
        <label className="danger-check">
          <input type="checkbox" checked={deleteMedia} onChange={(event) => setDeleteMedia(event.target.checked)} />
          <span>删除媒体数据</span>
        </label>
        <p className="danger-note">
          不勾选时只删除数据库索引。勾选后会删除保存目录下该UP主的整个本地目录，磁盘文件不可从应用内恢复。
        </p>
        <div className="modal-actions">
          <button type="button" onClick={onClose} disabled={pending}>取消</button>
          <button type="button" className="danger" disabled={pending} onClick={() => onConfirm(deleteMedia)}>确认删除</button>
        </div>
      </div>
    </div>
  );
}

function CreatorLinksDialog({
  creator,
  onClose,
  onSaved,
}: {
  creator: Creator;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [search, setSearch] = useState('');
  const links = useQuery({
    queryKey: ['creator-links', creator.id],
    queryFn: () => api.creatorLinks(creator.id),
  });
  const [checked, setChecked] = useState<number[]>([]);

  useEffect(() => {
    setChecked((links.data?.linked ?? []).map((item) => item.id));
  }, [links.data]);

  const save = useMutation({
    mutationFn: () => api.saveCreatorLinks(creator.id, checked),
    onSuccess: () => {
      onSaved();
      onClose();
    },
  });

  const candidates = (links.data?.candidates ?? []).filter((item) => {
    if (!search.trim()) return true;
    const keyword = search.trim().toLowerCase();
    return item.name.toLowerCase().includes(keyword);
  });

  return (
    <div className="modal-backdrop" role="dialog" aria-label="关联创作者">
      <div className="modal creator-links-modal">
        <div className="modal-head">
          <div>
            <h2>关联创作者</h2>
            <p>把同一创作者的多个目录或马甲账号合并到同一个播放组。</p>
          </div>
          <button type="button" className="icon-button" onClick={onClose} title="关闭">
            <X size={18} />
          </button>
        </div>
        <div className="creator-links-target">
          当前UP主：<strong>{creator.name}</strong>
        </div>
        <label className="page-search compact-search">
          <Search size={16} />
          <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索可关联UP主" aria-label="搜索可关联UP主" />
        </label>
        <div className="creator-links-list">
          {candidates.map((item) => (
            <label key={item.id} className="creator-links-row">
              <input
                type="checkbox"
                checked={checked.includes(item.id)}
                onChange={(event) =>
                  setChecked((current) =>
                    event.target.checked ? [...current, item.id] : current.filter((id) => id !== item.id),
                  )
                }
              />
              <span>{item.name}</span>
              <small>{item.work_count} 个作品</small>
            </label>
          ))}
          {!links.isLoading && candidates.length === 0 && <div className="empty-state">没有可关联的UP主。</div>}
        </div>
        <div className="modal-actions">
          <button type="button" onClick={onClose}>
            取消
          </button>
          <button type="button" disabled={save.isPending} onClick={() => save.mutate()}>
            保存关联
          </button>
        </div>
      </div>
    </div>
  );
}
