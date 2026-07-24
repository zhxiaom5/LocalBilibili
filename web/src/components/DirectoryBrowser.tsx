import { ChevronLeft, Folder, X } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { api } from '../lib/api';

export function DirectoryBrowser({
  open,
  onClose,
  onSelect,
}: {
  open: boolean;
  onClose: () => void;
  onSelect: (path: string) => void;
}) {
  const roots = useQuery({ queryKey: ['fs-roots'], queryFn: api.roots, enabled: open });
  const [currentPath, setCurrentPath] = useState('');
  const list = useQuery({
    queryKey: ['fs-list', currentPath],
    queryFn: () => api.listFS(currentPath),
    enabled: open && currentPath.length > 0,
  });

  if (!open) return null;
  const entries = currentPath ? list.data?.entries ?? [] : roots.data?.roots ?? [];

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true">
      <div className="directory-modal">
        <header>
          <button className="icon-button" onClick={() => setCurrentPath('')} title="返回根目录">
            <ChevronLeft size={18} />
          </button>
          <strong>{currentPath || '选择目录'}</strong>
          <button className="icon-button" onClick={onClose} title="关闭">
            <X size={18} />
          </button>
        </header>
        <div className="directory-list">
          {entries.map((entry) => (
            <button key={entry.path} onClick={() => setCurrentPath(entry.path)}>
              <Folder size={18} />
              <span>{entry.name}</span>
              <small>{entry.path}</small>
            </button>
          ))}
        </div>
        <footer>
          <button disabled={!currentPath} onClick={() => currentPath && onSelect(currentPath)}>
            选择此目录
          </button>
        </footer>
      </div>
    </div>
  );
}
