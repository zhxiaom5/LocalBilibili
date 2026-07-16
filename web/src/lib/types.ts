export type Directory = {
  id: number;
  path: string;
  name: string;
  status: string;
  creator_count: number;
  work_count: number;
  last_scanned_at?: string;
  created_at: string;
};

export type CreatorSummary = {
  id: number;
  name: string;
  avatar_url?: string;
  avatar_file?: string;
  bilibili_space_url?: string;
  bilibili_mid?: string;
};

export type Creator = {
  id: number;
  directory_id: number;
  name: string;
  work_count: number;
  bilibili_mid?: string;
  bilibili_mid_legacy?: string;
  bilibili_space_url?: string;
  online_identity_status?: string;
  avatar_url?: string;
  avatar_file?: string;
  bio?: string;
  profile_updated_at?: string;
  view_count?: number;
  subtitle_count?: number;
  last_work_created_at?: string;
  linked_creators?: CreatorSummary[];
};

export type CreatorLinks = {
  linked: Creator[];
  candidates: Creator[];
};

export type BulkImportItem = {
  input: string;
  bilibili_space_url?: string;
  bilibili_mid?: string;
  uid?: string;
  display_name?: string;
  avatar_url?: string;
  bio?: string;
  resolve_status?: string;
  status: string;
  existing: boolean;
  existing_creator_id?: number;
  error?: string;
};

export type Media = {
  id: number;
  work_id: number;
  type: "video" | "image" | "audio";
  file_name: string;
  url: string;
  ordinal: number;
};

export type Work = {
  id: number;
  directory_id: number;
  creator_id: number;
  creator_name: string;
  creator_avatar_url?: string;
  creator_bio?: string;
  linked_creators?: CreatorSummary[];
  bvid: string;
  work_type?: "video_long" | "video_short" | "image_text" | string;
  title: string;
  description: string;
  music_title: string;
  source_url?: string;
  cover_url?: string;
  cover_file?: string;
  tags: string[] | null;
  published_at: string;
  created_at?: string;
  media: Media[];
};

export type FavoriteFolderType = "work" | "creator";

export type FavoriteFolder = {
  id: number;
  user_id: number;
  type: FavoriteFolderType;
  name: string;
  is_default: boolean;
  created_at: string;
  updated_at: string;
};

export type FavoriteStatus = {
  work_folder_ids: number[] | null;
  creator_folder_ids: number[] | null;
  work_favorited?: boolean;
  creator_favorited?: boolean;
};

export type User = {
  id: number;
  username: string;
  role?: "super_admin" | "admin" | "user" | string;
  is_admin: boolean;
  is_super_admin?: boolean;
  can_update?: boolean;
  created_at: string;
  updated_at: string;
};

export type ViewContext = {
  actor_user: User;
  view_user: User;
};

export type LogEvent = {
  id: number;
  actor_user_id?: number;
  actor_name?: string;
  event_type: string;
  target_type?: string;
  target_id?: number;
  severity?: string;
  message: string;
  detail_json?: string;
  created_at: string;
};

export type LogEventsPage = {
  events: LogEvent[];
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
};

export type UpdateJobsPage = {
  jobs: UpdateJob[];
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
  summary: UpdateJobsSummary;
};

export type UpdateJobsSummary = {
  pending: number;
  running: number;
  succeeded: number;
  failed: number;
  added_works: number;
};

export type AboutInfo = {
  name: string;
  version: string;
  commit?: string;
  build_date?: string;
  go_version?: string;
  platform?: string;
  build_mode?: string;
  description: string;
  database_path?: string;
};

export type ScanStatus = {
  running: boolean;
  queue_length: number;
  current_task_id?: string;
  current_directory_id?: number;
  current_directory?: string;
  current_creator?: string;
  scanned_creators: number;
  total_creators: number;
  scanned_works: number;
  progress: number;
  state: string;
  errors: string[] | null;
};

export type ScanEvent = {
  type: string;
  status: ScanStatus;
  creator?: Creator;
};

export type FSEntry = {
  path: string;
  name: string;
};

export type DatabaseConfig = {
  directory: string;
  path: string;
  exists: boolean;
  writable: boolean;
};

export type RuntimeConfig = {
  downloader_concurrency: number;
};

export type UpdateQueue = {
  id: number;
  name: string;
  cron: string;
  is_default: boolean;
  enabled: boolean;
  next_run_at: string;
  last_run_summary: string;
  subscribed_count: number;
  created_at: string;
  updated_at: string;
};

export type UpdateJob = {
  id: number;
  creator_id: number;
  creator_name: string;
  actor_user_id?: number;
  actor_username?: string;
  cookie_owner_user_id?: number;
  cookie_owner_name?: string;
  queue_id?: number;
  run_id?: number;
  retry_of_job_id?: number;
  attempt?: number;
  status: string;
  trigger_source?: string;
  added_works: number;
  total_works?: number;
  processed_works?: number;
  downloaded_media?: number;
  skipped_works?: number;
  download_speed_bytes_per_second?: number;
  progress_message?: string;
  download_directory?: string;
  download_notice?: string;
  error?: string;
  error_detail?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
};

export type UpdateStatus = {
  running: boolean;
  current_creator?: string;
  pending_creators: number;
  total_creators: number;
  succeeded_creators: number;
  failed_creators: number;
  added_works: number;
  progress: number;
  state: string;
  errors: string[] | null;
  running_jobs: UpdateJob[];
  jobs: UpdateJob[];
};

export type UpdateCookie = {
  configured: boolean;
  updated_at?: string;
  validation_status: string;
  validation_message?: string;
  value?: string;
};

export type SubtitleConfig = {
  mode: "local" | "api" | "scan" | string;
  concurrency: number;
  cli_path?: string;
  api_url?: string;
  whisper_path?: string;
  ffmpeg_path?: string;
  model_path?: string;
  language: string;
  gpu: boolean;
};

export type SubtitleCheck = {
  ok: boolean;
  mode: string;
  message?: string;
  cli_path?: string;
  api_url?: string;
  whisper_path?: string;
  ffmpeg_path?: string;
  model_path?: string;
  cuda?: string;
};

export type SubtitleQueue = {
  id: number;
  name: string;
  cron: string;
  is_default: boolean;
  enabled: boolean;
  next_run_at: string;
  last_run_summary: string;
  subscribed_count: number;
  created_at: string;
  updated_at: string;
};

export type SubtitleJob = {
  id: number;
  creator_id: number;
  creator_name: string;
  work_id?: number;
  work_title?: string;
  queue_id?: number;
  run_id?: number;
  retry_of_job_id?: number;
  attempt?: number;
  source: string;
  status: string;
  total_works?: number;
  processed_works?: number;
  subtitle_count?: number;
  skipped_works?: number;
  progress_message?: string;
  output_directory?: string;
  error?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
};

export type SubtitleStatus = {
  running: boolean;
  current_creator?: string;
  pending_creators: number;
  total_creators: number;
  succeeded_creators: number;
  failed_creators: number;
  subtitle_works: number;
  progress: number;
  state: string;
  errors: string[] | null;
  running_jobs: SubtitleJob[];
  jobs: SubtitleJob[];
};

export type SubtitleJobsPage = {
  jobs: SubtitleJob[];
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
};

export type Diagnostics = {
  about: AboutInfo;
  current_user: User;
  current_view_user: User;
  database: DatabaseConfig;
  directories: Directory[];
  cookie: UpdateCookie;
  scan_status: ScanStatus;
  update_status: UpdateStatus;
  accessible_roots: FSEntry[];
  playback: {
    total_events: number;
    total_views: number;
    total_errors: number;
    last_viewed_at?: string;
    top_viewed_creator?: string;
    top_creators: Array<{
      creator_id: number;
      creator_name: string;
      views: number;
      errors: number;
      last_viewed_at?: string;
    }>;
    top_problem_works: Array<{
      work_id: number;
      creator_id: number;
      creator_name: string;
      work_title: string;
      views: number;
      errors: number;
      last_event_at?: string;
    }>;
    recent_repair_warnings: Array<{
      id: number;
      event_type: string;
      severity: string;
      message: string;
      detail?: string;
      created_at: string;
    }>;
    recent_events: Array<{
      id: number;
      user_id?: number;
      username?: string;
      work_id: number;
      creator_id: number;
      creator_name: string;
      work_title: string;
      event_type: string;
      position_sec?: number;
      duration_sec?: number;
      media_type?: string;
      detail?: string;
      created_at: string;
    }>;
  };
  latest_scan_error?: string;
  latest_update_error?: string;
};
