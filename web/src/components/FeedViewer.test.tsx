import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import type { ReactElement } from 'react';
import { readFileSync } from 'node:fs';
import { FeedViewer } from './FeedViewer';
import type { Work } from '../lib/types';
import { api } from '../lib/api';

const works: Work[] = [
  {
    id: 1,
    directory_id: 1,
    creator_id: 1,
    creator_name: '阿言',
    linked_creators: [{ id: 2, name: '阿言2' }],
    bvid: '1001',
    title: '第一条',
    description: '第一条',
    music_title: '音乐',
    tags: ['测试'],
    published_at: '',
    media: [
      { id: 1, work_id: 1, type: 'image', file_name: 'a.webp', url: '/a.webp', ordinal: 0 },
      { id: 2, work_id: 1, type: 'image', file_name: 'b.webp', url: '/b.webp', ordinal: 1 },
    ],
  },
  {
    id: 2,
    directory_id: 1,
    creator_id: 1,
    creator_name: '阿言',
    bvid: '1002',
    title: '第二条',
    description: '第二条',
    music_title: '',
    tags: [],
    published_at: '',
    media: [{ id: 3, work_id: 2, type: 'video', file_name: 'c.mp4', url: '/c.mp4', ordinal: 0 }],
  },
  {
    id: 3,
    directory_id: 1,
    creator_id: 1,
    creator_name: '阿言',
    bvid: '1003',
    title: '第三条',
    description: '第三条',
    music_title: '',
    tags: [],
    published_at: '',
    media: [{ id: 4, work_id: 3, type: 'image', file_name: 'd.webp', url: '/d.webp', ordinal: 0 }],
  },
];

const quickSwipeWorks: Work[] = Array.from({ length: 6 }, (_, index) => ({
  ...works[index % works.length],
  id: index + 10,
  bvid: `quick-${index + 1}`,
  title: `快速第${index + 1}条`,
  description: `快速第${index + 1}条描述`,
  media: [{ id: index + 100, work_id: index + 10, type: 'image' as const, file_name: `quick-${index + 1}.webp`, url: `/quick-${index + 1}.webp`, ordinal: 0 }],
}));

const backgroundVideoWorks: Work[] = [
  {
    ...works[1],
    id: 31,
    creator_name: '后台作者一',
    creator_avatar_url: '/creator-one.webp',
    title: '后台视频一',
    description: '后台视频一',
    cover_url: '/cover-one.webp',
    media: [{ id: 301, work_id: 31, type: 'video' as const, file_name: 'one.mp4', url: '/one.mp4', ordinal: 0 }],
  },
  {
    ...works[1],
    id: 32,
    creator_name: '后台作者二',
    title: '后台视频二',
    description: '后台视频二',
    cover_url: '/cover-two.webp',
    media: [{ id: 302, work_id: 32, type: 'video' as const, file_name: 'two.mp4', url: '/two.mp4', ordinal: 0 }],
  },
  {
    ...works[0],
    id: 33,
    title: '后台图片',
    description: '后台图片',
    media: [{ id: 303, work_id: 33, type: 'image' as const, file_name: 'three.webp', url: '/three.webp', ordinal: 0 }],
  },
];

type MockMediaSession = {
  metadata: MediaMetadata | null;
  playbackState: MediaSessionPlaybackState;
  handlers: Partial<Record<MediaSessionAction, MediaSessionActionHandler | null>>;
  setActionHandler: ReturnType<typeof vi.fn>;
};

type PictureInPictureMock = {
  requestPictureInPicture: ReturnType<typeof vi.fn>;
  exitPictureInPicture: ReturnType<typeof vi.fn>;
  getElement: () => Element | null;
};

function fireTouchSwipe(viewer: Element, startY = 620, moveY = 520, endY = 500) {
  fireEvent.touchStart(viewer, { touches: [{ clientY: startY }] });
  fireEvent.touchMove(viewer, { touches: [{ clientY: moveY }] });
  fireEvent.touchEnd(viewer, { changedTouches: [{ clientY: endY }] });
}

function firePointerSwipe(viewer: Element, startY = 620, moveY = 520, endY = 500) {
  const dispatchPointer = (type: string, clientY: number) => {
    const event = new Event(type, { bubbles: true, cancelable: true }) as PointerEvent;
    Object.defineProperties(event, {
      pointerType: { value: 'touch' },
      clientY: { value: clientY },
    });
    viewer.dispatchEvent(event);
  };
  act(() => {
    dispatchPointer('pointerdown', startY);
    dispatchPointer('pointermove', moveY);
    dispatchPointer('pointerup', endY);
  });
}

function mockRangeRect(input: HTMLElement, left = 20, width = 200) {
  Object.defineProperty(input, 'getBoundingClientRect', {
    configurable: true,
    value: () => ({
      left,
      right: left + width,
      top: 10,
      bottom: 30,
      width,
      height: 20,
      x: left,
      y: 10,
      toJSON: () => ({}),
    }),
  });
}

function firePointerRangeDrag(input: HTMLElement, startX: number, moveX: number, endX = moveX) {
  const dispatchPointer = (type: string, clientX: number) => {
    const event = new Event(type, { bubbles: true, cancelable: true }) as PointerEvent;
    Object.defineProperties(event, {
      pointerType: { value: 'touch' },
      pointerId: { value: 1 },
      clientX: { value: clientX },
      clientY: { value: 20 },
    });
    input.dispatchEvent(event);
  };
  act(() => {
    dispatchPointer('pointerdown', startX);
    dispatchPointer('pointermove', moveX);
    dispatchPointer('pointerup', endX);
  });
}

function fireTouchRangeDrag(input: HTMLElement, startX: number, moveX: number, endX = moveX) {
  fireEvent.touchStart(input, { touches: [{ clientX: startX, clientY: 20 }] });
  fireEvent.touchMove(input, { touches: [{ clientX: moveX, clientY: 20 }] });
  fireEvent.touchEnd(input, { changedTouches: [{ clientX: endX, clientY: 20 }] });
}

function mockPerformanceClock() {
  let now = 0;
  vi.spyOn(performance, 'now').mockImplementation(() => now);
  return (ms: number) => {
    now += ms;
    act(() => {
      vi.advanceTimersByTime(ms);
    });
  };
}

beforeEach(() => {
  if (!window.localStorage) {
    const values = new Map<string, string>();
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
        clear: () => values.clear(),
        removeItem: (key: string) => values.delete(key),
      },
    });
  }
  Object.defineProperty(HTMLMediaElement.prototype, 'play', { configurable: true, value: vi.fn().mockResolvedValue(undefined) });
  Object.defineProperty(HTMLMediaElement.prototype, 'pause', { configurable: true, value: vi.fn() });
  Object.defineProperty(HTMLMediaElement.prototype, 'load', { configurable: true, value: vi.fn() });
  vi.spyOn(api, 'recordWorkView').mockResolvedValue({ ok: true });
  vi.spyOn(api, 'favoriteFolders').mockResolvedValue({ folders: [] });
  vi.spyOn(api, 'favoriteStatus').mockResolvedValue({
    status: {
      work_folder_ids: [],
      creator_folder_ids: [],
      work_favorited: false,
      creator_favorited: false,
    },
  });
  vi.spyOn(api, 'workSubtitleLink').mockResolvedValue({ url: '/api/public/subtitles/work/token' });
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
  });
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  window.localStorage?.clear();
});

function renderFeed(ui: ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

function installMediaSessionMock() {
  const handlers: MockMediaSession['handlers'] = {};
  const session: MockMediaSession = {
    metadata: null,
    playbackState: 'none',
    handlers,
    setActionHandler: vi.fn((action: MediaSessionAction, handler: MediaSessionActionHandler | null) => {
      handlers[action] = handler;
    }),
  };
  class MockMediaMetadata {
    title: string;
    artist: string;
    album: string;
    artwork: MediaImage[];

    constructor(init: MediaMetadataInit = {}) {
      this.title = init.title ?? '';
      this.artist = init.artist ?? '';
      this.album = init.album ?? '';
      this.artwork = [...(init.artwork ?? [])];
    }
  }
  Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
  vi.stubGlobal('MediaMetadata', MockMediaMetadata);
  return session;
}

function installPictureInPictureMock({ enabled = true, rejectRequest = false } = {}): PictureInPictureMock {
  let pictureInPictureElement: Element | null = null;
  const requestPictureInPicture = vi.fn(function requestPictureInPicture(this: HTMLVideoElement) {
    if (rejectRequest) return Promise.reject(new Error('PiP rejected'));
    pictureInPictureElement = this;
    this.dispatchEvent(new Event('enterpictureinpicture'));
    return Promise.resolve({ width: 320, height: 180 });
  });
  const exitPictureInPicture = vi.fn(() => {
    const element = pictureInPictureElement;
    pictureInPictureElement = null;
    element?.dispatchEvent(new Event('leavepictureinpicture'));
    return Promise.resolve();
  });
  Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: enabled });
  Object.defineProperty(document, 'pictureInPictureElement', {
    configurable: true,
    get: () => pictureInPictureElement,
  });
  Object.defineProperty(document, 'exitPictureInPicture', { configurable: true, value: exitPictureInPicture });
  Object.defineProperty(HTMLVideoElement.prototype, 'requestPictureInPicture', {
    configurable: true,
    value: enabled ? requestPictureInPicture : undefined,
  });
  return { requestPictureInPicture, exitPictureInPicture, getElement: () => pictureInPictureElement };
}

it('switches media with arrow buttons and works with keyboard and wheel', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  expect(screen.getByText(/第一条/)).toBeInTheDocument();
  expect(screen.getByText('1/2')).toBeInTheDocument();
  await user.click(screen.getByTitle('下一张'));
  expect(screen.getByText('2/2')).toBeInTheDocument();
  await user.keyboard('{ArrowDown}');
  expect(screen.getByText('第二条')).toBeInTheDocument();
  fireEvent.wheel(screen.getByLabelText('作品播放器'), { deltaY: -100 });
  expect(screen.getByText(/第一条/)).toBeInTheDocument();
});

it('does not jump multiple works during one continuous wheel gesture', async () => {
  vi.useFakeTimers();
  const advance = mockPerformanceClock();
  renderFeed(<FeedViewer works={works} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;
  fireEvent.wheel(viewer, { deltaY: 120 });
  advance(40);
  fireEvent.wheel(viewer, { deltaY: 120 });
  advance(40);
  fireEvent.wheel(viewer, { deltaY: 120 });
  expect(screen.getByText('第二条')).toBeInTheDocument();
  expect(screen.queryByText('第三条')).not.toBeInTheDocument();
  advance(250);
  fireEvent.wheel(viewer, { deltaY: 120 });
  expect(screen.getByText('第三条')).toBeInTheDocument();
});

it('keeps one Mac trackpad inertia stream to one work switch', () => {
  vi.useFakeTimers();
  const advance = mockPerformanceClock();
  renderFeed(<FeedViewer works={quickSwipeWorks} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;

  [110, 92, 64, 38, 22, 13, 8, 5].forEach((delta) => {
    fireEvent.wheel(viewer, { deltaY: delta });
    advance(36);
  });

  expect(screen.getByText(/快速第2条/)).toBeInTheDocument();
  expect(screen.queryByText(/快速第3条/)).not.toBeInTheDocument();
});

it('treats a renewed large wheel delta after decayed inertia as a new Mac trackpad swipe', () => {
  vi.useFakeTimers();
  const advance = mockPerformanceClock();
  renderFeed(<FeedViewer works={quickSwipeWorks} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;

  [120, 46, 17, 8].forEach((delta) => {
    fireEvent.wheel(viewer, { deltaY: delta });
    advance(32);
  });
  expect(screen.getByText(/快速第2条/)).toBeInTheDocument();

  fireEvent.wheel(viewer, { deltaY: 96 });
  expect(screen.getByText(/快速第3条/)).toBeInTheDocument();
});

it('treats wheel direction reversal as a new gesture immediately', () => {
  vi.useFakeTimers();
  const advance = mockPerformanceClock();
  renderFeed(<FeedViewer works={quickSwipeWorks} initialWorkId={12} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;

  fireEvent.wheel(viewer, { deltaY: 120 });
  expect(screen.getByText(/快速第4条/)).toBeInTheDocument();
  advance(40);
  fireEvent.wheel(viewer, { deltaY: -120 });
  expect(screen.getByText(/快速第3条/)).toBeInTheDocument();
});

it('does not make normal mouse wheel users wait for the long inertia idle window', () => {
  vi.useFakeTimers();
  const advance = mockPerformanceClock();
  renderFeed(<FeedViewer works={quickSwipeWorks} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;

  fireEvent.wheel(viewer, { deltaY: 120 });
  expect(screen.getByText(/快速第2条/)).toBeInTheDocument();
  advance(260);
  fireEvent.wheel(viewer, { deltaY: 120 });
  expect(screen.getByText(/快速第3条/)).toBeInTheDocument();
});

it('uses media content sizing that scales images and videos within the stage', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  const image = screen.getByAltText('第一条');
  expect(image).toHaveClass('media-content');
  expect(screen.getByLabelText('作品播放器')).toHaveClass('immersive-stage');
  await user.keyboard('{ArrowDown}');
  const video = document.querySelector('video');
  expect(video).toHaveClass('media-content');
  const styles = readFileSync('src/styles.css', 'utf8');
  const mediaRule = styles.match(/\.media-content\s*\{[\s\S]*?\}/)?.[0] ?? '';
  expect(mediaRule).toContain('width: 100%');
  expect(mediaRule).toContain('height: 100%');
  expect(mediaRule).toContain('object-fit: contain');
  expect(styles).not.toMatch(/\.stage\s+video\.media-content\s*\{[\s\S]*?aspect-ratio:\s*9\s*\/\s*16/);
});

it('allows native horizontal touch dragging on feed range controls', () => {
  const styles = readFileSync('src/styles.css', 'utf8');
  const rangeRule = styles.match(/\.feed-range-input\s*\{[\s\S]*?\}/)?.[0] ?? '';
  expect(rangeRule).toContain('touch-action: pan-x');
});

it('does not create hidden video elements when preloading neighboring works', () => {
  const createElement = vi.spyOn(document, 'createElement');
  renderFeed(<FeedViewer works={works} />);
  expect(createElement).not.toHaveBeenCalledWith('video');
  createElement.mockRestore();
});

it('loads video metadata conservatively before the first autoplay attempt', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  const video = document.querySelector('video') as HTMLVideoElement;
  const play = vi.mocked(HTMLMediaElement.prototype.play);

  expect(video).toHaveAttribute('preload', 'metadata');
  expect(video).not.toHaveAttribute('autoplay');
  expect(play).not.toHaveBeenCalled();

  fireEvent.loadedMetadata(video);
  expect(play).toHaveBeenCalledTimes(1);
  fireEvent.loadedMetadata(video);
  expect(play).toHaveBeenCalledTimes(1);
});

it('adds a playback session to video requests and reports buffering diagnostics', async () => {
  renderFeed(<FeedViewer works={works} initialWorkId={2} />);
  const video = document.querySelector('video') as HTMLVideoElement;
  const srcURL = new URL(video.getAttribute('src') ?? '', window.location.origin);
  expect(srcURL.pathname).toBe('/c.mp4');
  expect(srcURL.searchParams.get('work_id')).toBe('2');
  expect(srcURL.searchParams.get('media_id')).toBe('3');
  expect(srcURL.searchParams.get('play_session_id')).toMatch(/^m3-/);

  const recordSpy = vi.mocked(api.recordWorkView);
  recordSpy.mockClear();
  Object.defineProperty(video, 'readyState', { configurable: true, value: HTMLMediaElement.HAVE_CURRENT_DATA });
  Object.defineProperty(video, 'networkState', { configurable: true, value: HTMLMediaElement.NETWORK_LOADING });
  Object.defineProperty(video, 'currentTime', { configurable: true, value: 12 });
  Object.defineProperty(video, 'duration', { configurable: true, value: 90 });
  Object.defineProperty(video, 'buffered', {
    configurable: true,
    value: { length: 1, start: () => 0, end: () => 18 },
  });

  fireEvent.waiting(video);

  await waitFor(() => expect(recordSpy).toHaveBeenCalledTimes(1));
  expect(recordSpy).toHaveBeenCalledWith(
    2,
    expect.objectContaining({
      eventType: 'video_waiting',
      positionSec: 12,
      durationSec: 90,
      mediaType: 'video',
      detail: expect.any(String),
    }),
  );
  const firstCall = recordSpy.mock.calls[0];
  expect(firstCall).toBeTruthy();
  const firstBody = firstCall?.[1];
  expect(firstBody).toBeTruthy();
  const detail = JSON.parse(firstBody?.detail ?? '{}');
  expect(detail.play_session_id).toBe(srcURL.searchParams.get('play_session_id'));
  expect(detail.media_id).toBe(3);
  expect(detail.ready_state).toBe(HTMLMediaElement.HAVE_CURRENT_DATA);
  expect(detail.network_state).toBe(HTMLMediaElement.NETWORK_LOADING);
  expect(detail.buffered).toEqual([{ start: 0, end: 18 }]);
});

it('throttles video progress diagnostics for the same playback session', () => {
  renderFeed(<FeedViewer works={works} initialWorkId={2} />);
  const video = document.querySelector('video') as HTMLVideoElement;
  const recordSpy = vi.mocked(api.recordWorkView);
  recordSpy.mockClear();

  fireEvent.progress(video);
  fireEvent.progress(video);

  expect(recordSpy).toHaveBeenCalledTimes(1);
  expect(recordSpy).toHaveBeenCalledWith(2, expect.objectContaining({ eventType: 'video_progress' }));
});

it('renders the initial work synchronously without loading the first random work first', () => {
  const videoWorks: Work[] = [
    {
      ...works[1],
      id: 20,
      bvid: '2000',
      title: '不该先加载的视频',
      description: '不该先加载的视频',
      media: [{ id: 30, work_id: 20, type: 'video', file_name: 'first.mp4', url: '/first.mp4', ordinal: 0 }],
    },
    {
      ...works[1],
      id: 21,
      bvid: '2001',
      title: '目标视频',
      description: '目标视频',
      media: [{ id: 31, work_id: 21, type: 'video', file_name: 'target.mp4', url: '/target.mp4', ordinal: 0 }],
    },
  ];
  renderFeed(<FeedViewer works={videoWorks} initialWorkId={21} />);
  const video = document.querySelector('video') as HTMLVideoElement;
  const srcURL = new URL(video.getAttribute('src') ?? '', window.location.origin);
  expect(srcURL.pathname).toBe('/target.mp4');
  expect(srcURL.searchParams.get('play_session_id')).toMatch(/^m31-/);
  expect(screen.getByText('目标视频')).toBeInTheDocument();
  expect(screen.queryByText('不该先加载的视频')).not.toBeInTheDocument();
});

it('does not retry video playback when volume or mute changes', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  const video = document.querySelector('video') as HTMLVideoElement;
  const play = vi.mocked(HTMLMediaElement.prototype.play);

  fireEvent.loadedMetadata(video);
  expect(play).toHaveBeenCalledTimes(1);

  fireEvent.change(screen.getByLabelText('音量'), { target: { value: '0.35' } });
  expect(play).toHaveBeenCalledTimes(1);

  await user.click(screen.getByTitle('静音'));
  expect(play).toHaveBeenCalledTimes(1);
});

it('stops showing loading when a video fails to load', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  const video = document.querySelector('video')!;
  expect(screen.getByText('加载中')).toBeInTheDocument();
  fireEvent.error(video);
  expect(screen.queryByText('加载中')).not.toBeInTheDocument();
  expect(screen.getByText('视频加载失败')).toBeInTheDocument();
});

it('does not keep loading forever when media events never fire', async () => {
  vi.useFakeTimers();
  renderFeed(<FeedViewer works={works} />);
  fireEvent.keyDown(window, { key: 'ArrowDown' });
  expect(screen.getByText('加载中')).toBeInTheDocument();
  act(() => {
    vi.advanceTimersByTime(6000);
  });
  expect(screen.queryByText('加载中')).not.toBeInTheDocument();
  expect(screen.getByText('媒体加载较慢，可尝试切换后再回来')).toBeInTheDocument();
  vi.useRealTimers();
});

it('keeps media arrows clickable and bounded', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  expect(screen.getByTitle('上一张')).toBeDisabled();
  await user.click(screen.getByTitle('下一张'));
  expect(screen.getByText('2/2')).toBeInTheDocument();
  expect(screen.getByTitle('下一张')).toBeDisabled();
  await user.click(screen.getByTitle('上一张'));
  expect(screen.getByText('1/2')).toBeInTheDocument();
});

it('activates overlay on hover and submits search', async () => {
  const user = userEvent.setup();
  const onSearch = vi.fn();
  const { container } = renderFeed(<FeedViewer works={works} onSearch={onSearch} />);
  const viewer = container.querySelector('.feed-viewer');
  expect(viewer).not.toHaveClass('overlay-active');
  fireEvent.mouseMove(viewer!);
  expect(viewer).toHaveClass('overlay-active');
  await user.type(screen.getByLabelText('搜索本地作品'), '夏天');
  await user.click(screen.getByTitle('搜索'));
  expect(onSearch).toHaveBeenCalledWith('夏天');
});

it('can hide and restore floating overlays', async () => {
  const user = userEvent.setup();
  const { container } = renderFeed(<FeedViewer works={works} />);
  await user.click(screen.getByLabelText('关闭悬浮'));
  const viewer = container.querySelector('.feed-viewer')!;
  expect(viewer).toHaveClass('overlay-pinned-hidden');
  expect(screen.queryByLabelText('搜索本地作品')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('作品操作')).not.toBeInTheDocument();
  expect(screen.queryByTitle('下一张')).not.toBeInTheDocument();
  expect(screen.getByLabelText('打开悬浮')).toBeInTheDocument();
  await user.click(screen.getByLabelText('打开悬浮'));
  expect(viewer).not.toHaveClass('overlay-pinned-hidden');
  expect(screen.getByLabelText('搜索本地作品')).toBeInTheDocument();
  expect(screen.getByLabelText('作品操作')).toBeInTheDocument();
});

it('keeps a minimal draggable timeline when video overlays are hidden', async () => {
  const user = userEvent.setup();
  const { container } = renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  const video = document.querySelector('video') as HTMLVideoElement;
  Object.defineProperty(video, 'duration', { configurable: true, value: 120 });
  fireEvent.loadedMetadata(video);
  await user.click(screen.getByLabelText('关闭悬浮'));

  const viewer = container.querySelector('.feed-viewer')!;
  expect(viewer).toHaveClass('overlay-pinned-hidden');
  expect(screen.queryByLabelText('搜索本地作品')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('作品操作')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('音量')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('悬浮透明度')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('网页全屏')).not.toBeInTheDocument();

  const minimalTimeline = screen.getByLabelText('视频极简进度');
  expect(minimalTimeline).toBeInTheDocument();
  expect(minimalTimeline.closest('.minimal-video-timeline')).toBeInTheDocument();
  fireEvent.change(minimalTimeline, { target: { value: '36' } });
  expect(video.currentTime).toBe(36);

  await user.click(screen.getByLabelText('打开悬浮'));
  expect(screen.getByLabelText('视频进度')).toBeInTheDocument();
  expect(screen.getByLabelText('音量')).toBeInTheDocument();
});

it('restores media arrows after hiding floating overlays', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  await user.click(screen.getByLabelText('关闭悬浮'));
  await user.click(screen.getByLabelText('打开悬浮'));
  await user.click(screen.getByTitle('下一张'));
  expect(screen.getByText('2/2')).toBeInTheDocument();
});

it('updates video timeline and volume controls', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  const video = document.querySelector('video') as HTMLVideoElement;
  Object.defineProperty(video, 'duration', { configurable: true, value: 120 });
  fireEvent.loadedMetadata(video);
  const timeline = screen.getByLabelText('视频进度');
  fireEvent.change(timeline, { target: { value: '42' } });
  expect(video.currentTime).toBe(42);
  fireEvent.change(screen.getByLabelText('音量'), { target: { value: '0.35' } });
  expect(video.volume).toBe(0.35);
  await user.click(screen.getByTitle('静音'));
  expect(video.muted).toBe(true);
});

it('updates player range controls continuously during touch-like pointer dragging', async () => {
  const user = userEvent.setup();
  const { container } = renderFeed(<FeedViewer works={works} initialWorkId={2} />);
  const video = document.querySelector('video') as HTMLVideoElement;
  Object.defineProperty(video, 'duration', { configurable: true, value: 120 });
  fireEvent.loadedMetadata(video);

  const timeline = screen.getByLabelText('视频进度');
  mockRangeRect(timeline);
  firePointerRangeDrag(timeline, 20, 120);
  expect(video.currentTime).toBe(60);
  expect(screen.getByText('第二条')).toBeInTheDocument();

  const volume = screen.getByLabelText('音量');
  mockRangeRect(volume);
  fireTouchRangeDrag(volume, 20, 170);
  expect(volume).toHaveValue('0.75');
  expect(video.volume).toBe(0.75);
  expect(screen.getByText('第二条')).toBeInTheDocument();

  const opacity = screen.getByLabelText('悬浮透明度');
  mockRangeRect(opacity);
  firePointerRangeDrag(opacity, 20, 70);
  expect(opacity).toHaveValue('0.32');
  expect(container.querySelector('.feed-viewer')).toHaveStyle({ '--overlay-alpha': '0.32' });
  expect(screen.getByText('第二条')).toBeInTheDocument();

  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;
  fireTouchSwipe(viewer, 500, 360, 340);
  expect(screen.getByText('第三条')).toBeInTheDocument();

  await user.keyboard('{ArrowUp}');
  const restoredVideo = document.querySelector('video') as HTMLVideoElement;
  Object.defineProperty(restoredVideo, 'duration', { configurable: true, value: 120 });
  fireEvent.loadedMetadata(restoredVideo);
  await user.click(screen.getByLabelText('关闭悬浮'));
  const minimalTimeline = screen.getByLabelText('视频极简进度');
  mockRangeRect(minimalTimeline);
  fireTouchRangeDrag(minimalTimeline, 20, 180);
  expect(restoredVideo.currentTime).toBe(96);
  expect(restoredVideo.src).toContain('work_id=2');
});

it('toggles autoplay next and persists the preference', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  const video = document.querySelector('video') as HTMLVideoElement;
  expect(video).toHaveAttribute('loop');
  expect(screen.getByLabelText('自动连播')).toHaveTextContent('单作品循环');

  await user.click(screen.getByLabelText('自动连播'));
  expect(window.localStorage.getItem('localbilibili.autoPlayNext')).toBe('true');
  expect(screen.getByLabelText('自动连播')).toHaveTextContent('自动连播');
  expect(video).not.toHaveAttribute('loop');
});

it('keeps the current video looping when autoplay next is disabled while manual feed navigation still works', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  const video = document.querySelector('video') as HTMLVideoElement;
  expect(video).toHaveAttribute('loop');

  fireEvent.ended(video);
  expect(screen.getByText('第二条')).toBeInTheDocument();
  expect(screen.queryByText('第三条')).not.toBeInTheDocument();

  await user.keyboard('{ArrowDown}');
  expect(screen.getByText('第三条')).toBeInTheDocument();
});

it('moves to the next work when autoplay next is enabled and the video ends', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  await user.click(screen.getByLabelText('自动连播'));
  const video = document.querySelector('video') as HTMLVideoElement;
  Object.defineProperty(video, 'duration', { configurable: true, value: 8 });
  Object.defineProperty(video, 'currentTime', { configurable: true, value: 8 });
  fireEvent.loadedMetadata(video);
  fireEvent.playing(video);
  await new Promise((resolve) => window.setTimeout(resolve, 1250));
  fireEvent.ended(video);
  expect(screen.getByText('第三条')).toBeInTheDocument();
});

it('does not auto-advance videos that have not loaded metadata', async () => {
  const user = userEvent.setup();
  const videoWorks = [1, 2, 3].map((id) => ({
    ...works[1],
    id,
    bvid: `video-${id}`,
    title: `视频${id}`,
    description: `视频${id}`,
    media: [{ id: 200 + id, work_id: id, type: 'video' as const, file_name: `${id}.mp4`, url: `/${id}.mp4`, ordinal: 0 }],
  }));
  renderFeed(<FeedViewer works={videoWorks} initialWorkId={1} />);
  await user.click(screen.getByLabelText('自动连播'));
  const video = document.querySelector('video') as HTMLVideoElement;
  Object.defineProperty(video, 'duration', { configurable: true, value: 8 });
  Object.defineProperty(video, 'currentTime', { configurable: true, value: 8 });
  fireEvent.playing(video);
  await new Promise((resolve) => window.setTimeout(resolve, 1250));
  fireEvent.ended(video);
  expect(screen.getByText('视频1')).toBeInTheDocument();
  expect(screen.queryByText('视频2')).not.toBeInTheDocument();
});

it('releases the previous video request when switching to another video', async () => {
  const user = userEvent.setup();
  const videoWorks = [1, 2].map((id) => ({
    ...works[1],
    id,
    bvid: `release-${id}`,
    title: `释放视频${id}`,
    description: `释放视频${id}`,
    media: [{ id: 300 + id, work_id: id, type: 'video' as const, file_name: `${id}.mp4`, url: `/${id}.mp4`, ordinal: 0 }],
  }));
  renderFeed(<FeedViewer works={videoWorks} initialWorkId={1} />);
  const firstVideo = document.querySelector('video') as HTMLVideoElement;
  const pauseSpy = vi.spyOn(firstVideo, 'pause');
  const removeAttributeSpy = vi.spyOn(firstVideo, 'removeAttribute');
  const loadSpy = vi.spyOn(firstVideo, 'load');

  await user.keyboard('{ArrowDown}');

  await waitFor(() => expect(pauseSpy).toHaveBeenCalledTimes(1));
  expect(removeAttributeSpy).toHaveBeenCalledWith('src');
  expect(loadSpy).toHaveBeenCalledTimes(1);
  expect(document.querySelector('video')).not.toBe(firstVideo);
  expect(screen.getByText('释放视频2')).toBeInTheDocument();
});

it('ignores stale ended events after a manual feed navigation', async () => {
  const user = userEvent.setup();
  const videoWorks = [1, 2, 3, 4].map((id) => ({
    ...works[1],
    id,
    bvid: `video-${id}`,
    title: `视频${id}`,
    description: `视频${id}`,
    media: [{ id: 100 + id, work_id: id, type: 'video' as const, file_name: `${id}.mp4`, url: `/${id}.mp4`, ordinal: 0 }],
  }));
  renderFeed(<FeedViewer works={videoWorks} initialWorkId={2} />);
  await user.click(screen.getByLabelText('自动连播'));
  const video = document.querySelector('video') as HTMLVideoElement;

  await user.keyboard('{ArrowDown}');
  expect(screen.getByText('视频3')).toBeInTheDocument();
  fireEvent.ended(video);
  expect(screen.getByText('视频3')).toBeInTheDocument();
  expect(screen.queryByText('视频4')).not.toBeInTheDocument();
});

it('requests the next play order when the order button is clicked', async () => {
  const user = userEvent.setup();
  const onOrderChange = vi.fn();
  renderFeed(<FeedViewer works={works} order="random" onOrderChange={onOrderChange} />);
  await user.keyboard('{ArrowDown}');
  const orderButton = screen.getByLabelText('播放顺序');
  expect(orderButton).toHaveTextContent('随机');
  await user.click(orderButton);
  expect(onOrderChange).toHaveBeenCalledWith('desc');
});

it('enables background audio from the video controls and persists the setting', async () => {
  const user = userEvent.setup();
  const session = installMediaSessionMock();
  renderFeed(<FeedViewer works={backgroundVideoWorks} initialWorkId={31} />);
  const video = document.querySelector('video') as HTMLVideoElement;
  const playSpy = vi.spyOn(video, 'play').mockResolvedValue(undefined);

  const backgroundButton = screen.getByLabelText('后台播放');
  await user.click(backgroundButton);

  expect(backgroundButton).toHaveClass('active-toggle');
  expect(backgroundButton).toHaveTextContent('后台播放');
  expect(window.localStorage.getItem('localbilibili.backgroundAudio')).toBe('true');
  expect(video.muted).toBe(false);
  expect(playSpy).toHaveBeenCalled();
  expect(session.metadata?.title).toBe('后台视频一');
  expect(session.metadata?.artist).toBe('后台作者一');
  expect(session.metadata?.artwork[0]?.src).toBe('http://localhost:3000/cover-one.webp');
  expect(session.playbackState).toBe('playing');
  expect(session.setActionHandler).toHaveBeenCalledWith('play', expect.any(Function));
  expect(session.setActionHandler).toHaveBeenCalledWith('nexttrack', expect.any(Function));
});

it('wires media session actions to player controls while background audio is enabled', async () => {
  const user = userEvent.setup();
  const session = installMediaSessionMock();
  renderFeed(<FeedViewer works={backgroundVideoWorks} initialWorkId={31} />);
  const video = document.querySelector('video') as HTMLVideoElement;
  const playSpy = vi.spyOn(video, 'play').mockResolvedValue(undefined);
  const pauseSpy = vi.spyOn(video, 'pause');
  Object.defineProperty(video, 'duration', { configurable: true, value: 120 });

  await user.click(screen.getByLabelText('后台播放'));
  act(() => {
    session.handlers.pause?.({ action: 'pause' });
  });
  expect(pauseSpy).toHaveBeenCalled();
  expect(session.playbackState).toBe('paused');

  act(() => {
    session.handlers.play?.({ action: 'play' });
  });
  expect(playSpy).toHaveBeenCalledTimes(2);
  await waitFor(() => expect(session.playbackState).toBe('playing'));

  act(() => {
    session.handlers.seekto?.({ action: 'seekto', seekTime: 42 });
  });
  expect(video.currentTime).toBe(42);

  act(() => {
    session.handlers.nexttrack?.({ action: 'nexttrack' });
  });
  expect(screen.getByText('后台视频二')).toBeInTheDocument();
  expect(session.metadata?.title).toBe('后台视频二');

  act(() => {
    session.handlers.previoustrack?.({ action: 'previoustrack' });
  });
  expect(screen.getByText('后台视频一')).toBeInTheDocument();
});

it('cleans media session metadata when leaving video or unmounting', async () => {
  const user = userEvent.setup();
  const session = installMediaSessionMock();
  const { unmount } = renderFeed(<FeedViewer works={backgroundVideoWorks} initialWorkId={32} />);

  await user.click(screen.getByLabelText('后台播放'));
  expect(session.metadata?.title).toBe('后台视频二');

  await user.keyboard('{ArrowDown}');
  expect(screen.getByText(/后台图片/)).toBeInTheDocument();
  expect(session.metadata).toBeNull();
  expect(session.playbackState).toBe('none');

  await user.keyboard('{ArrowUp}');
  await waitFor(() => expect(session.metadata?.title).toBe('后台视频二'));
  unmount();
  expect(session.metadata).toBeNull();
  expect(session.handlers.play).toBeNull();
});

it('falls back to audible playback when media session is unavailable', async () => {
  const user = userEvent.setup();
  Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: undefined });
  vi.stubGlobal('MediaMetadata', undefined);
  renderFeed(<FeedViewer works={backgroundVideoWorks} initialWorkId={31} />);
  const video = document.querySelector('video') as HTMLVideoElement;
  const playSpy = vi.spyOn(video, 'play').mockResolvedValue(undefined);

  await user.click(screen.getByLabelText('后台播放'));

  expect(video.muted).toBe(false);
  expect(playSpy).toHaveBeenCalled();
  expect(window.localStorage.getItem('localbilibili.backgroundAudio')).toBe('true');
});

it('enters and exits picture in picture from the video controls', async () => {
  const user = userEvent.setup();
  const pip = installPictureInPictureMock();
  renderFeed(<FeedViewer works={backgroundVideoWorks} initialWorkId={31} />);
  const video = document.querySelector('video') as HTMLVideoElement;
  const playSpy = vi.spyOn(video, 'play').mockResolvedValue(undefined);

  const pipButton = await screen.findByLabelText('画中画');
  await user.click(pipButton);

  expect(playSpy).toHaveBeenCalled();
  expect(pip.requestPictureInPicture).toHaveBeenCalledTimes(1);
  expect(pip.getElement()).toBe(video);
  expect(await screen.findByLabelText('退出画中画')).toHaveClass('active-toggle');

  await user.click(screen.getByLabelText('退出画中画'));
  expect(pip.exitPictureInPicture).toHaveBeenCalledTimes(1);
  expect(pip.getElement()).toBeNull();
  expect(await screen.findByLabelText('画中画')).not.toHaveClass('active-toggle');
});

it('keeps picture in picture visible with a clear notice when the browser does not support it', async () => {
  const user = userEvent.setup();
  installPictureInPictureMock({ enabled: false });
  renderFeed(<FeedViewer works={backgroundVideoWorks} initialWorkId={31} />);

  const pipButton = await screen.findByLabelText('画中画');
  expect(pipButton).toBeInTheDocument();
  await user.click(pipButton);

  expect(await screen.findByText('当前浏览器不支持画中画')).toBeInTheDocument();
  expect(screen.getByLabelText('后台播放')).toBeInTheDocument();
});

it('cleans picture in picture state when leaving video or unmounting', async () => {
  const user = userEvent.setup();
  const pip = installPictureInPictureMock();
  const { unmount } = renderFeed(<FeedViewer works={backgroundVideoWorks} initialWorkId={31} />);

  await user.click(await screen.findByLabelText('画中画'));
  expect(pip.getElement()).toBe(document.querySelector('video'));
  await user.keyboard('{ArrowDown}');
  expect(screen.getByText('后台视频二')).toBeInTheDocument();
  expect(pip.exitPictureInPicture).toHaveBeenCalledTimes(1);

  await user.click(await screen.findByLabelText('画中画'));
  unmount();
  expect(pip.exitPictureInPicture).toHaveBeenCalledTimes(2);
});

it('keeps playback controls usable when picture in picture is rejected', async () => {
  const user = userEvent.setup();
  installPictureInPictureMock({ rejectRequest: true });
  renderFeed(<FeedViewer works={backgroundVideoWorks} initialWorkId={31} />);
  const video = document.querySelector('video') as HTMLVideoElement;
  vi.spyOn(video, 'play').mockResolvedValue(undefined);

  await user.click(await screen.findByLabelText('画中画'));

  expect(await screen.findByText('画中画不可用')).toBeInTheDocument();
  expect(screen.getByLabelText('画中画')).not.toHaveClass('active-toggle');
  expect(screen.getByTitle('暂停')).toBeInTheDocument();
});

it('shows overlays when video pauses', async () => {
  const user = userEvent.setup();
  const { container } = renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  const viewer = container.querySelector('.feed-viewer')!;
  expect(viewer).not.toHaveClass('video-paused');
  fireEvent.pause(document.querySelector('video') as HTMLVideoElement);
  expect(viewer).toHaveClass('video-paused');
});

it('toggles page fullscreen and calls native fullscreen api', async () => {
  const user = userEvent.setup();
  const requestFullscreen = vi.fn().mockResolvedValue(undefined);
  const exitFullscreen = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(HTMLElement.prototype, 'requestFullscreen', { configurable: true, value: requestFullscreen });
  Object.defineProperty(document, 'exitFullscreen', { configurable: true, value: exitFullscreen });
  const { container } = renderFeed(<FeedViewer works={works} />);
  const viewer = container.querySelector('.feed-viewer')!;
  await user.click(screen.getByLabelText('网页全屏'));
  expect(viewer).toHaveClass('page-fullscreen');
  await user.click(screen.getByLabelText('退出网页全屏'));
  expect(viewer).not.toHaveClass('page-fullscreen');
  await user.click(screen.getByLabelText('完整全屏'));
  expect(requestFullscreen).toHaveBeenCalledTimes(1);
});

it('falls back to iOS video fullscreen when native fullscreen api is unavailable', async () => {
  const user = userEvent.setup();
  const webkitEnterFullscreen = vi.fn();
  Object.defineProperty(HTMLElement.prototype, 'requestFullscreen', { configurable: true, value: undefined });
  Object.defineProperty(document, 'exitFullscreen', { configurable: true, value: undefined });
  Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
    configurable: true,
    value: webkitEnterFullscreen,
  });

  renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  await user.click(await screen.findByLabelText('完整全屏'));
  expect(webkitEnterFullscreen).toHaveBeenCalledTimes(1);
});

it('shows empty state without works', () => {
  renderFeed(<FeedViewer works={[]} />);
  expect(screen.getByText(/还没有作品/)).toBeInTheDocument();
});

it('deduplicates lower-left metadata and links top creator with work title', () => {
  const { container } = renderFeed(<FeedViewer works={works} />);
  expect(container.querySelector('.meta-card')).not.toBeInTheDocument();
  expect(screen.getByRole('link', { name: '@阿言' })).toHaveAttribute('href', '/creator/1');
  expect(screen.getByRole('link', { name: '@阿言2' })).toHaveAttribute('href', '/creator/2');
  expect(container.querySelector('.top-work-title')).toHaveTextContent('第一条');
  expect(screen.queryByText(/♪ 音乐/)).not.toBeInTheDocument();
});

it('shows publish date, title, and original source link in the top work meta', () => {
  const datedWorks = [
    {
      ...works[0],
      published_at: '2026-03-10T12:34:56Z',
      source_url: 'https://www.bilibili.com/video/BV1source',
    },
  ];
  const { container } = renderFeed(<FeedViewer works={datedWorks} />);
  const meta = container.querySelector('.top-work-meta')!;
  expect(meta.querySelector('.top-work-date')).toHaveTextContent('2026-03-10');
  expect(meta.querySelector('.top-work-title')).toHaveTextContent('第一条');
  expect(meta).not.toHaveTextContent('3月10日');
  const sourceLink = screen.getByRole('link', { name: '原链接' });
  expect(sourceLink).toHaveAttribute('href', 'https://www.bilibili.com/video/BV1source');
  expect(sourceLink).toHaveAttribute('target', '_blank');
  expect(sourceLink).toHaveAttribute('rel', 'noreferrer');
});

it('hides invalid publish dates without breaking the top work meta', () => {
  const invalidDateWorks = [
    {
      ...works[0],
      published_at: 'not-a-date',
    },
  ];
  const { container } = renderFeed(<FeedViewer works={invalidDateWorks} />);
  const meta = container.querySelector('.top-work-meta')!;
  expect(meta.querySelector('.top-work-date')).toHaveTextContent('日期未知');
  expect(meta.querySelector('.top-work-title')).toHaveTextContent('第一条');
  expect(meta).not.toHaveTextContent('Invalid Date');
  expect(screen.queryByRole('link', { name: '原链接' })).not.toBeInTheDocument();
});

it('keeps top creator name and long work title in one truncating row', () => {
  const longNameWorks = [
    {
      ...works[0],
      creator_name: '自律的小屁孩很长很长很长的名字',
      title: '当初以为的再次见，原来早就成了最后一面 #徒手健身 #自律',
    },
  ];
  const { container } = renderFeed(<FeedViewer works={longNameWorks} />);
  const primary = container.querySelector('.top-meta-primary')!;
  const creator = primary.querySelector('.top-creator-link')!;
  const meta = primary.querySelector('.top-work-meta')!;
  const title = meta.querySelector('.top-work-title')!;
  expect(creator).toHaveTextContent('@自律的小屁孩很长很长很长的名字');
  expect(title).toHaveTextContent('当初以为的再次见');
  expect(Array.from(primary.children)).toEqual([creator, meta]);
});

it('falls back when the top creator avatar image fails', () => {
  const avatarWorks = [
    {
      ...works[0],
      creator_avatar_url: '/api/creators/1/avatar',
    },
  ];
  renderFeed(<FeedViewer works={avatarWorks} />);
  const avatar = document.querySelector('.top-creator-avatar') as HTMLImageElement;
  expect(avatar.tagName).toBe('IMG');
  fireEvent.error(avatar);
  expect(document.querySelector('.top-creator-avatar.fallback')).toHaveTextContent('阿');
});

it('keeps only compact favorite actions on the right rail and opens folders from the main buttons', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  const workFavorite = screen.getByTitle('收藏该作品');
  expect(workFavorite).toBeInTheDocument();
  expect(screen.getByTitle('收藏该作者')).toBeInTheDocument();
  expect(screen.queryByText('目录')).not.toBeInTheDocument();
  expect(screen.queryByTitle('UP主')).not.toBeInTheDocument();
  expect(screen.queryByTitle('评论')).not.toBeInTheDocument();
  expect(screen.queryByTitle('分享')).not.toBeInTheDocument();
  expect(screen.queryByTitle('音频')).not.toBeInTheDocument();
  await user.click(workFavorite);
  expect(await screen.findByText('收藏到默认')).toBeInTheDocument();
});

it('collapses advanced playback controls behind the more button and closes the panel', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} initialWorkId={2} />);
  const controls = screen.getByLabelText('视频控制');
  const moreButton = screen.getByLabelText('更多播放控制');
  expect(controls).not.toHaveClass('mobile-controls-expanded');

  await user.click(moreButton);
  expect(controls).toHaveClass('mobile-controls-expanded');
  expect(screen.getByLabelText('自动连播')).toBeInTheDocument();
  expect(screen.getByLabelText('播放顺序')).toBeInTheDocument();
  expect(screen.getByLabelText('悬浮透明度')).toBeInTheDocument();

  await user.keyboard('{Escape}');
  expect(controls).not.toHaveClass('mobile-controls-expanded');

  await user.click(moreButton);
  expect(controls).toHaveClass('mobile-controls-expanded');
  fireEvent.pointerDown(document.body);
  expect(controls).not.toHaveClass('mobile-controls-expanded');
});

it('persists overlay opacity from the control slider', () => {
  const { container } = renderFeed(<FeedViewer works={works} />);
  const slider = screen.getByLabelText('悬浮透明度');
  fireEvent.change(slider, { target: { value: '0.24' } });
  expect(container.querySelector('.feed-viewer')).toHaveStyle({ '--overlay-alpha': '0.24' });
  expect(window.localStorage.getItem('localbilibili.overlayOpacity')).toBe('0.24');
});

it('does not switch works when touch dragging the volume range control', () => {
  renderFeed(<FeedViewer works={works} initialWorkId={2} />);
  const volume = screen.getByLabelText('音量');
  fireEvent.touchStart(volume, { touches: [{ clientY: 500, clientX: 60 }] });
  fireEvent.touchMove(volume, { touches: [{ clientY: 180, clientX: 160 }] });
  fireEvent.touchEnd(volume, { changedTouches: [{ clientY: 180, clientX: 160 }] });
  fireEvent.change(volume, { target: { value: '0.35' } });
  expect(screen.getByText('第二条')).toBeInTheDocument();
  expect(volume).toHaveValue('0.35');
});

it('does not switch works when touch dragging the opacity range control', () => {
  const { container } = renderFeed(<FeedViewer works={works} initialWorkId={2} />);
  const opacity = screen.getByLabelText('悬浮透明度');
  fireEvent.pointerDown(opacity, { pointerType: 'touch', clientY: 500, clientX: 60 });
  fireEvent.pointerMove(opacity, { pointerType: 'touch', clientY: 180, clientX: 160 });
  fireEvent.pointerUp(opacity, { pointerType: 'touch', clientY: 180, clientX: 160 });
  fireEvent.change(opacity, { target: { value: '0.24' } });
  expect(screen.getByText('第二条')).toBeInTheDocument();
  expect(container.querySelector('.feed-viewer')).toHaveStyle({ '--overlay-alpha': '0.24' });
});

it('switches one work per touch gesture', () => {
  renderFeed(<FeedViewer works={works} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;
  fireEvent.touchStart(viewer, { touches: [{ clientY: 500 }] });
  fireEvent.touchEnd(viewer, { changedTouches: [{ clientY: 360 }] });
  expect(screen.getByText('第二条')).toBeInTheDocument();
});

it('allows a second independent touch gesture to switch again', () => {
  renderFeed(<FeedViewer works={works} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;
  fireEvent.touchStart(viewer, { touches: [{ clientY: 500 }] });
  fireEvent.touchEnd(viewer, { changedTouches: [{ clientY: 360 }] });
  fireEvent.touchStart(viewer, { touches: [{ clientY: 500 }] });
  fireEvent.touchEnd(viewer, { changedTouches: [{ clientY: 360 }] });
  expect(screen.getByText('第三条')).toBeInTheDocument();
});

it('allows rapid independent touch swipes immediately after touchmove consumed the previous swipe', () => {
  renderFeed(<FeedViewer works={quickSwipeWorks} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;

  fireTouchSwipe(viewer);
  expect(screen.getByText(/快速第2条/)).toBeInTheDocument();
  fireTouchSwipe(viewer);
  expect(screen.getByText(/快速第3条/)).toBeInTheDocument();
  fireTouchSwipe(viewer);
  expect(screen.getByText(/快速第4条/)).toBeInTheDocument();
  fireTouchSwipe(viewer);
  expect(screen.getByText(/快速第5条/)).toBeInTheDocument();
  fireTouchSwipe(viewer);
  expect(screen.getByText(/快速第6条/)).toBeInTheDocument();
});

it('switches only one work during a long touch gesture with repeated move events', () => {
  renderFeed(<FeedViewer works={works} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;
  fireEvent.touchStart(viewer, { touches: [{ clientY: 620 }] });
  fireEvent.touchMove(viewer, { touches: [{ clientY: 520 }] });
  fireEvent.touchMove(viewer, { touches: [{ clientY: 360 }] });
  fireEvent.touchMove(viewer, { touches: [{ clientY: 180 }] });
  fireEvent.touchEnd(viewer, { changedTouches: [{ clientY: 120 }] });
  expect(screen.getByText('第二条')).toBeInTheDocument();
  expect(screen.queryByText('第三条')).not.toBeInTheDocument();
});

it('suppresses synthetic wheel events during and shortly after a touch gesture', () => {
  renderFeed(<FeedViewer works={works} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;
  fireEvent.touchStart(viewer, { touches: [{ clientY: 620 }] });
  fireEvent.touchMove(viewer, { touches: [{ clientY: 520 }] });
  fireEvent.wheel(viewer, { deltaY: 240 });
  fireEvent.touchEnd(viewer, { changedTouches: [{ clientY: 180 }] });
  fireEvent.wheel(viewer, { deltaY: 240 });
  expect(screen.getByText('第二条')).toBeInTheDocument();
  expect(screen.queryByText('第三条')).not.toBeInTheDocument();
});

it('allows a new touch swipe immediately after suppressed synthetic wheel momentum', () => {
  renderFeed(<FeedViewer works={quickSwipeWorks} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;

  fireTouchSwipe(viewer);
  fireEvent.wheel(viewer, { deltaY: 240 });
  expect(screen.getByText(/快速第2条/)).toBeInTheDocument();
  fireTouchSwipe(viewer);
  expect(screen.getByText(/快速第3条/)).toBeInTheDocument();
});

it('starts a fresh touch gesture even while old wheel state is still cooling down', () => {
  vi.useFakeTimers();
  renderFeed(<FeedViewer works={works} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;
  fireEvent.wheel(viewer, { deltaY: 120 });
  expect(screen.getByText('第二条')).toBeInTheDocument();
  act(() => {
    vi.advanceTimersByTime(200);
  });
  fireEvent.touchStart(viewer, { touches: [{ clientY: 500 }] });
  fireEvent.touchEnd(viewer, { changedTouches: [{ clientY: 360 }] });
  expect(screen.getByText('第三条')).toBeInTheDocument();
});

it('treats rapid touch pointer lifecycles as independent swipes', () => {
  renderFeed(<FeedViewer works={quickSwipeWorks} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;

  firePointerSwipe(viewer);
  expect(screen.getByText(/快速第2条/)).toBeInTheDocument();
  firePointerSwipe(viewer);
  expect(screen.getByText(/快速第3条/)).toBeInTheDocument();
});

it('does not double consume one physical swipe when pointer and touch events are both emitted', () => {
  renderFeed(<FeedViewer works={quickSwipeWorks} />);
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;

  act(() => {
    const pointerDown = new Event('pointerdown', { bubbles: true, cancelable: true }) as PointerEvent;
    Object.defineProperties(pointerDown, {
      pointerType: { value: 'touch' },
      clientY: { value: 620 },
    });
    viewer.dispatchEvent(pointerDown);

    const pointerMove = new Event('pointermove', { bubbles: true, cancelable: true }) as PointerEvent;
    Object.defineProperties(pointerMove, {
      pointerType: { value: 'touch' },
      clientY: { value: 520 },
    });
    viewer.dispatchEvent(pointerMove);

    fireEvent.touchStart(viewer, { touches: [{ clientY: 620 }] });
    fireEvent.touchMove(viewer, { touches: [{ clientY: 520 }] });
    fireEvent.touchEnd(viewer, { changedTouches: [{ clientY: 500 }] });
  });

  expect(screen.getByText(/快速第2条/)).toBeInTheDocument();
  expect(screen.queryByText(/快速第3条/)).not.toBeInTheDocument();
});

it('does not switch works from interactive controls', () => {
  renderFeed(<FeedViewer works={works} />);
  const search = screen.getByLabelText('搜索本地作品');
  fireEvent.wheel(search, { deltaY: 120 });
  fireEvent.touchStart(search, { touches: [{ clientY: 500 }] });
  fireEvent.touchEnd(search, { changedTouches: [{ clientY: 360 }] });
  expect(screen.getByText(/第一条/)).toBeInTheDocument();
});

it('clears subtitle copy notice when switching media or work', async () => {
  const user = userEvent.setup();
  renderFeed(<FeedViewer works={works} />);
  await user.keyboard('{ArrowDown}');
  await user.click(screen.getByTitle('复制字幕链接'));
  expect(await screen.findByText(/字幕链接已复制/)).toBeInTheDocument();
  await user.keyboard('{ArrowDown}');
  expect(screen.queryByText(/字幕链接已复制/)).not.toBeInTheDocument();
});
