import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { FolderHeart, UserRound } from 'lucide-react';
import { useState } from 'react';
import type { ReactNode } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../lib/api';
import type { FavoriteFolderType } from '../lib/types';

export function FavoriteWorksPage() {
  const params = useParams();
  const folderId = params.folderId ? Number(params.folderId) : undefined;
  return (
    <FavoriteLayout type="work" title="作品收藏夹" folderId={folderId}>
      {folderId && <FavoriteWorkGrid folderId={folderId} />}
    </FavoriteLayout>
  );
}

export function FavoriteCreatorsPage() {
  const params = useParams();
  const folderId = params.folderId ? Number(params.folderId) : undefined;
  return (
    <FavoriteLayout type="creator" title="作者收藏夹" folderId={folderId}>
      {folderId && <FavoriteCreatorGrid folderId={folderId} />}
    </FavoriteLayout>
  );
}

function FavoriteLayout({ type, title, folderId, children }: { type: FavoriteFolderType; title: string; folderId?: number; children: ReactNode }) {
  const queryClient = useQueryClient();
  const [newName, setNewName] = useState('');
  const folders = useQuery({ queryKey: ['favorite-folders', type], queryFn: () => api.favoriteFolders(type) });
  const folderList = folders.data?.folders ?? [];
  const basePath = type === 'work' ? '/favorite-works' : '/favorite-creators';
  const createFolder = useMutation({
    mutationFn: () => api.createFavoriteFolder(type, newName),
    onSuccess: () => {
      setNewName('');
      void queryClient.invalidateQueries({ queryKey: ['favorite-folders', type] });
    },
  });
  const updateFolder = useMutation({
    mutationFn: ({ id, name, isDefault }: { id: number; name?: string; isDefault?: boolean }) => api.updateFavoriteFolder(id, { name, is_default: isDefault }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['favorite-folders', type] }),
  });

  return (
    <div className="page narrow-page">
      <header className="page-header">
        <h1>{title}</h1>
        <p>本地收藏目录，只保存索引，不移动原始文件。</p>
      </header>
      <section className="favorite-manager">
        <div className="favorite-folder-create">
          <input value={newName} onChange={(event) => setNewName(event.target.value)} placeholder="新收藏夹名称" aria-label="新收藏夹名称" />
          <button type="button" onClick={() => createFolder.mutate()} disabled={!newName.trim()}>
            创建收藏夹
          </button>
        </div>
        <div className="favorite-folder-list">
          {folderList.map((folder) => (
            <div key={folder.id} className={`favorite-folder-row ${folder.id === folderId ? 'active' : ''}`}>
              <Link to={`${basePath}/${folder.id}`}>
                <FolderHeart size={18} />
                {folder.name}
                {folder.is_default ? <span>默认</span> : null}
              </Link>
              <button type="button" onClick={() => updateFolder.mutate({ id: folder.id, name: window.prompt('收藏夹名称', folder.name) ?? folder.name })}>
                重命名
              </button>
              <button type="button" onClick={() => updateFolder.mutate({ id: folder.id, isDefault: true })} disabled={folder.is_default}>
                设为默认
              </button>
            </div>
          ))}
        </div>
      </section>
      {folderId ? children : <div className="empty-state">选择一个收藏夹查看内容。</div>}
    </div>
  );
}

function FavoriteWorkGrid({ folderId }: { folderId: number }) {
  const works = useQuery({ queryKey: ['favorite-works', folderId], queryFn: () => api.favoriteWorks(folderId) });
  const workList = works.data?.works ?? [];
  return (
    <div className="work-grid">
      {workList.map((work) => {
        const cover = work.media.find((item) => item.type === 'image') ?? work.media[0];
        return (
          <Link key={work.id} className="work-card" to={`/favorite-works/${folderId}/play?work_id=${work.id}`}>
            {cover?.type === 'image' ? <img src={cover.url} alt={work.title} /> : <div className="work-video-cover">视频</div>}
            <strong>{work.title || work.description || '本地作品'}</strong>
            <span>@{work.creator_name}</span>
          </Link>
        );
      })}
      {!works.isLoading && workList.length === 0 && <div className="empty-state">这个作品收藏夹还没有内容。</div>}
    </div>
  );
}

function FavoriteCreatorGrid({ folderId }: { folderId: number }) {
  const creators = useQuery({ queryKey: ['favorite-creators', folderId], queryFn: () => api.favoriteCreators(folderId) });
  const creatorList = creators.data?.creators ?? [];
  return (
    <div className="creator-grid">
      {creatorList.map((creator) => (
        <Link className="creator-card" key={creator.id} to={`/creator/${creator.id}`}>
          <div className="avatar">
            <UserRound size={24} />
          </div>
          <div>
            <strong>{creator.name}</strong>
            <span>{creator.work_count} 个作品</span>
          </div>
        </Link>
      ))}
      {!creators.isLoading && creatorList.length === 0 && <div className="empty-state">这个作者收藏夹还没有内容。</div>}
    </div>
  );
}
