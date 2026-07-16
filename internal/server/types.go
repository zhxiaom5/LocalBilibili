package server

import "time"

type Directory struct {
	ID            int64      `json:"id"`
	Path          string     `json:"path"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	CreatorCount  int64      `json:"creator_count"`
	WorkCount     int64      `json:"work_count"`
	LastScannedAt *time.Time `json:"last_scanned_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type Creator struct {
	ID                   int64            `json:"id"`
	DirectoryID          int64            `json:"directory_id"`
	Name                 string           `json:"name"`
	WorkCount            int64            `json:"work_count"`
	BilibiliUID          string           `json:"bilibili_mid"`
	BilibiliSecUID       string           `json:"bilibili_mid_legacy,omitempty"`
	BilibiliProfileURL   string           `json:"bilibili_space_url"`
	OnlineIdentityStatus string           `json:"online_identity_status"`
	AvatarURL            string           `json:"avatar_url"`
	AvatarFile           string           `json:"avatar_file,omitempty"`
	Bio                  string           `json:"bio"`
	ProfileUpdatedAt     string           `json:"profile_updated_at,omitempty"`
	ViewCount            int64            `json:"view_count"`
	SubtitleCount        int64            `json:"subtitle_count"`
	LastWorkCreatedAt    string           `json:"last_work_created_at,omitempty"`
	LinkedCreators       []CreatorSummary `json:"linked_creators,omitempty"`
}

type CreatorSummary struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	AvatarURL      string `json:"avatar_url,omitempty"`
	AvatarFile     string `json:"avatar_file,omitempty"`
	ProfileURL     string `json:"bilibili_space_url,omitempty"`
	BilibiliSecUID string `json:"bilibili_mid,omitempty"`
}

type Media struct {
	ID       int64  `json:"id"`
	WorkID   int64  `json:"work_id"`
	Type     string `json:"type"`
	FileName string `json:"file_name"`
	URL      string `json:"url"`
	Ordinal  int    `json:"ordinal"`
}

type Work struct {
	ID               int64            `json:"id"`
	DirectoryID      int64            `json:"directory_id"`
	CreatorID        int64            `json:"creator_id"`
	CreatorName      string           `json:"creator_name"`
	CreatorAvatarURL string           `json:"creator_avatar_url,omitempty"`
	CreatorBio       string           `json:"creator_bio,omitempty"`
	LinkedCreators   []CreatorSummary `json:"linked_creators,omitempty"`
	AwemeID          string           `json:"-"`
	BVID             string           `json:"bvid"`
	WorkType         string           `json:"work_type"`
	Title            string           `json:"title"`
	Description      string           `json:"description"`
	MusicTitle       string           `json:"music_title"`
	SourceURL        string           `json:"source_url"`
	CoverURL         string           `json:"cover_url,omitempty"`
	CoverFile        string           `json:"cover_file,omitempty"`
	Tags             []string         `json:"tags"`
	PublishedAt      string           `json:"published_at"`
	CreatedAt        time.Time        `json:"created_at"`
	Media            []Media          `json:"media"`
}

type CreatorLinksResult struct {
	Linked     []Creator `json:"linked"`
	Candidates []Creator `json:"candidates"`
}

type FavoriteFolder struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FavoriteStatus struct {
	WorkFolderIDs    []int64 `json:"work_folder_ids"`
	CreatorFolderIDs []int64 `json:"creator_folder_ids"`
	WorkFavorited    bool    `json:"work_favorited"`
	CreatorFavorited bool    `json:"creator_favorited"`
}

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Role         string    `json:"role"`
	IsAdmin      bool      `json:"is_admin"`
	IsSuperAdmin bool      `json:"is_super_admin"`
	CanUpdate    bool      `json:"can_update"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ScanStatus struct {
	Running            bool     `json:"running"`
	QueueLength        int      `json:"queue_length"`
	CurrentTaskID      string   `json:"current_task_id,omitempty"`
	CurrentDirectoryID int64    `json:"current_directory_id,omitempty"`
	CurrentDirectory   string   `json:"current_directory,omitempty"`
	CurrentCreator     string   `json:"current_creator,omitempty"`
	ScannedCreators    int      `json:"scanned_creators"`
	TotalCreators      int      `json:"total_creators"`
	ScannedWorks       int      `json:"scanned_works"`
	Progress           int      `json:"progress"`
	State              string   `json:"state"`
	Errors             []string `json:"errors"`
}

type ScanEvent struct {
	Type    string     `json:"type"`
	Status  ScanStatus `json:"status"`
	Creator *Creator   `json:"creator,omitempty"`
}

type UpdateQueue struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Cron            string    `json:"cron"`
	IsDefault       bool      `json:"is_default"`
	Enabled         bool      `json:"enabled"`
	NextRunAt       string    `json:"next_run_at"`
	LastRunSummary  string    `json:"last_run_summary"`
	SubscribedCount int64     `json:"subscribed_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type UpdateRun struct {
	ID                int64     `json:"id"`
	QueueID           int64     `json:"queue_id"`
	QueueName         string    `json:"queue_name"`
	EnqueuedAt        time.Time `json:"enqueued_at"`
	StartedAt         string    `json:"started_at,omitempty"`
	FinishedAt        string    `json:"finished_at,omitempty"`
	TriggerSource     string    `json:"trigger_source"`
	TotalCreators     int       `json:"total_creators"`
	SucceededCreators int       `json:"succeeded_creators"`
	FailedCreators    int       `json:"failed_creators"`
	AddedWorks        int       `json:"added_works"`
	Status            string    `json:"status"`
	Summary           string    `json:"summary"`
}

type UpdateJob struct {
	ID                int64     `json:"id"`
	CreatorID         int64     `json:"creator_id"`
	CreatorName       string    `json:"creator_name"`
	ActorUserID       int64     `json:"actor_user_id,omitempty"`
	ActorUsername     string    `json:"actor_username,omitempty"`
	CookieOwnerUserID int64     `json:"cookie_owner_user_id,omitempty"`
	CookieOwnerName   string    `json:"cookie_owner_name,omitempty"`
	QueueID           int64     `json:"queue_id,omitempty"`
	RunID             int64     `json:"run_id,omitempty"`
	RetryOfJobID      int64     `json:"retry_of_job_id,omitempty"`
	Attempt           int       `json:"attempt"`
	Status            string    `json:"status"`
	TriggerSource     string    `json:"trigger_source"`
	AddedWorks        int       `json:"added_works"`
	TotalWorks        int       `json:"total_works"`
	ProcessedWorks    int       `json:"processed_works"`
	DownloadedMedia   int       `json:"downloaded_media"`
	SkippedWorks      int       `json:"skipped_works"`
	DownloadSpeedBps  int64     `json:"download_speed_bytes_per_second"`
	ProgressMessage   string    `json:"progress_message,omitempty"`
	DownloadDirectory string    `json:"download_directory,omitempty"`
	DownloadNotice    string    `json:"download_notice,omitempty"`
	Error             string    `json:"error,omitempty"`
	ErrorDetail       string    `json:"error_detail,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	StartedAt         string    `json:"started_at,omitempty"`
	FinishedAt        string    `json:"finished_at,omitempty"`
}

type UpdateStatus struct {
	Running           bool        `json:"running"`
	CurrentCreator    string      `json:"current_creator,omitempty"`
	PendingCreators   int         `json:"pending_creators"`
	TotalCreators     int         `json:"total_creators"`
	SucceededCreators int         `json:"succeeded_creators"`
	FailedCreators    int         `json:"failed_creators"`
	AddedWorks        int         `json:"added_works"`
	Progress          int         `json:"progress"`
	State             string      `json:"state"`
	Errors            []string    `json:"errors"`
	RunningJobs       []UpdateJob `json:"running_jobs"`
	Jobs              []UpdateJob `json:"jobs"`
}

type UpdateEvent struct {
	Type   string       `json:"type"`
	Status UpdateStatus `json:"status"`
	Job    *UpdateJob   `json:"job,omitempty"`
}

type UpdateCookie struct {
	Configured        bool   `json:"configured"`
	UpdatedAt         string `json:"updated_at,omitempty"`
	ValidationStatus  string `json:"validation_status"`
	ValidationMessage string `json:"validation_message,omitempty"`
	Value             string `json:"value,omitempty"`
}

type SubtitleConfig struct {
	Mode        string `json:"mode"`
	Concurrency int    `json:"concurrency"`
	CLIPath     string `json:"cli_path,omitempty"`
	APIURL      string `json:"api_url,omitempty"`
	WhisperPath string `json:"whisper_path,omitempty"`
	FFmpegPath  string `json:"ffmpeg_path,omitempty"`
	ModelPath   string `json:"model_path,omitempty"`
	Language    string `json:"language"`
	GPU         bool   `json:"gpu"`
}

type SubtitleCheck struct {
	OK          bool   `json:"ok"`
	Mode        string `json:"mode"`
	Message     string `json:"message,omitempty"`
	CLIPath     string `json:"cli_path,omitempty"`
	APIURL      string `json:"api_url,omitempty"`
	WhisperPath string `json:"whisper_path,omitempty"`
	FFmpegPath  string `json:"ffmpeg_path,omitempty"`
	ModelPath   string `json:"model_path,omitempty"`
	CUDA        string `json:"cuda,omitempty"`
}

type SubtitleQueue struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Cron            string    `json:"cron"`
	IsDefault       bool      `json:"is_default"`
	Enabled         bool      `json:"enabled"`
	NextRunAt       string    `json:"next_run_at"`
	LastRunSummary  string    `json:"last_run_summary"`
	SubscribedCount int64     `json:"subscribed_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SubtitleJob struct {
	ID              int64     `json:"id"`
	CreatorID       int64     `json:"creator_id"`
	CreatorName     string    `json:"creator_name"`
	WorkID          int64     `json:"work_id,omitempty"`
	WorkTitle       string    `json:"work_title,omitempty"`
	QueueID         int64     `json:"queue_id,omitempty"`
	RunID           int64     `json:"run_id,omitempty"`
	RetryOfJobID    int64     `json:"retry_of_job_id,omitempty"`
	Attempt         int       `json:"attempt"`
	Source          string    `json:"source"`
	Status          string    `json:"status"`
	TotalWorks      int       `json:"total_works"`
	ProcessedWorks  int       `json:"processed_works"`
	SubtitleCount   int       `json:"subtitle_count"`
	SkippedWorks    int       `json:"skipped_works"`
	ProgressMessage string    `json:"progress_message,omitempty"`
	OutputDirectory string    `json:"output_directory,omitempty"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	StartedAt       string    `json:"started_at,omitempty"`
	FinishedAt      string    `json:"finished_at,omitempty"`
}

type SubtitleStatus struct {
	Running           bool          `json:"running"`
	CurrentCreator    string        `json:"current_creator,omitempty"`
	PendingCreators   int           `json:"pending_creators"`
	TotalCreators     int           `json:"total_creators"`
	SucceededCreators int           `json:"succeeded_creators"`
	FailedCreators    int           `json:"failed_creators"`
	SubtitleWorks     int           `json:"subtitle_works"`
	Progress          int           `json:"progress"`
	State             string        `json:"state"`
	Errors            []string      `json:"errors"`
	RunningJobs       []SubtitleJob `json:"running_jobs"`
	Jobs              []SubtitleJob `json:"jobs"`
}

type SubtitleEvent struct {
	Type   string         `json:"type"`
	Status SubtitleStatus `json:"status"`
	Job    *SubtitleJob   `json:"job,omitempty"`
}

type SubtitleJobsPage struct {
	Jobs       []SubtitleJob `json:"jobs"`
	Page       int           `json:"page"`
	PageSize   int           `json:"page_size"`
	Total      int           `json:"total"`
	TotalPages int           `json:"total_pages"`
}

type ViewContext struct {
	ActorUser User `json:"actor_user"`
	ViewUser  User `json:"view_user"`
}

type LogEvent struct {
	ID          int64     `json:"id"`
	ActorUserID int64     `json:"actor_user_id,omitempty"`
	ActorName   string    `json:"actor_name,omitempty"`
	EventType   string    `json:"event_type"`
	TargetType  string    `json:"target_type,omitempty"`
	TargetID    int64     `json:"target_id,omitempty"`
	Severity    string    `json:"severity,omitempty"`
	Message     string    `json:"message"`
	DetailJSON  string    `json:"detail_json,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type PlaybackEvent struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id,omitempty"`
	Username    string    `json:"username,omitempty"`
	WorkID      int64     `json:"work_id"`
	CreatorID   int64     `json:"creator_id"`
	CreatorName string    `json:"creator_name"`
	WorkTitle   string    `json:"work_title"`
	EventType   string    `json:"event_type"`
	PositionSec float64   `json:"position_sec,omitempty"`
	DurationSec float64   `json:"duration_sec,omitempty"`
	MediaType   string    `json:"media_type,omitempty"`
	Detail      string    `json:"detail,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type PlaybackCreatorStat struct {
	CreatorID    int64  `json:"creator_id"`
	CreatorName  string `json:"creator_name"`
	Views        int64  `json:"views"`
	Errors       int64  `json:"errors"`
	LastViewedAt string `json:"last_viewed_at,omitempty"`
}

type PlaybackWorkStat struct {
	WorkID      int64  `json:"work_id"`
	CreatorID   int64  `json:"creator_id"`
	CreatorName string `json:"creator_name"`
	WorkTitle   string `json:"work_title"`
	Views       int64  `json:"views"`
	Errors      int64  `json:"errors"`
	LastEventAt string `json:"last_event_at,omitempty"`
}

type DiagnosticWarning struct {
	ID        int64     `json:"id"`
	EventType string    `json:"event_type"`
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

type PlaybackDiagnostics struct {
	TotalEvents          int64                 `json:"total_events"`
	TotalViews           int64                 `json:"total_views"`
	TotalErrors          int64                 `json:"total_errors"`
	LastViewedAt         string                `json:"last_viewed_at,omitempty"`
	RecentEvents         []PlaybackEvent       `json:"recent_events"`
	TopViewedCreator     string                `json:"top_viewed_creator,omitempty"`
	TopCreators          []PlaybackCreatorStat `json:"top_creators,omitempty"`
	TopProblemWorks      []PlaybackWorkStat    `json:"top_problem_works,omitempty"`
	RecentRepairWarnings []DiagnosticWarning   `json:"recent_repair_warnings,omitempty"`
}

type LogEventsPage struct {
	Events     []LogEvent `json:"events"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	Total      int        `json:"total"`
	TotalPages int        `json:"total_pages"`
}

type UpdateJobsPage struct {
	Jobs       []UpdateJob       `json:"jobs"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	Total      int               `json:"total"`
	TotalPages int               `json:"total_pages"`
	Summary    UpdateJobsSummary `json:"summary"`
}

type UpdateJobsSummary struct {
	Pending    int `json:"pending"`
	Running    int `json:"running"`
	Succeeded  int `json:"succeeded"`
	Failed     int `json:"failed"`
	AddedWorks int `json:"added_works"`
}

type Diagnostics struct {
	About             AboutInfo           `json:"about"`
	CurrentUser       User                `json:"current_user"`
	CurrentViewUser   User                `json:"current_view_user"`
	Database          DatabaseConfig      `json:"database"`
	ConfiguredDirs    []Directory         `json:"directories"`
	CookieStatus      UpdateCookie        `json:"cookie"`
	ScanStatus        ScanStatus          `json:"scan_status"`
	UpdateStatus      UpdateStatus        `json:"update_status"`
	AccessibleRoots   []FSEntry           `json:"accessible_roots"`
	Playback          PlaybackDiagnostics `json:"playback"`
	LatestScanError   string              `json:"latest_scan_error,omitempty"`
	LatestUpdateError string              `json:"latest_update_error,omitempty"`
}
