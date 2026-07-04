import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderSearch, Play, RotateCw, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { DirectoryBrowser } from "../components/DirectoryBrowser";
import { ProgressPanel } from "../components/ProgressPanel";
import { api } from "../lib/api";

export function SettingsPage() {
  const [path, setPath] = useState("");
  const [databaseDirectory, setDatabaseDirectory] = useState("");
  const [browserOpen, setBrowserOpen] = useState(false);
  const queryClient = useQueryClient();

  const directories = useQuery({
    queryKey: ["directories"],
    queryFn: api.directories,
  });
  const status = useQuery({
    queryKey: ["scan-status"],
    queryFn: api.scanStatus,
    refetchInterval: 3000,
  });
  const database = useQuery({
    queryKey: ["database-config"],
    queryFn: api.databaseConfig,
  });
  useEffect(() => {
    const directory = database.data?.database?.directory;
    if (directory) {
      setDatabaseDirectory(directory);
    }
  }, [database.data]);
  const directoryList = directories.data?.directories ?? [];
  const save = useMutation({
    mutationFn: api.saveDirectory,
    onSuccess: () => {
      setPath("");
      queryClient.invalidateQueries({ queryKey: ["directories"] });
    },
  });
  const remove = useMutation({
    mutationFn: api.deleteDirectory,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["directories"] });
      queryClient.invalidateQueries({ queryKey: ["creators"] });
      queryClient.invalidateQueries({ queryKey: ["feed"] });
    },
  });
  const scanOne = useMutation({ mutationFn: api.scanDirectory });
  const scanAll = useMutation({ mutationFn: api.scanAll });
  const saveDatabase = useMutation({
    mutationFn: api.saveDatabaseDirectory,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["database-config"] });
      queryClient.invalidateQueries({ queryKey: ["directories"] });
    },
  });
  return (
    <div className="page settings-page">
      <header className="page-header">
        <h1>目录配置</h1>
        <p>选择运行服务机器上的本地作品目录</p>
      </header>

      <section className="settings-card">
        <div className="account-card-head">
          <FolderSearch size={18} />
          <div>
            <h2>数据库位置</h2>
            <p>切换 SQLite 的存放目录，适合把数据放到更稳定的位置。</p>
          </div>
        </div>
        <form
          className="path-form"
          onSubmit={(event) => {
            event.preventDefault();
            if (databaseDirectory.trim())
              saveDatabase.mutate(databaseDirectory.trim());
          }}
        >
          <input
            value={databaseDirectory}
            onChange={(event) => setDatabaseDirectory(event.target.value)}
            placeholder="/Users/ted/Documents/LocalBilibili"
          />
          <button
            type="submit"
            disabled={saveDatabase.isPending || !databaseDirectory.trim()}
          >
            保存数据库目录
          </button>
        </form>
        {database.data?.database && (
          <p className="hint-line">
            当前数据库：{database.data.database.path} ·{" "}
            {database.data.database.writable ? "可写" : "不可写"}
          </p>
        )}
        {database.error && (
          <p className="error-line">{database.error.message}</p>
        )}
        {saveDatabase.error && (
          <p className="error-line">{saveDatabase.error.message}</p>
        )}
      </section>

      <section className="settings-layout">
        <div className="settings-main">
          <form
            className="path-form"
            onSubmit={(event) => {
              event.preventDefault();
              if (path.trim()) save.mutate(path.trim());
            }}
          >
            <input
              value={path}
              onChange={(event) => setPath(event.target.value)}
              placeholder="/Users/ted/Documents/LocalBilibili/bilibili"
            />
            <button
              type="button"
              onClick={() => setBrowserOpen(true)}
              title="浏览目录"
            >
              <FolderSearch size={18} /> 浏览
            </button>
            <button type="submit">保存</button>
          </form>
          {save.error && <p className="error-line">{save.error.message}</p>}

          <div className="directory-table">
            {directoryList.map((directory) => (
              <div className="directory-row" key={directory.id}>
                <div>
                  <strong>{directory.name}</strong>
                  <span>{directory.path}</span>
                  <small>
                    {directory.creator_count} UP主 · {directory.work_count} 作品
                    · {directory.status}
                  </small>
                </div>
                <button
                  onClick={() => scanOne.mutate(directory.id)}
                  title="扫描此目录"
                >
                  <Play size={17} /> 扫描
                </button>
                <button
                  className="danger"
                  onClick={() => remove.mutate(directory.id)}
                  title="删除目录"
                >
                  <Trash2 size={17} />
                </button>
              </div>
            ))}
          </div>
          <button
            className="scan-all"
            onClick={() => scanAll.mutate()}
            disabled={directoryList.length === 0}
          >
            <RotateCw size={18} /> 扫描全部目录
          </button>
        </div>
        <ProgressPanel status={status.data?.status} />
      </section>

      <DirectoryBrowser
        open={browserOpen}
        onClose={() => setBrowserOpen(false)}
        onSelect={(selected) => {
          setPath(selected);
          setBrowserOpen(false);
        }}
      />
    </div>
  );
}
