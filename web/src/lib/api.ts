import type {
  AboutInfo,
  BulkImportItem,
  CreatorLinks,
  Creator,
  DatabaseConfig,
  Diagnostics,
  Directory,
  FavoriteFolder,
  FavoriteFolderType,
  FavoriteStatus,
  FSEntry,
  LogEventsPage,
  LearningCheck,
  CodexLoginStatus,
  LearningConfig,
  LearningJob,
  LearningQueue,
  LearningStatus,
  RuntimeConfig,
  ScanStatus,
  SubtitleCheck,
  SubtitleConfig,
  SubtitleJobsPage,
  SubtitleQueue,
  SubtitleStatus,
  UpdateCookie,
  UpdateJobsPage,
  UpdateQueue,
  UpdateStatus,
  User,
  ViewContext,
  Work,
} from "./types";

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = "ApiError";
  }
}

const unauthorizedEventName = "localbilibili:unauthorized";

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(options?.headers ?? {}),
    },
    ...options,
  });
  if (!response.ok) {
    const body = await response
      .json()
      .catch(() => ({ error: response.statusText }));
    if (response.status === 401 && !path.startsWith("/api/auth/")) {
      window.dispatchEvent(new Event(unauthorizedEventName));
    }
    throw new ApiError(response.status, body.error ?? response.statusText);
  }
  return response.json() as Promise<T>;
}

export const api = {
  me: () => request<{ user: User }>("/api/auth/me"),
  login: (username: string, password: string) =>
    request<{ user: User }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () =>
    request<{ ok: boolean }>("/api/auth/logout", { method: "POST" }),
  changePassword: (oldPassword: string, newPassword: string) =>
    request<{ user: User }>("/api/auth/password", {
      method: "POST",
      body: JSON.stringify({
        old_password: oldPassword,
        new_password: newPassword,
      }),
    }),
  users: () => request<{ users: User[] }>("/api/users"),
  createUser: (username: string, password: string) =>
    request<{ user: User }>("/api/users", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  resetUserPassword: (id: number, password: string) =>
    request<{ user: User }>(`/api/users/${id}/reset-password`, {
      method: "POST",
      body: JSON.stringify({ password }),
    }),
  deleteUser: (id: number) =>
    request<{ ok: boolean }>(`/api/users/${id}`, { method: "DELETE" }),
  viewContext: () => request<ViewContext>("/api/users/view-context"),
  setViewContext: (userId: number) =>
    request<ViewContext>("/api/users/view-context", {
      method: "PUT",
      body: JSON.stringify({ user_id: userId }),
    }),
  clearViewContext: () =>
    request<ViewContext>("/api/users/view-context", { method: "DELETE" }),
  about: () => request<{ about: AboutInfo }>("/api/about"),
  auditLogs: (params: { page?: number; pageSize?: number } = {}) =>
    request<LogEventsPage>(`/api/logs/audit?${logQuery(params).toString()}`),
  systemLogs: (params: { page?: number; pageSize?: number } = {}) =>
    request<LogEventsPage>(`/api/logs/system?${logQuery(params).toString()}`),
  diagnostics: () => request<{ diagnostics: Diagnostics }>("/api/diagnostics"),

  roots: () => request<{ roots: FSEntry[] }>("/api/fs/roots"),
  listFS: (path: string) =>
    request<{ entries: FSEntry[] }>(
      `/api/fs/list?path=${encodeURIComponent(path)}`,
    ),
  databaseConfig: () =>
    request<{ database: DatabaseConfig }>("/api/config/database"),
  saveDatabaseDirectory: (directory: string) =>
    request<{ database: DatabaseConfig }>("/api/config/database", {
      method: "PUT",
      body: JSON.stringify({ directory }),
    }),
  runtimeConfig: () => request<{ runtime: RuntimeConfig }>("/api/config/runtime"),
  saveRuntimeConfig: (downloaderConcurrency: number) =>
    request<{ runtime: RuntimeConfig }>("/api/config/runtime", {
      method: "PUT",
      body: JSON.stringify({ downloader_concurrency: downloaderConcurrency }),
    }),
  directories: () => request<{ directories: Directory[] }>("/api/directories"),
  saveDirectory: (path: string) =>
    request<{ directory: Directory }>("/api/directories", {
      method: "POST",
      body: JSON.stringify({ path }),
    }),
  deleteDirectory: (id: number) =>
    request<{ ok: boolean }>(`/api/directories/${id}`, {
      method: "DELETE",
    }),
  scanDirectory: (id: number) =>
    request<{ status: ScanStatus }>(`/api/directories/${id}/scan`, {
      method: "POST",
    }),
  scanAll: () =>
    request<{ status: ScanStatus }>("/api/directories/scan-all", {
      method: "POST",
    }),
  scanStatus: () => request<{ status: ScanStatus }>("/api/scan/status"),
  creators: (
    search = "",
    params: {
      sort?: "updated_at" | "name" | "work_count" | "views";
      order?: "asc" | "desc";
      minWorkCount?: number;
    } = {},
  ) => {
    const query = new URLSearchParams();
    if (search.trim()) query.set("search", search.trim());
    if (params.sort) query.set("sort", params.sort);
    if (params.order) query.set("order", params.order);
    if (params.minWorkCount !== undefined) query.set("min_work_count", String(params.minWorkCount));
    return request<{ creators: Creator[] }>(
      `/api/creators${query.size ? `?${query.toString()}` : ""}`,
    );
  },
  creatorLinks: (id: number) =>
    request<CreatorLinks>(`/api/creators/${id}/links`),
  deleteCreator: (id: number, deleteMedia: boolean) =>
    request<{ ok: boolean }>(`/api/creators/${id}`, {
      method: "DELETE",
      body: JSON.stringify({ delete_media: deleteMedia }),
    }),
  saveCreatorLinks: (id: number, creatorIds: number[]) =>
    request<CreatorLinks>(`/api/creators/${id}/links`, {
      method: "PUT",
      body: JSON.stringify({ creator_ids: creatorIds }),
    }),
  previewBulkImportCreators: (directoryId: number, text: string) =>
    request<{ items: BulkImportItem[] }>("/api/creators/bulk-import/preview", {
      method: "POST",
      body: JSON.stringify({ directory_id: directoryId, text }),
    }),
  bulkImportCreators: (directoryId: number, profileUrls: string[]) =>
    request<{ creators: Creator[]; items?: BulkImportItem[] }>("/api/creators/bulk-import", {
      method: "POST",
      body: JSON.stringify({
        directory_id: directoryId,
        bilibili_space_urls: profileUrls,
      }),
    }),
  resolveCreatorPlaceholders: () =>
    request<{ creators: Creator[]; items: BulkImportItem[] }>("/api/creators/resolve-placeholders", {
      method: "POST",
    }),
  updateStatus: () => request<{ status: UpdateStatus }>("/api/update/status"),
  updateJobs: (params: { page?: number; pageSize?: number; status?: string } = {}) =>
    request<UpdateJobsPage>(`/api/update/jobs?${logQuery(params).toString()}`),
  deleteUpdateJob: (id: number) =>
    request<{ ok: boolean }>(`/api/update/jobs/${id}`, { method: "DELETE" }),
  enqueueImmediateUpdate: (creatorIds: number[]) =>
    request<{ status: UpdateStatus }>("/api/update/immediate", {
      method: "POST",
      body: JSON.stringify({ creator_ids: creatorIds }),
    }),
  resetImmediateUpdate: () =>
    request<{ status: UpdateStatus }>("/api/update/immediate/reset", {
      method: "POST",
    }),
  retryUpdateJob: (id: number) =>
    request<{ status: UpdateStatus }>(`/api/update/jobs/${id}/retry`, {
      method: "POST",
    }),
  updateQueues: () => request<{ queues: UpdateQueue[] }>("/api/update/queues"),
  createUpdateQueue: (name: string, cron: string) =>
    request<{ queue: UpdateQueue }>("/api/update/queues", {
      method: "POST",
      body: JSON.stringify({ name, cron }),
    }),
  updateQueue: (
    id: number,
    body: {
      name?: string;
      cron?: string;
      is_default?: boolean;
      enabled?: boolean;
    },
  ) =>
    request<{ queue: UpdateQueue }>(`/api/update/queues/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  deleteUpdateQueue: (id: number) =>
    request<{ ok: boolean }>(`/api/update/queues/${id}`, { method: "DELETE" }),
  updateQueueCreators: (id: number) =>
    request<{ creators: Creator[] }>(`/api/update/queues/${id}/creators`),
  addUpdateQueueCreators: (id: number, creatorIds: number[]) =>
    request<{ ok: boolean }>(`/api/update/queues/${id}/creators`, {
      method: "POST",
      body: JSON.stringify({ creator_ids: creatorIds }),
    }),
  removeUpdateQueueCreator: (queueId: number, creatorId: number) =>
    request<{ ok: boolean }>(
      `/api/update/queues/${queueId}/creators/${creatorId}`,
      { method: "DELETE" },
    ),
  runUpdateQueueNow: (id: number) =>
    request<{ status: UpdateStatus }>(`/api/update/queues/${id}/run-now`, {
      method: "POST",
    }),
  updateCookie: () => request<{ cookie: UpdateCookie }>("/api/update/cookie"),
  saveUpdateCookie: (cookie: string) =>
    request<{ cookie: UpdateCookie }>("/api/update/cookie", {
      method: "PATCH",
      body: JSON.stringify({ cookie }),
    }),
  validateUpdateCookie: () =>
    request<{ cookie: UpdateCookie; ok: boolean }>(
      "/api/update/cookie/validate",
      { method: "POST" },
    ),
  subtitleConfig: () => request<{ config: SubtitleConfig }>("/api/subtitles/config"),
  saveSubtitleConfig: (config: SubtitleConfig) =>
    request<{ config: SubtitleConfig }>("/api/subtitles/config", {
      method: "PUT",
      body: JSON.stringify(config),
    }),
  checkSubtitles: (config?: SubtitleConfig) =>
    request<{ check: SubtitleCheck }>("/api/subtitles/check", {
      method: "POST",
      ...(config ? { body: JSON.stringify(config) } : {}),
    }),
  subtitleStatus: () => request<{ status: SubtitleStatus }>("/api/subtitles/status"),
  subtitleJobs: (params: { page?: number; pageSize?: number } = {}) =>
    request<SubtitleJobsPage>(`/api/subtitles/jobs?${logQuery(params).toString()}`),
  deleteSubtitleJob: (id: number) =>
    request<{ ok: boolean }>(`/api/subtitles/jobs/${id}`, { method: "DELETE" }),
  retrySubtitleJob: (id: number) =>
    request<{ status: SubtitleStatus }>(`/api/subtitles/jobs/${id}/retry`, { method: "POST" }),
  enqueueImmediateSubtitles: (creatorIds: number[], source = "manual") =>
    request<{ status: SubtitleStatus }>("/api/subtitles/immediate", {
      method: "POST",
      body: JSON.stringify({ creator_ids: creatorIds, source }),
    }),
  enqueueWorkSubtitle: (workId: number, source = "manual") =>
    request<{ status: SubtitleStatus }>("/api/subtitles/immediate", {
      method: "POST",
      body: JSON.stringify({ work_ids: [workId], source }),
    }),
  resetImmediateSubtitles: () =>
    request<{ status: SubtitleStatus }>("/api/subtitles/immediate/reset", { method: "POST" }),
  scanExistingSubtitles: () =>
    request<{ status: SubtitleStatus; creator_count: number }>("/api/subtitles/scan-existing", { method: "POST" }),
  subtitleQueues: () => request<{ queues: SubtitleQueue[] }>("/api/subtitles/queues"),
  createSubtitleQueue: (name: string, cron: string) =>
    request<{ queue: SubtitleQueue }>("/api/subtitles/queues", {
      method: "POST",
      body: JSON.stringify({ name, cron }),
    }),
  updateSubtitleQueue: (
    id: number,
    body: { name?: string; cron?: string; is_default?: boolean; enabled?: boolean },
  ) =>
    request<{ queue: SubtitleQueue }>(`/api/subtitles/queues/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  deleteSubtitleQueue: (id: number) =>
    request<{ ok: boolean }>(`/api/subtitles/queues/${id}`, { method: "DELETE" }),
  subtitleQueueCreators: (id: number) =>
    request<{ creators: Creator[] }>(`/api/subtitles/queues/${id}/creators`),
  addSubtitleQueueCreators: (id: number, creatorIds: number[]) =>
    request<{ ok: boolean }>(`/api/subtitles/queues/${id}/creators`, {
      method: "POST",
      body: JSON.stringify({ creator_ids: creatorIds }),
    }),
  removeSubtitleQueueCreator: (queueId: number, creatorId: number) =>
    request<{ ok: boolean }>(`/api/subtitles/queues/${queueId}/creators/${creatorId}`, { method: "DELETE" }),
  runSubtitleQueueNow: (id: number) =>
    request<{ status: SubtitleStatus }>(`/api/subtitles/queues/${id}/run-now`, { method: "POST" }),
  creatorSubtitleLink: (id: number) =>
    request<{ url: string }>(`/api/subtitles/links/creators/${id}`),
  workSubtitleLink: (id: number) =>
    request<{ url: string }>(`/api/subtitles/links/works/${id}`),
  learningConfig: () => request<{ config: LearningConfig }>("/api/learning/config"),
  saveLearningConfig: (config: LearningConfig) => request<{ config: LearningConfig }>("/api/learning/config", { method: "PUT", body: JSON.stringify(config) }),
  checkLearning: () => request<{ check: LearningCheck }>("/api/learning/check", { method: "POST" }),
  checkWiseMapping: () => request<{ ok: boolean }>("/api/learning/wisemapping/check", { method: "POST" }),
  codexLoginStatus: () => request<{ login: CodexLoginStatus }>("/api/learning/codex/login"),
  startCodexLogin: () => request<{ login: CodexLoginStatus }>("/api/learning/codex/login", { method: "POST" }),
  cancelCodexLogin: () => request<{ login: CodexLoginStatus }>("/api/learning/codex/login/cancel", { method: "POST" }),
  logoutCodex: () => request<{ login: CodexLoginStatus }>("/api/learning/codex/logout", { method: "POST" }),
  learningStatus: () => request<{ status: LearningStatus }>("/api/learning/status"),
  learningJobs: () => request<{ jobs: LearningJob[] }>("/api/learning/jobs"),
  learningRuns: () => request<{ runs: import('./types').LearningRun[] }>("/api/learning/runs"),
  enqueueImmediateLearning: (creatorIds: number[] = [], workIds: number[] = [], source = "manual") => request<{ status: LearningStatus }>("/api/learning/immediate", { method: "POST", body: JSON.stringify({ creator_ids: creatorIds, work_ids: workIds, source }) }),
  resetImmediateLearning: () => request<{ status: LearningStatus }>("/api/learning/immediate/reset", { method: "POST" }),
  retryLearningJob: (id: number) => request<{ status: LearningStatus }>(`/api/learning/jobs/${id}/retry`, { method: "POST" }),
  deleteLearningJob: (id: number) => request<{ ok: boolean }>(`/api/learning/jobs/${id}`, { method: "DELETE" }),
  learningQueues: () => request<{ queues: LearningQueue[] }>("/api/learning/queues"),
  createLearningQueue: (name: string, cron: string) => request<{ queue: LearningQueue }>("/api/learning/queues", { method: "POST", body: JSON.stringify({ name, cron }) }),
  addLearningQueueCreators: (id: number, creatorIds: number[]) => request<{ ok: boolean }>(`/api/learning/queues/${id}/creators`, { method: "POST", body: JSON.stringify({ creator_ids: creatorIds }) }),
  runLearningQueueNow: (id: number) => request<{ status: LearningStatus }>(`/api/learning/queues/${id}/run-now`, { method: "POST" }),
  creatorMindMapLink: (id: number) => request<{ url: string }>(`/api/learning/links/creators/${id}`),
  workMindMapLink: (id: number) => request<{ url: string; creator_url?: string }>(`/api/learning/links/works/${id}`),
  feed: (
    params: {
      creatorId?: number;
      directoryId?: number;
      favoriteFolderId?: number;
      search?: string;
      order?: "random" | "desc" | "asc";
	      recentMode?: "three_days" | "week" | "works";
      limit?: number;
    } = {},
  ) => {
    const query = new URLSearchParams();
    query.set("mode", "random");
    query.set("limit", String(params.limit ?? 30));
    query.set("order", params.order ?? "random");
	    if (params.recentMode) query.set("recent_mode", params.recentMode);
    if (params.creatorId) query.set("creator_id", String(params.creatorId));
    if (params.directoryId)
      query.set("directory_id", String(params.directoryId));
    if (params.favoriteFolderId)
      query.set("favorite_folder_id", String(params.favoriteFolderId));
    if (params.search?.trim()) query.set("search", params.search.trim());
    return request<{ works: Work[] }>(`/api/feed?${query.toString()}`);
  },
  work: (id: number) => request<{ work: Work }>(`/api/works/${id}`),
  recordWorkView: (
    id: number,
    body: {
      eventType?: string;
      positionSec?: number;
      durationSec?: number;
      mediaType?: string;
      detail?: string;
    } = {},
  ) =>
    request<{ ok: boolean }>(`/api/works/${id}/view`, {
      method: "POST",
      body: JSON.stringify({
        event_type: body.eventType,
        position_sec: body.positionSec,
        duration_sec: body.durationSec,
        media_type: body.mediaType,
        detail: body.detail,
      }),
    }),
  favoriteFolders: (type: FavoriteFolderType) =>
    request<{ folders: FavoriteFolder[] }>(
      `/api/favorite-folders?type=${type}`,
    ),
  createFavoriteFolder: (type: FavoriteFolderType, name: string) =>
    request<{ folder: FavoriteFolder }>("/api/favorite-folders", {
      method: "POST",
      body: JSON.stringify({ type, name }),
    }),
  updateFavoriteFolder: (
    id: number,
    body: { name?: string; is_default?: boolean },
  ) =>
    request<{ folder: FavoriteFolder }>(`/api/favorite-folders/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  favoriteStatus: (params: { workId?: number; creatorId?: number }) => {
    const query = new URLSearchParams();
    if (params.workId) query.set("work_id", String(params.workId));
    if (params.creatorId) query.set("creator_id", String(params.creatorId));
    return request<{ status: FavoriteStatus }>(
      `/api/favorites/status?${query.toString()}`,
    );
  },
  favoriteWorks: (folderId: number) =>
    request<{ works: Work[] }>(`/api/favorites/works?folder_id=${folderId}`),
  addWorkFavorite: (workId: number, folderId?: number) =>
    request<{ ok: boolean; status: FavoriteStatus }>("/api/favorites/works", {
      method: "POST",
      body: JSON.stringify({ work_id: workId, folder_id: folderId }),
    }),
  removeWorkFavorite: (workId: number, folderId?: number) =>
    request<{ ok: boolean; status: FavoriteStatus }>(
      `/api/favorites/works/${workId}${folderId ? `?folder_id=${folderId}` : ""}`,
      {
        method: "DELETE",
      },
    ),
  favoriteCreators: (folderId: number) =>
    request<{ creators: Creator[] }>(
      `/api/favorites/creators?folder_id=${folderId}`,
    ),
  addCreatorFavorite: (creatorId: number, folderId?: number) =>
    request<{ ok: boolean; status: FavoriteStatus }>(
      "/api/favorites/creators",
      {
        method: "POST",
        body: JSON.stringify({ creator_id: creatorId, folder_id: folderId }),
      },
    ),
  removeCreatorFavorite: (creatorId: number, folderId?: number) =>
    request<{ ok: boolean; status: FavoriteStatus }>(
      `/api/favorites/creators/${creatorId}${folderId ? `?folder_id=${folderId}` : ""}`,
      {
        method: "DELETE",
      },
    ),
};

function logQuery(params: { page?: number; pageSize?: number; status?: string }) {
  const query = new URLSearchParams();
  query.set("page", String(params.page ?? 1));
  query.set("page_size", String(params.pageSize ?? 20));
  if (params.status) query.set("status", params.status);
  return query;
}
