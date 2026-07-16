import {
  ArrowDownUp,
  ChevronLeft,
  ChevronRight,
  Captions,
  Expand,
  Eye,
  EyeOff,
  Headphones,
  Heart,
  Maximize2,
  Minimize2,
  MoreHorizontal,
  Pause,
  PictureInPicture2,
  Play,
  Repeat,
  Search,
  Star,
  Volume2,
  VolumeX,
} from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import type { FormEvent, MouseEvent, PointerEvent as ReactPointerEvent, TouchEvent as ReactTouchEvent } from 'react';
import type { ReactNode } from 'react';
import type { Work } from '../lib/types';
import { api } from '../lib/api';

type FeedViewerProps = {
  works: Work[];
  search?: string;
  onSearch?: (keyword: string) => void;
  enableFavorites?: boolean;
  initialWorkId?: number;
  order?: FeedOrder;
  onOrderChange?: (order: FeedOrder) => void;
  onCurrentWorkChange?: (workId: number) => void;
};

const AUTO_PLAY_NEXT_KEY = 'localbilibili.autoPlayNext';
const BACKGROUND_AUDIO_KEY = 'localbilibili.backgroundAudio';
const PLAYBACK_RATE_KEY = 'localbilibili.playbackRate';
const GESTURE_IDLE_MS = 700;
const TOUCH_SWIPE_THRESHOLD = 48;
const WHEEL_SWIPE_THRESHOLD = 40;
const WHEEL_NEW_GESTURE_GAP_MS = 240;
const WHEEL_MOMENTUM_DECAY_DELTA = 18;
const WHEEL_RENEW_MIN_DELTA = 60;
const WHEEL_RENEW_RATIO = 2.4;

type FeedOrder = 'random' | 'desc' | 'asc';

type GestureState = {
  kind: 'idle' | 'touch' | 'wheel';
  consumed: boolean;
  startY: number | null;
  lastEventAt: number;
  direction: -1 | 0 | 1;
  accumulatedDelta: number;
  lastAbsDelta: number;
  momentumMinAbsDelta: number;
};

type GestureDebugEntry = {
  type: string;
  at: number;
  deltaY?: number;
  clientY?: number | null;
  elapsedMs?: number;
  direction?: -1 | 0 | 1;
  newWheelGesture?: boolean;
  wheelReason?: string;
  consumed?: boolean;
  workId?: number;
};

declare global {
  interface Window {
    __localbilibiliGestureLog?: GestureDebugEntry[];
  }
}

type IOSFullscreenVideo = HTMLVideoElement & {
  webkitEnterFullscreen?: () => void;
  webkitExitFullscreen?: () => void;
  webkitDisplayingFullscreen?: boolean;
};

type PictureInPictureDocument = Document & {
  pictureInPictureEnabled?: boolean;
  pictureInPictureElement?: Element | null;
  exitPictureInPicture?: () => Promise<void>;
};

type PictureInPictureVideo = HTMLVideoElement & {
  requestPictureInPicture?: () => Promise<unknown>;
  disablePictureInPicture?: boolean;
};

function readAutoPlayNext() {
  try {
    return window.localStorage?.getItem(AUTO_PLAY_NEXT_KEY) === 'true';
  } catch {
    return false;
  }
}

function writeAutoPlayNext(value: boolean) {
  try {
    window.localStorage?.setItem(AUTO_PLAY_NEXT_KEY, String(value));
  } catch {
    // localStorage is optional in test and restricted browser contexts.
  }
}

function readBackgroundAudio() {
  try {
    return window.localStorage?.getItem(BACKGROUND_AUDIO_KEY) === 'true';
  } catch {
    return false;
  }
}

function writeBackgroundAudio(value: boolean) {
  try {
    window.localStorage?.setItem(BACKGROUND_AUDIO_KEY, String(value));
  } catch {
    // localStorage is optional in test and restricted browser contexts.
  }
}

function readPlaybackRate() {
  try {
    const value = Number(window.localStorage?.getItem(PLAYBACK_RATE_KEY));
    return Number.isFinite(value) && value >= 1 && value <= 2 ? value : 1;
  } catch {
    return 1;
  }
}

function writePlaybackRate(value: number) {
  try {
    window.localStorage?.setItem(PLAYBACK_RATE_KEY, String(value));
  } catch {
    // localStorage is optional in test and restricted browser contexts.
  }
}

function gestureDebugEnabled() {
  try {
    return window.location.search.includes('gesture_debug=1') || window.localStorage?.getItem('localbilibili.gestureDebug') === 'true';
  } catch {
    return false;
  }
}

function createIdleGesture(): GestureState {
  return { kind: 'idle', consumed: false, startY: null, lastEventAt: 0, direction: 0, accumulatedDelta: 0, lastAbsDelta: 0, momentumMinAbsDelta: Number.POSITIVE_INFINITY };
}

function cycleFeedOrder(current: FeedOrder): FeedOrder {
  switch (current) {
    case 'random':
      return 'desc';
    case 'desc':
      return 'asc';
    default:
      return 'random';
  }
}

function feedOrderLabel(order: FeedOrder) {
  switch (order) {
    case 'desc':
      return '倒序';
    case 'asc':
      return '顺序';
    default:
      return '随机';
  }
}

export function FeedViewer({
  works,
  search = '',
  onSearch,
  enableFavorites = false,
  initialWorkId,
  order = 'random',
  onOrderChange,
  onCurrentWorkChange,
}: FeedViewerProps) {
  const [workIndex, setWorkIndex] = useState(() => {
    const nextIndex = initialWorkId ? works.findIndex((item) => item.id === initialWorkId) : -1;
    return nextIndex >= 0 ? nextIndex : 0;
  });
  const [mediaIndex, setMediaIndex] = useState(0);
  const [query, setQuery] = useState(search);
  const [searchOpen, setSearchOpen] = useState(false);
  const [overlayActive, setOverlayActive] = useState(false);
  const [infoExpanded, setInfoExpanded] = useState(false);
  const [isPlaying, setIsPlaying] = useState(true);
  const [muted, setMuted] = useState(true);
  const [volume, setVolume] = useState(0.7);
  const [playbackRate, setPlaybackRate] = useState(readPlaybackRate);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [isPageFullscreen, setIsPageFullscreen] = useState(false);
  const [isNativeFullscreen, setIsNativeFullscreen] = useState(false);
  const [overlayPinnedHidden, setOverlayPinnedHidden] = useState(false);
  const [favoriteMenu, setFavoriteMenu] = useState<'work' | 'creator' | null>(null);
  const [hasWebkitVideoFullscreen, setHasWebkitVideoFullscreen] = useState(false);
  const [mediaReady, setMediaReady] = useState(false);
  const [workFavoriteFolderIDs, setWorkFavoriteFolderIDs] = useState<number[]>([]);
  const [creatorFavoriteFolderIDs, setCreatorFavoriteFolderIDs] = useState<number[]>([]);
  const [workFolders, setWorkFolders] = useState<{ id: number; name: string; is_default: boolean }[]>([]);
  const [creatorFolders, setCreatorFolders] = useState<{ id: number; name: string; is_default: boolean }[]>([]);
  const [subtitleNotice, setSubtitleNotice] = useState('');
  const [playerNotice, setPlayerNotice] = useState('');
  const [mediaError, setMediaError] = useState('');
  const [autoPlayNext, setAutoPlayNext] = useState(readAutoPlayNext);
  const [backgroundAudio, setBackgroundAudio] = useState(readBackgroundAudio);
  const [hasPictureInPictureSupport, setHasPictureInPictureSupport] = useState(false);
  const [isPictureInPicture, setIsPictureInPicture] = useState(false);
  const overlayTimer = useRef<number | undefined>(undefined);
  const subtitleNoticeTimer = useRef<number | undefined>(undefined);
  const playerNoticeTimer = useRef<number | undefined>(undefined);
  const overlayActiveRef = useRef(false);
  const lastOverlayActivationRef = useRef(0);
  const gestureRef = useRef<GestureState>(createIdleGesture());
  const gestureIdleTimerRef = useRef<number | undefined>(undefined);
  const ignoreWheelUntilRef = useRef(0);
  const touchPointerActiveRef = useRef(false);
  const duplicateTouchSequenceRef = useRef(false);
  const viewerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const previousWorksRef = useRef(works);
  const autoplayTargetMediaIDRef = useRef<number | null>(null);
  const autoplayAttemptedMediaIDRef = useRef<number | null>(null);
  const metadataLoadedMediaIDRef = useRef<number | null>(null);
  const playedMediaIDRef = useRef<number | null>(null);
  const lastManualWorkChangeAtRef = useRef(0);
  const viewedWorkRef = useRef<number | null>(null);
  const playbackSessionRef = useRef<{ mediaID: number | null; id: string }>({ mediaID: null, id: '' });
  const lastProgressDiagnosticAtRef = useRef(0);
  const initialWorkIndex = initialWorkId ? works.findIndex((item) => item.id === initialWorkId) : -1;
  const renderedWorkIndex = previousWorksRef.current !== works && initialWorkIndex >= 0 ? initialWorkIndex : workIndex;
  const work = works[renderedWorkIndex];
  const media = work?.media[mediaIndex];
  const isVideo = media?.type === 'video';
  const playbackSessionID = isVideo && media ? ensurePlaybackSessionID(playbackSessionRef, media.id) : '';
  const mediaURL = media
    ? mediaURLWithPlaybackDiagnostics(media.url, {
        playSessionID: playbackSessionID,
        workID: work?.id,
        mediaID: media.id,
      })
    : '';

  useEffect(() => {
    setMediaReady(false);
    setMediaError('');
    setSubtitleNotice('');
    setPlayerNotice('');
    lastProgressDiagnosticAtRef.current = 0;
    if (media?.type === 'video') {
      autoplayTargetMediaIDRef.current = media.id;
      autoplayAttemptedMediaIDRef.current = null;
      metadataLoadedMediaIDRef.current = null;
      playedMediaIDRef.current = null;
    } else {
      autoplayTargetMediaIDRef.current = null;
      autoplayAttemptedMediaIDRef.current = null;
      metadataLoadedMediaIDRef.current = null;
      playedMediaIDRef.current = null;
    }
    window.clearTimeout(subtitleNoticeTimer.current);
    window.clearTimeout(playerNoticeTimer.current);
  }, [media?.id, media?.type]);

  useEffect(() => {
    if (media?.type === 'audio') setMediaReady(true);
  }, [media?.type]);

  useEffect(() => {
    const neighbors = [works[renderedWorkIndex - 1], works[renderedWorkIndex + 1]].filter(Boolean);
    neighbors.forEach((item) => {
      const firstMedia = item.media?.[0];
      if (!firstMedia) return;
      if (firstMedia.type === 'image') {
        const image = new Image();
        image.decoding = 'async';
        image.src = firstMedia.url;
        return;
      }
    });
  }, [renderedWorkIndex, works]);

  useEffect(() => {
    writeAutoPlayNext(autoPlayNext);
  }, [autoPlayNext]);

  useEffect(() => {
    writeBackgroundAudio(backgroundAudio);
  }, [backgroundAudio]);

  useEffect(() => {
    setQuery(search);
  }, [search]);

  useEffect(() => {
    const nextIndex = initialWorkId ? works.findIndex((item) => item.id === initialWorkId) : -1;
    setWorkIndex((current) => {
      if (nextIndex >= 0) return nextIndex;
      return Math.max(0, Math.min(works.length - 1, current));
    });
    setMediaIndex(0);
    setInfoExpanded(false);
    previousWorksRef.current = works;
  }, [initialWorkId, works]);

  useEffect(() => {
    if (!enableFavorites || !work) return;
    let cancelled = false;
    Promise.all([
      api.favoriteFolders('work'),
      api.favoriteFolders('creator'),
      api.favoriteStatus({ workId: work.id, creatorId: work.creator_id }),
    ])
      .then(([workFolderData, creatorFolderData, statusData]) => {
        if (cancelled) return;
        setWorkFolders(Array.isArray(workFolderData.folders) ? workFolderData.folders : []);
        setCreatorFolders(Array.isArray(creatorFolderData.folders) ? creatorFolderData.folders : []);
        setWorkFavoriteFolderIDs(statusData.status?.work_folder_ids ?? []);
        setCreatorFavoriteFolderIDs(statusData.status?.creator_folder_ids ?? []);
      })
      .catch(() => {
        if (cancelled) return;
        setWorkFolders([]);
        setCreatorFolders([]);
      });
    return () => {
      cancelled = true;
    };
  }, [enableFavorites, work]);

  useEffect(() => {
    document.body.classList.toggle('feed-page-fullscreen', isPageFullscreen);
    return () => document.body.classList.remove('feed-page-fullscreen');
  }, [isPageFullscreen]);

  useEffect(() => {
    const onFullscreenChange = () => setIsNativeFullscreen(document.fullscreenElement === viewerRef.current);
    document.addEventListener('fullscreenchange', onFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', onFullscreenChange);
  }, []);

  useEffect(() => {
    const video = videoRef.current as IOSFullscreenVideo | null;
    setHasWebkitVideoFullscreen(Boolean(video?.webkitEnterFullscreen));
    if (!video) return;

    const handleBegin = () => setIsNativeFullscreen(true);
    const handleEnd = () => setIsNativeFullscreen(false);
    video.addEventListener('webkitbeginfullscreen', handleBegin as EventListener);
    video.addEventListener('webkitendfullscreen', handleEnd as EventListener);
    return () => {
      video.removeEventListener('webkitbeginfullscreen', handleBegin as EventListener);
      video.removeEventListener('webkitendfullscreen', handleEnd as EventListener);
    };
  }, [media?.id, isVideo]);

  useEffect(() => {
    const pictureInPictureDocument = document as PictureInPictureDocument;
    if (!isVideo) {
      setHasPictureInPictureSupport(false);
      setIsPictureInPicture(false);
      return;
    }
    const video = videoRef.current as PictureInPictureVideo | null;
    const supported = Boolean(
      pictureInPictureDocument.pictureInPictureEnabled &&
        video &&
        typeof video.requestPictureInPicture === 'function' &&
        !video.disablePictureInPicture,
    );
    setHasPictureInPictureSupport(supported);
    setIsPictureInPicture(Boolean(video && pictureInPictureDocument.pictureInPictureElement === video));
    if (!video) return;

    const handleEnter = () => setIsPictureInPicture(true);
    const handleLeave = () => setIsPictureInPicture(false);
    video.addEventListener('enterpictureinpicture', handleEnter);
    video.addEventListener('leavepictureinpicture', handleLeave);
    return () => {
      video.removeEventListener('enterpictureinpicture', handleEnter);
      video.removeEventListener('leavepictureinpicture', handleLeave);
      if (pictureInPictureDocument.pictureInPictureElement === video && typeof pictureInPictureDocument.exitPictureInPicture === 'function') {
        void pictureInPictureDocument.exitPictureInPicture().catch(() => undefined);
      }
    };
  }, [media?.id, isVideo]);

  useEffect(() => {
    if (!isVideo) return;
    const video = videoRef.current;
    return () => {
      if (!video) return;
      video.pause();
      video.removeAttribute('src');
      video.load();
    };
  }, [media?.id, isVideo]);

  useEffect(() => {
    return () => {
      window.clearTimeout(overlayTimer.current);
      window.clearTimeout(subtitleNoticeTimer.current);
      window.clearTimeout(playerNoticeTimer.current);
      window.clearTimeout(gestureIdleTimerRef.current);
    };
  }, []);

  useEffect(() => {
    overlayActiveRef.current = overlayActive;
  }, [overlayActive]);

  const activateOverlay = useCallback(() => {
    if (overlayPinnedHidden) return;
    const now = performance.now();
    if (overlayActiveRef.current && now - lastOverlayActivationRef.current < 600) {
      return;
    }
    lastOverlayActivationRef.current = now;
    if (!overlayActiveRef.current) {
      overlayActiveRef.current = true;
      setOverlayActive(true);
    }
    window.clearTimeout(overlayTimer.current);
    overlayTimer.current = window.setTimeout(() => {
      overlayActiveRef.current = false;
      setOverlayActive(false);
    }, 2200);
  }, [overlayPinnedHidden]);

  const showPlayerNotice = useCallback(
    (message: string) => {
      setPlayerNotice(message);
      activateOverlay();
      window.clearTimeout(playerNoticeTimer.current);
      playerNoticeTimer.current = window.setTimeout(() => setPlayerNotice(''), 2600);
    },
    [activateOverlay],
  );

  const moveWork = useCallback(
    (delta: number, source: 'manual' | 'auto' = 'manual') => {
      activateOverlay();
      setWorkIndex((current) => {
        const next = Math.max(0, Math.min(works.length - 1, current + delta));
        if (next !== current) {
          if (source === 'manual') {
            lastManualWorkChangeAtRef.current = performance.now();
          }
          setMediaIndex(0);
        }
        return next;
      });
    },
    [activateOverlay, works.length],
  );

  const moveMedia = useCallback(
    (delta: number) => {
      activateOverlay();
      const count = work?.media.length ?? 0;
      setMediaIndex((current) => Math.max(0, Math.min(count - 1, current + delta)));
    },
    [activateOverlay, work],
  );

  const standardFullscreenAvailable =
    typeof HTMLElement.prototype.requestFullscreen === 'function' &&
    typeof document.exitFullscreen === 'function';
  const nativeFullscreenAvailable = standardFullscreenAvailable || hasWebkitVideoFullscreen;

  const clearMediaSession = useCallback(() => {
    const session = navigator.mediaSession;
    if (!session) return;
    (['play', 'pause', 'seekto', 'previoustrack', 'nexttrack'] as MediaSessionAction[]).forEach((action) => {
      try {
        session.setActionHandler(action, null);
      } catch {
        // Some browsers expose a partial Media Session implementation.
      }
    });
    session.metadata = null;
    session.playbackState = 'none';
  }, []);

  const playCurrentVideo = useCallback(async () => {
    const video = videoRef.current;
    if (!video) return;
    try {
      await video.play();
      setIsPlaying(true);
      if (backgroundAudio && navigator.mediaSession) {
        navigator.mediaSession.playbackState = 'playing';
      }
    } catch {
      setIsPlaying(false);
    }
  }, [backgroundAudio]);

  const pauseCurrentVideo = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    video.pause();
    setIsPlaying(false);
    if (backgroundAudio && navigator.mediaSession) {
      navigator.mediaSession.playbackState = 'paused';
    }
  }, [backgroundAudio]);

  const recordVideoDiagnostic = useCallback(
    (eventType: string, video: HTMLVideoElement | null = videoRef.current, reason = '') => {
      if (!work || !media || media.type !== 'video') return;
      void api.recordWorkView(work.id, {
        eventType,
        positionSec: finiteNumber(video?.currentTime),
        durationSec: finiteNumber(video?.duration),
        mediaType: 'video',
        detail: videoDiagnosticDetail({
          playSessionID: playbackSessionID,
          workID: work.id,
          mediaID: media.id,
          reason,
          video,
        }),
      });
    },
    [media, playbackSessionID, work],
  );

  const recordVideoProgress = useCallback(
    (video: HTMLVideoElement) => {
      const now = performance.now();
      if (lastProgressDiagnosticAtRef.current > 0 && now - lastProgressDiagnosticAtRef.current < 5000) {
        return;
      }
      lastProgressDiagnosticAtRef.current = now;
      recordVideoDiagnostic('video_progress', video);
    },
    [recordVideoDiagnostic],
  );

  const attemptAutoplayVideo = useCallback(async () => {
    const video = videoRef.current;
    if (!video || !work || !media || media.type !== 'video') return;
    if (autoplayAttemptedMediaIDRef.current === media.id) return;
    autoplayAttemptedMediaIDRef.current = media.id;
    try {
      await video.play();
      setIsPlaying(true);
    } catch {
      setIsPlaying(false);
      recordVideoDiagnostic('video_play_error', video, 'video.play() rejected');
    }
  }, [media?.id, media?.type, recordVideoDiagnostic, work?.id]);

  const syncVideoDuration = useCallback((video: HTMLVideoElement | null = videoRef.current) => {
    const nextDuration = video?.duration;
    setDuration(typeof nextDuration === 'number' && Number.isFinite(nextDuration) && nextDuration > 0 ? nextDuration : 0);
  }, []);

  const handleMediaClick = (event: MouseEvent<HTMLButtonElement>, delta: number) => {
    event.preventDefault();
    event.stopPropagation();
    moveMedia(delta);
  };

  const pushGestureDebug = useCallback((entry: GestureDebugEntry) => {
    if (!gestureDebugEnabled()) return;
    const log = window.__localbilibiliGestureLog ?? [];
    log.push({ ...entry, workId: work?.id });
    window.__localbilibiliGestureLog = log.slice(-80);
  }, [work?.id]);

  const resetGesture = useCallback(() => {
    gestureRef.current = createIdleGesture();
  }, []);

  const scheduleWheelGestureIdle = useCallback(() => {
    window.clearTimeout(gestureIdleTimerRef.current);
    gestureIdleTimerRef.current = window.setTimeout(() => {
      if (gestureRef.current.kind === 'wheel') {
        resetGesture();
      }
    }, GESTURE_IDLE_MS);
  }, [resetGesture]);

  const consumeGestureDelta = useCallback((delta: number, threshold: number) => {
    const gesture = gestureRef.current;
    if (gesture.consumed || Math.abs(delta) < threshold) return false;
    const direction = delta > 0 ? 1 : -1;
    gesture.consumed = true;
    gesture.direction = direction;
    moveWork(direction);
    return true;
  }, [moveWork]);

  const shouldStartNewWheelGesture = (gesture: GestureState, now: number, deltaY: number) => {
    if (gesture.kind !== 'wheel') return { start: true, reason: 'idle' };
    const elapsed = now - gesture.lastEventAt;
    const direction = deltaY > 0 ? 1 : -1;
    const absDelta = Math.abs(deltaY);
    if (elapsed > WHEEL_NEW_GESTURE_GAP_MS) return { start: true, reason: 'gap' };
    if (gesture.consumed && gesture.direction !== 0 && direction !== gesture.direction) {
      return { start: true, reason: 'reverse' };
    }
    if (
      gesture.consumed &&
      gesture.momentumMinAbsDelta <= WHEEL_MOMENTUM_DECAY_DELTA &&
      absDelta >= WHEEL_RENEW_MIN_DELTA &&
      absDelta >= gesture.momentumMinAbsDelta * WHEEL_RENEW_RATIO
    ) {
      return { start: true, reason: 'renewed-delta' };
    }
    return { start: false, reason: 'same-stream' };
  };

  const handleWheelDelta = useCallback((deltaY: number, target: EventTarget | null, preventDefault?: () => void) => {
    if (target && isInteractiveWheelTarget(target)) {
      return;
    }
    const now = performance.now();
    if (now < ignoreWheelUntilRef.current || gestureRef.current.kind === 'touch') {
      preventDefault?.();
      pushGestureDebug({ type: 'wheel-ignore', at: now, deltaY, consumed: gestureRef.current.consumed });
      return;
    }
    preventDefault?.();
    activateOverlay();
    const previous = gestureRef.current;
    const elapsed = previous.lastEventAt ? now - previous.lastEventAt : 0;
    const nextWheel = shouldStartNewWheelGesture(previous, now, deltaY);
    if (nextWheel.start) {
      gestureRef.current = {
        kind: 'wheel',
        consumed: false,
        startY: null,
        lastEventAt: now,
        direction: 0,
        accumulatedDelta: 0,
        lastAbsDelta: 0,
        momentumMinAbsDelta: Number.POSITIVE_INFINITY,
      };
    } else {
      previous.lastEventAt = now;
    }
    const current = gestureRef.current;
    const absDelta = Math.abs(deltaY);
    current.accumulatedDelta += deltaY;
    current.lastAbsDelta = absDelta;
    current.momentumMinAbsDelta = Math.min(current.momentumMinAbsDelta, absDelta);
    consumeGestureDelta(current.accumulatedDelta, WHEEL_SWIPE_THRESHOLD);
    pushGestureDebug({
      type: 'wheel',
      at: now,
      deltaY,
      elapsedMs: elapsed,
      direction: deltaY > 0 ? 1 : -1,
      newWheelGesture: nextWheel.start,
      wheelReason: nextWheel.reason,
      consumed: current.consumed,
    });
    scheduleWheelGestureIdle();
  }, [activateOverlay, consumeGestureDelta, pushGestureDebug, scheduleWheelGestureIdle]);

  const handleTouchStartY = useCallback((target: EventTarget | null, clientY: number | null, skipDuplicateCheck = false) => {
    const now = performance.now();
    if (!skipDuplicateCheck && touchPointerActiveRef.current) {
      duplicateTouchSequenceRef.current = true;
      return;
    }
    duplicateTouchSequenceRef.current = false;
    window.clearTimeout(gestureIdleTimerRef.current);
    resetGesture();
    ignoreWheelUntilRef.current = 0;
    if (target && isInteractiveWheelTarget(target)) return;
    if (clientY == null) return;
    gestureRef.current = { kind: 'touch', consumed: false, startY: clientY, lastEventAt: now, direction: 0, accumulatedDelta: 0, lastAbsDelta: 0, momentumMinAbsDelta: Number.POSITIVE_INFINITY };
    pushGestureDebug({ type: 'touchstart', at: now, clientY, consumed: false });
  }, [pushGestureDebug, resetGesture]);

  const consumeTouchY = useCallback((clientY: number | null, preventDefault?: () => void) => {
    const gesture = gestureRef.current;
    if (gesture.kind !== 'touch' || gesture.startY == null || clientY == null) return;
    const delta = gesture.startY - clientY;
    if (Math.abs(delta) < TOUCH_SWIPE_THRESHOLD) return;
    preventDefault?.();
    consumeGestureDelta(delta, TOUCH_SWIPE_THRESHOLD);
  }, [consumeGestureDelta]);

  const handleTouchMoveY = useCallback((target: EventTarget | null, clientY: number | null, preventDefault?: () => void) => {
    if (duplicateTouchSequenceRef.current) {
      preventDefault?.();
      return;
    }
    if (gestureRef.current.kind !== 'touch') return;
    if (target && isInteractiveWheelTarget(target)) return;
    consumeTouchY(clientY, preventDefault);
  }, [consumeTouchY]);

  const handleTouchEndY = useCallback((target: EventTarget | null, clientY: number | null) => {
    if (duplicateTouchSequenceRef.current) {
      duplicateTouchSequenceRef.current = false;
      ignoreWheelUntilRef.current = performance.now() + GESTURE_IDLE_MS;
      return;
    }
    if (gestureRef.current.kind !== 'touch') return;
    if (!(target && isInteractiveWheelTarget(target))) {
      consumeTouchY(clientY);
    }
    resetGesture();
    ignoreWheelUntilRef.current = performance.now() + GESTURE_IDLE_MS;
  }, [consumeTouchY, resetGesture]);

  const handleTouchCancel = useCallback(() => {
    if (duplicateTouchSequenceRef.current) {
      duplicateTouchSequenceRef.current = false;
      ignoreWheelUntilRef.current = performance.now() + GESTURE_IDLE_MS;
      return;
    }
    if (gestureRef.current.kind === 'touch') {
      resetGesture();
      ignoreWheelUntilRef.current = performance.now() + GESTURE_IDLE_MS;
    }
  }, [resetGesture]);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer) return;

    const onWheel = (event: globalThis.WheelEvent) => {
      handleWheelDelta(event.deltaY, event.target, () => {
        if (event.cancelable) event.preventDefault();
      });
    };
    const onTouchStart = (event: globalThis.TouchEvent) => {
      handleTouchStartY(event.target, event.touches[0]?.clientY ?? null);
    };
    const onTouchMove = (event: globalThis.TouchEvent) => {
      handleTouchMoveY(event.target, event.touches[0]?.clientY ?? null, () => {
        if (event.cancelable) event.preventDefault();
      });
    };
    const onTouchEnd = (event: globalThis.TouchEvent) => {
      handleTouchEndY(event.target, event.changedTouches[0]?.clientY ?? null);
    };
    const onPointerStart = (event: globalThis.PointerEvent) => {
      if (!isTouchLikePointer(event)) return;
      touchPointerActiveRef.current = true;
      handleTouchStartY(event.target, event.clientY, true);
    };
    const onPointerMove = (event: globalThis.PointerEvent) => {
      if (!isTouchLikePointer(event)) return;
      handleTouchMoveY(event.target, event.clientY, () => {
        if (event.cancelable) event.preventDefault();
      });
    };
    const onPointerEnd = (event: globalThis.PointerEvent) => {
      if (!isTouchLikePointer(event)) return;
      handleTouchEndY(event.target, event.clientY);
      touchPointerActiveRef.current = false;
    };
    const onPointerCancel = (event: globalThis.PointerEvent) => {
      if (!isTouchLikePointer(event)) return;
      handleTouchCancel();
      touchPointerActiveRef.current = false;
    };

    viewer.addEventListener('wheel', onWheel, { passive: false });
    viewer.addEventListener('pointerdown', onPointerStart, { passive: true });
    viewer.addEventListener('pointermove', onPointerMove, { passive: false });
    viewer.addEventListener('pointerup', onPointerEnd, { passive: false });
    viewer.addEventListener('pointercancel', onPointerCancel, { passive: true });
    viewer.addEventListener('touchstart', onTouchStart, { passive: true });
    viewer.addEventListener('touchmove', onTouchMove, { passive: false });
    viewer.addEventListener('touchend', onTouchEnd, { passive: false });
    viewer.addEventListener('touchcancel', handleTouchCancel, { passive: true });
    return () => {
      viewer.removeEventListener('wheel', onWheel);
      viewer.removeEventListener('pointerdown', onPointerStart);
      viewer.removeEventListener('pointermove', onPointerMove);
      viewer.removeEventListener('pointerup', onPointerEnd);
      viewer.removeEventListener('pointercancel', onPointerCancel);
      viewer.removeEventListener('touchstart', onTouchStart);
      viewer.removeEventListener('touchmove', onTouchMove);
      viewer.removeEventListener('touchend', onTouchEnd);
      viewer.removeEventListener('touchcancel', handleTouchCancel);
    };
  }, [handleTouchCancel, handleTouchEndY, handleTouchMoveY, handleTouchStartY, handleWheelDelta]);

  const togglePageFullscreen = () => {
    setIsPageFullscreen((current) => !current);
    activateOverlay();
  };

  const toggleNativeFullscreen = async () => {
    activateOverlay();
    if (standardFullscreenAvailable) {
      if (!document.fullscreenElement) {
        await viewerRef.current?.requestFullscreen?.();
        return;
      }
      await document.exitFullscreen?.();
      return;
    }
    const video = videoRef.current as IOSFullscreenVideo | null;
    if (!video?.webkitEnterFullscreen) return;
    if (video.webkitDisplayingFullscreen && typeof video.webkitExitFullscreen === 'function') {
      video.webkitExitFullscreen();
      return;
    }
    video.webkitEnterFullscreen();
  };

  const toggleFloatingOverlay = () => {
    setOverlayPinnedHidden((current) => {
      const next = !current;
      if (!next) {
        setOverlayActive(true);
        window.clearTimeout(overlayTimer.current);
        overlayTimer.current = window.setTimeout(() => setOverlayActive(false), 2200);
      } else {
        setOverlayActive(false);
      }
      return next;
    });
  };

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === 'ArrowDown') moveWork(1);
      if (event.key === 'ArrowUp') moveWork(-1);
      if (event.key === 'ArrowRight') moveMedia(1);
      if (event.key === 'ArrowLeft') moveMedia(-1);
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [moveMedia, moveWork]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !isVideo) return;
    video.volume = volume;
    video.muted = muted;
    video.playbackRate = playbackRate;
    setCurrentTime(0);
    if (video.readyState >= HTMLMediaElement.HAVE_METADATA) {
      syncVideoDuration(video);
      setMediaReady(true);
      void attemptAutoplayVideo();
      return;
    }
    setDuration(0);
  }, [attemptAutoplayVideo, isVideo, media?.id, syncVideoDuration]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    video.volume = volume;
  }, [volume]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    video.muted = muted;
  }, [muted]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    video.playbackRate = playbackRate;
  }, [playbackRate]);

  useEffect(() => {
    if (!isVideo) return;
    const timer = window.setTimeout(() => {
      const video = videoRef.current;
      if (video && video.readyState >= HTMLMediaElement.HAVE_METADATA) {
        setMediaReady(true);
        void attemptAutoplayVideo();
      }
    }, 0);
    return () => window.clearTimeout(timer);
  }, [attemptAutoplayVideo, isVideo, media?.id]);

  useEffect(() => {
    if (media?.type === 'audio' || mediaReady) return;
    const timer = window.setTimeout(() => {
      setMediaReady(true);
      setMediaError((current) => current || '媒体加载较慢，可尝试切换后再回来');
    }, 6000);
    return () => window.clearTimeout(timer);
  }, [media?.id, media?.type, mediaReady]);

  useEffect(() => {
    if (media?.type !== 'image') return;
    const timer = window.setTimeout(() => {
      const image = viewerRef.current?.querySelector<HTMLImageElement>('.media-content');
      if (image?.complete) {
        setMediaReady(true);
      }
    }, 0);
    return () => window.clearTimeout(timer);
  }, [media?.id, media?.type]);

  useEffect(() => {
    if (!work || viewedWorkRef.current === work.id) return;
    viewedWorkRef.current = work.id;
    onCurrentWorkChange?.(work.id);
    void api.recordWorkView(work.id, {
      eventType: 'view',
      mediaType: media?.type,
      durationSec: duration,
    });
  }, [duration, media?.type, onCurrentWorkChange, work]);

  useEffect(() => {
    if (!work || !media || media.type !== 'video' || mediaReady) return;
    const timer = window.setTimeout(() => {
      void api.recordWorkView(work.id, {
        eventType: 'video_prebuffer_error',
        mediaType: 'video',
        detail: videoDiagnosticDetail({
          playSessionID: playbackSessionID,
          workID: work.id,
          mediaID: media.id,
          reason: 'video metadata not ready after timeout',
          video: videoRef.current,
        }),
      });
    }, 8000);
    return () => window.clearTimeout(timer);
  }, [media, mediaReady, playbackSessionID, work]);

  const submitSearch = (event: FormEvent) => {
    event.preventDefault();
    onSearch?.(query.trim());
    activateOverlay();
  };

  const togglePlay = () => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) {
      void playCurrentVideo();
    } else {
      pauseCurrentVideo();
    }
  };

  const toggleBackgroundAudio = () => {
    const next = !backgroundAudio;
    setBackgroundAudio(next);
    activateOverlay();
    if (!next) {
      clearMediaSession();
      return;
    }
    const nextVolume = volume > 0.05 ? volume : 0.7;
    setMuted(false);
    if (volume <= 0.05) {
      setVolume(nextVolume);
    }
    const video = videoRef.current;
    if (video) {
      video.muted = false;
      video.volume = nextVolume;
      void video.play().then(() => {
        setIsPlaying(true);
        if (navigator.mediaSession) navigator.mediaSession.playbackState = 'playing';
      }).catch(() => {
        setIsPlaying(false);
      });
    }
  };

  const toggleMute = () => {
    const next = !muted;
    setMuted(next);
    if (videoRef.current) videoRef.current.muted = next;
  };

  const togglePictureInPicture = async () => {
    activateOverlay();
    const video = videoRef.current as PictureInPictureVideo | null;
    const pictureInPictureDocument = document as PictureInPictureDocument;
    if (!video || !hasPictureInPictureSupport || typeof video.requestPictureInPicture !== 'function') {
      showPlayerNotice('当前浏览器不支持画中画');
      return;
    }
    try {
      if (pictureInPictureDocument.pictureInPictureElement === video) {
        await pictureInPictureDocument.exitPictureInPicture?.();
        setIsPictureInPicture(false);
        return;
      }
      if (video.paused) {
        await video.play();
        setIsPlaying(true);
      }
      await video.requestPictureInPicture();
      setIsPictureInPicture(true);
    } catch {
      setIsPictureInPicture(false);
      showPlayerNotice('画中画不可用');
    }
  };

  const toggleAutoPlayNext = () => {
    setAutoPlayNext((current) => !current);
    activateOverlay();
  };

  const toggleFeedOrder = () => {
    onOrderChange?.(cycleFeedOrder(order));
    activateOverlay();
  };

  const shouldAutoAdvanceAfterEnded = (video: HTMLVideoElement) => {
    if (!autoPlayNext || !media || media.type !== 'video') return false;
    if (metadataLoadedMediaIDRef.current !== media.id) return false;
    if (playedMediaIDRef.current !== media.id) return false;
    const durationValue = video.duration;
    if (!Number.isFinite(durationValue) || durationValue <= 0) return false;
    const currentValue = video.currentTime;
    if (!Number.isFinite(currentValue) || currentValue < 0) return false;
    if (currentValue < durationValue - 0.5) return false;
    if (performance.now() - lastManualWorkChangeAtRef.current < 1200) return false;
    return true;
  };

  const seek = (value: number) => {
    const video = videoRef.current;
    if (!video || !Number.isFinite(video.duration)) return;
    video.currentTime = value;
    setCurrentTime(value);
    void api.recordWorkView(work.id, {
      eventType: 'seek',
      positionSec: value,
      durationSec: video.duration,
      mediaType: 'video',
    });
  };

  useEffect(() => {
    if (!backgroundAudio || !isVideo || !work || !media) {
      clearMediaSession();
      return;
    }
    const session = navigator.mediaSession;
    if (!session) return;
    const artworkSource = work.cover_url || work.creator_avatar_url || '';
    try {
      if (typeof MediaMetadata !== 'undefined') {
        session.metadata = new MediaMetadata({
          title: work.title || work.description || '本地作品',
          artist: work.creator_name || 'LocalBilibili',
          album: work.music_title || 'LocalBilibili',
          artwork: artworkSource ? [{ src: absoluteURL(artworkSource), sizes: '512x512', type: 'image/jpeg' }] : [],
        });
      }
      session.playbackState = isPlaying ? 'playing' : 'paused';
      session.setActionHandler('play', () => {
        void playCurrentVideo();
      });
      session.setActionHandler('pause', () => pauseCurrentVideo());
      session.setActionHandler('seekto', (details) => {
        if (typeof details.seekTime === 'number') {
          seek(details.seekTime);
        }
      });
      session.setActionHandler('previoustrack', () => moveWork(-1));
      session.setActionHandler('nexttrack', () => moveWork(1));
    } catch {
      // Background audio is best-effort; unsupported Media Session pieces should not break playback.
    }
    return clearMediaSession;
  }, [backgroundAudio, clearMediaSession, isPlaying, isVideo, media, moveWork, pauseCurrentVideo, playCurrentVideo, seek, work]);

  const workDefaultFolderID = workFolders.find((folder) => folder.is_default)?.id;
  const creatorDefaultFolderID = creatorFolders.find((folder) => folder.is_default)?.id;
  const isWorkFavorite = workFavoriteFolderIDs.length > 0;
  const isCreatorFavorite = creatorFavoriteFolderIDs.length > 0;

  if (!work || !media) {
    return <div className="empty-state">还没有作品。先去设置里保存目录并扫描。</div>;
  }

  const workDate = formatDate(work.published_at) || formatDate(work.created_at ?? '') || '日期未知';
  const workTitle = work.title?.trim() || '本地作品';
  const bottomSummary = [workDate, workTitle].filter(Boolean).join(' · ');
  const hasExpandableInfo = Boolean(work.linked_creators?.length);

  const toggleWorkFavorite = async (folderID = workDefaultFolderID) => {
    if (!enableFavorites || !folderID) return;
    const response = workFavoriteFolderIDs.includes(folderID)
      ? await api.removeWorkFavorite(work.id, folderID)
      : await api.addWorkFavorite(work.id, folderID);
    setWorkFavoriteFolderIDs(response.status.work_folder_ids ?? []);
    activateOverlay();
  };

  const toggleCreatorFavorite = async (folderID = creatorDefaultFolderID) => {
    if (!enableFavorites || !folderID) return;
    const response = creatorFavoriteFolderIDs.includes(folderID)
      ? await api.removeCreatorFavorite(work.creator_id, folderID)
      : await api.addCreatorFavorite(work.creator_id, folderID);
    setCreatorFavoriteFolderIDs(response.status.creator_folder_ids ?? []);
    activateOverlay();
  };

  const copyWorkSubtitleLink = async () => {
    if (!isVideo) return;
    try {
      const response = await api.workSubtitleLink(work.id);
      await copyText(absoluteURL(response.url));
      setSubtitleNotice('字幕链接已复制；如果字幕缺失，打开链接会自动加入字幕队列。');
    } catch (cause) {
      setSubtitleNotice(cause instanceof Error ? cause.message : '复制字幕链接失败');
    }
    activateOverlay();
    window.clearTimeout(subtitleNoticeTimer.current);
    subtitleNoticeTimer.current = window.setTimeout(() => setSubtitleNotice(''), 2600);
  };

  return (
    <div
      ref={viewerRef}
      className={`feed-viewer immersive ${overlayActive ? 'overlay-active' : ''} ${isPageFullscreen ? 'page-fullscreen' : ''} ${overlayPinnedHidden ? 'overlay-pinned-hidden' : ''} ${isVideo && !isPlaying ? 'video-paused' : ''}`}
      tabIndex={0}
      onFocus={activateOverlay}
      onMouseMove={activateOverlay}
    >
      <section
        className="stage immersive-stage"
        aria-label="作品播放器"
        onClick={(event) => {
          if (!isVideo || isInteractiveWheelTarget(event.target)) return;
          togglePlay();
          activateOverlay();
        }}
      >
        {media.type === 'video' && (
          <video
            key={media.id}
            className="media-content"
            ref={videoRef}
            src={mediaURL}
            preload="metadata"
            muted={muted}
            loop={!autoPlayNext}
            playsInline
            onLoadedMetadata={(event) => {
              event.currentTarget.playbackRate = playbackRate;
              metadataLoadedMediaIDRef.current = media.id;
              syncVideoDuration(event.currentTarget);
              setMediaReady(true);
              recordVideoDiagnostic('video_loadedmetadata', event.currentTarget);
              if (autoplayTargetMediaIDRef.current === media.id) {
                void attemptAutoplayVideo();
              }
            }}
            onDurationChange={(event) => syncVideoDuration(event.currentTarget)}
            onCanPlay={(event) => {
              setMediaReady(true);
              recordVideoDiagnostic('video_canplay', event.currentTarget);
            }}
            onLoadedData={(event) => {
              setMediaReady(true);
              recordVideoDiagnostic('video_loadeddata', event.currentTarget);
            }}
            onPlaying={(event) => {
              playedMediaIDRef.current = media.id;
              setMediaReady(true);
              recordVideoDiagnostic('video_playing', event.currentTarget);
            }}
            onWaiting={(event) => recordVideoDiagnostic('video_waiting', event.currentTarget)}
            onStalled={(event) => recordVideoDiagnostic('video_stalled', event.currentTarget)}
            onSuspend={(event) => recordVideoDiagnostic('video_suspend', event.currentTarget)}
            onProgress={(event) => recordVideoProgress(event.currentTarget)}
            onTimeUpdate={(event) => setCurrentTime(event.currentTarget.currentTime)}
            onPlay={() => setIsPlaying(true)}
            onPause={() => setIsPlaying(false)}
            onEnded={(event) => {
              if (!shouldAutoAdvanceAfterEnded(event.currentTarget as HTMLVideoElement)) return;
              moveWork(1, 'auto');
            }}
            onError={(event) => {
              setMediaReady(true);
              setMediaError('视频加载失败');
              void api.recordWorkView(work.id, {
                eventType: 'video_error',
                positionSec: currentTime,
                durationSec: duration,
                mediaType: 'video',
                detail: videoDiagnosticDetail({
                  playSessionID: playbackSessionID,
                  workID: work.id,
                  mediaID: media.id,
                  reason: 'video element error',
                  video: event.currentTarget,
                }),
              });
            }}
          />
        )}
        {media.type === 'image' && (
          <img
            className="media-content"
            src={media.url}
            alt={work.title}
            onLoad={() => setMediaReady(true)}
            onError={() => {
              setMediaReady(true);
              setMediaError('图片加载失败');
            }}
          />
        )}
        {media.type === 'audio' && <audio src={media.url} controls autoPlay />}
        {!mediaReady && media.type !== 'audio' && <div className="media-loading">加载中</div>}
        {mediaError && <div className="media-loading media-error">{mediaError}</div>}
        {isVideo && !isPlaying && !mediaError && (
          <div className="center-pause-indicator" aria-label="当前已暂停">
            <Pause size={46} fill="currentColor" strokeWidth={2.4} />
          </div>
        )}
      </section>

      <div className="player-overlay">
        {!overlayPinnedHidden && (
          <>
            <div className={`feed-top-actions ${searchOpen ? 'search-open' : ''}`}>
              {!searchOpen && (
                <button className="feed-search-toggle" type="button" title="搜索" aria-label="打开搜索" onClick={() => setSearchOpen(true)}>
                  <Search size={28} />
                </button>
              )}
              {searchOpen && (
                <form className="feed-search" onSubmit={submitSearch}>
                  <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索本地作品" aria-label="搜索本地作品" autoFocus />
                  <button type="submit" title="搜索">
                    <Search size={20} />
                    <span>搜索</span>
                  </button>
                </form>
              )}
            </div>

            <aside className="action-rail" aria-label="作品操作">
              <FavoriteRailButton
                active={isWorkFavorite}
                icon={<Heart size={30} fill={isWorkFavorite ? 'currentColor' : 'none'} />}
                label={isWorkFavorite ? '已收藏作品' : '收藏作品'}
                title={isWorkFavorite ? '取消收藏该作品' : '收藏该作品'}
                onClick={() => setFavoriteMenu(favoriteMenu === 'work' ? null : 'work')}
              />
              <FavoriteRailButton
                active={isCreatorFavorite}
                icon={<Star size={30} fill={isCreatorFavorite ? 'currentColor' : 'none'} />}
                label={isCreatorFavorite ? '已收藏作者' : '收藏作者'}
                title={isCreatorFavorite ? '取消收藏该作者' : '收藏该作者'}
                onClick={() => setFavoriteMenu(favoriteMenu === 'creator' ? null : 'creator')}
              />
              {isVideo && (
                <button className="rail-button" type="button" title={muted ? '取消静音' : '静音'} aria-label={muted ? '取消静音' : '静音'} onClick={toggleMute}>
                  {muted ? <VolumeX size={28} /> : <Volume2 size={28} />}
                  <span>{muted ? '静音' : '有声'}</span>
                </button>
              )}
              {favoriteMenu === 'work' && (
                <FavoriteFolderMenu
                  folders={workFolders}
                  selectedIDs={workFavoriteFolderIDs}
                  defaultLabel={isWorkFavorite ? '取消收藏作品' : '收藏到默认'}
                  onDefault={() => void toggleWorkFavorite()}
                  onSelect={(id) => void toggleWorkFavorite(id)}
                />
              )}
              {favoriteMenu === 'creator' && (
                <FavoriteFolderMenu
                  folders={creatorFolders}
                  selectedIDs={creatorFavoriteFolderIDs}
                  defaultLabel={isCreatorFavorite ? '取消收藏作者' : '收藏到默认'}
                  onDefault={() => void toggleCreatorFavorite()}
                  onSelect={(id) => void toggleCreatorFavorite(id)}
                />
              )}
              {isVideo && (
                <button className="rail-button subtitle-link-button" type="button" title="复制字幕链接" onClick={() => void copyWorkSubtitleLink()}>
                  <Captions size={28} />
                  <span>字幕</span>
                </button>
              )}
            </aside>
            {subtitleNotice && <div className="subtitle-copy-notice">{subtitleNotice}</div>}
            {playerNotice && <div className="subtitle-copy-notice player-control-notice">{playerNotice}</div>}

            {work.media.length > 1 && (
              <div className="media-controls floating-media-controls">
                <button className="icon-button" type="button" onClick={(event) => handleMediaClick(event, -1)} title="上一张" disabled={mediaIndex === 0}>
                  <ChevronLeft size={20} />
                </button>
                <span>
                  {mediaIndex + 1}/{work.media.length}
                </span>
                <button className="icon-button" type="button" onClick={(event) => handleMediaClick(event, 1)} title="下一张" disabled={mediaIndex === work.media.length - 1}>
                  <ChevronRight size={20} />
                </button>
              </div>
            )}

            {isVideo && <VideoTimeline currentTime={currentTime} duration={duration} onSeek={seek} />}

            <footer className="bottom-meta">
              <section className={`creator-info-panel ${infoExpanded ? 'expanded' : ''}`} aria-label="作品作者信息">
                <Link to={`/creator/${work.creator_id}`} className="bottom-creator-link" aria-label={`@${work.creator_name}`}>
                  <CreatorAvatarMini src={work.creator_avatar_url} name={work.creator_name} />
                  <span>@{work.creator_name}</span>
                </Link>
                <p className="bottom-summary">
                  <span>{bottomSummary}</span>
                  {work.source_url && (
                    <>
                      <span aria-hidden="true"> · </span>
                      <a className="bottom-source-link" href={work.source_url} target="_blank" rel="noreferrer">
                        原链接
                      </a>
                    </>
                  )}
                </p>
                {hasExpandableInfo && (
                  <button className="feed-info-expand" type="button" onClick={() => setInfoExpanded((current) => !current)}>
                    {infoExpanded ? '收起' : '展开'}
                  </button>
                )}
                {infoExpanded && work.linked_creators && work.linked_creators.length > 0 && (
                  <div className="bottom-meta-linked">
                    <small>关联创作者</small>
                    <div className="bottom-meta-linked-list">
                      {work.linked_creators.map((item) => (
                        <Link key={item.id} to={`/creator/${item.id}`} className="linked-creator-chip">
                          @{item.name}
                        </Link>
                      ))}
                    </div>
                  </div>
                )}
              </section>
              {isVideo && (
                <VideoControls
                  autoPlayNext={autoPlayNext}
                  backgroundAudio={backgroundAudio}
                  isPictureInPicture={isPictureInPicture}
                  order={order}
                  muted={muted}
                  volume={volume}
                  playbackRate={playbackRate}
                  currentTime={currentTime}
                  duration={duration}
                  onToggleAutoPlayNext={toggleAutoPlayNext}
                  onToggleBackgroundAudio={toggleBackgroundAudio}
                  onTogglePictureInPicture={() => void togglePictureInPicture()}
                  onToggleOrder={toggleFeedOrder}
                  onVolume={(next) => {
                    setVolume(next);
                    setMuted(next === 0);
                    if (videoRef.current) videoRef.current.volume = next;
                  }}
                  onPlaybackRate={(next) => {
                    setPlaybackRate(next);
                    writePlaybackRate(next);
                    if (videoRef.current) videoRef.current.playbackRate = next;
                  }}
                  onToggleMute={toggleMute}
                  overlayToggle={<OverlayToggle hidden={overlayPinnedHidden} onToggle={toggleFloatingOverlay} />}
                  closeKey={`${work.id}:${media.id}`}
                  fullscreenControls={
                    <FullscreenControls
                      isPageFullscreen={isPageFullscreen}
                      isNativeFullscreen={isNativeFullscreen}
                      nativeFullscreenAvailable={nativeFullscreenAvailable}
                      onTogglePageFullscreen={togglePageFullscreen}
                      onToggleNativeFullscreen={toggleNativeFullscreen}
                    />
                  }
                />
              )}
              {!isVideo && (
                <div className="image-control-cluster">
                  <OverlayToggle hidden={overlayPinnedHidden} onToggle={toggleFloatingOverlay} />
                  <FullscreenControls
                    isPageFullscreen={isPageFullscreen}
                    isNativeFullscreen={isNativeFullscreen}
                    nativeFullscreenAvailable={nativeFullscreenAvailable}
                    onTogglePageFullscreen={togglePageFullscreen}
                    onToggleNativeFullscreen={toggleNativeFullscreen}
                  />
                </div>
              )}
            </footer>
          </>
        )}
      </div>
      {overlayPinnedHidden && isVideo && <MinimalTimeline currentTime={currentTime} duration={duration} onSeek={seek} />}
      {overlayPinnedHidden && <OverlayToggle hidden={overlayPinnedHidden} onToggle={toggleFloatingOverlay} floating />}
    </div>
  );
}

function isInteractiveWheelTarget(target: EventTarget) {
  if (!(target instanceof Element)) return false;
  return Boolean(target.closest('button, input, textarea, select, a, .feed-range-input, .media-controls, .video-controls, .advanced-control-panel, .favorite-folder-menu, .feed-search, .feed-top-actions, .creator-info-panel, .overlay-toggle'));
}

function isTouchLikePointer(event: PointerEvent) {
  return event.pointerType === 'touch' || event.pointerType === 'pen';
}

function useRangeDrag(onChange: (value: number) => void) {
  const pointerDraggingRef = useRef(false);
  const touchDraggingRef = useRef(false);

  const commitClientX = useCallback(
    (input: HTMLInputElement, clientX: number) => {
      if (!Number.isFinite(clientX)) return;
      const value = rangeValueFromClientX(input, clientX);
      input.value = String(value);
      onChange(value);
    },
    [onChange],
  );

  return {
    onPointerDown: (event: ReactPointerEvent<HTMLInputElement>) => {
      event.stopPropagation();
      event.preventDefault();
      pointerDraggingRef.current = true;
      event.currentTarget.setPointerCapture?.(event.pointerId);
      commitClientX(event.currentTarget, event.clientX);
    },
    onPointerMove: (event: ReactPointerEvent<HTMLInputElement>) => {
      event.stopPropagation();
      if (!pointerDraggingRef.current) return;
      event.preventDefault();
      commitClientX(event.currentTarget, event.clientX);
    },
    onPointerUp: (event: ReactPointerEvent<HTMLInputElement>) => {
      event.stopPropagation();
      if (!pointerDraggingRef.current) return;
      event.preventDefault();
      commitClientX(event.currentTarget, event.clientX);
      pointerDraggingRef.current = false;
      event.currentTarget.releasePointerCapture?.(event.pointerId);
    },
    onPointerCancel: (event: ReactPointerEvent<HTMLInputElement>) => {
      event.stopPropagation();
      pointerDraggingRef.current = false;
      event.currentTarget.releasePointerCapture?.(event.pointerId);
    },
    onTouchStart: (event: ReactTouchEvent<HTMLInputElement>) => {
      event.stopPropagation();
      event.preventDefault();
      const touch = event.touches[0];
      if (!touch) return;
      touchDraggingRef.current = true;
      commitClientX(event.currentTarget, touch.clientX);
    },
    onTouchMove: (event: ReactTouchEvent<HTMLInputElement>) => {
      event.stopPropagation();
      if (!touchDraggingRef.current) return;
      event.preventDefault();
      const touch = event.touches[0];
      if (!touch) return;
      commitClientX(event.currentTarget, touch.clientX);
    },
    onTouchEnd: (event: ReactTouchEvent<HTMLInputElement>) => {
      event.stopPropagation();
      if (!touchDraggingRef.current) return;
      event.preventDefault();
      const touch = event.changedTouches[0];
      if (touch) commitClientX(event.currentTarget, touch.clientX);
      touchDraggingRef.current = false;
    },
    onTouchCancel: (event: ReactTouchEvent<HTMLInputElement>) => {
      event.stopPropagation();
      touchDraggingRef.current = false;
    },
  };
}

function rangeValueFromClientX(input: HTMLInputElement, clientX: number) {
  const min = finiteRangeNumber(input.min, 0);
  const max = finiteRangeNumber(input.max, 100);
  const rect = input.getBoundingClientRect();
  const width = rect.width > 0 ? rect.width : 1;
  const ratio = Math.min(1, Math.max(0, (clientX - rect.left) / width));
  const raw = min + (max - min) * ratio;
  const step = input.step && input.step !== 'any' ? Number(input.step) : 0;
  const stepped = Number.isFinite(step) && step > 0 ? min + Math.round((raw - min) / step) * step : raw;
  const clamped = Math.min(max, Math.max(min, stepped));
  return Number(clamped.toFixed(rangePrecision(input.step)));
}

function finiteRangeNumber(value: string, fallback: number) {
  const next = Number(value);
  return Number.isFinite(next) ? next : fallback;
}

function rangePrecision(step: string) {
  if (!step || step === 'any' || !step.includes('.')) return 3;
  return Math.min(6, step.split('.')[1]?.length ?? 3);
}

function FavoriteRailButton({
  active,
  icon,
  label,
  title,
  onClick,
}: {
  active: boolean;
  icon: ReactNode;
  label: string;
  title: string;
  onClick: () => void;
}) {
  return (
    <div className="favorite-action">
      <button className={`rail-button ${active ? 'favorited' : ''}`} title={title} onClick={onClick} type="button">
        {icon}
        <span>{label}</span>
      </button>
    </div>
  );
}

function FavoriteFolderMenu({
  folders,
  selectedIDs,
  defaultLabel,
  onDefault,
  onSelect,
}: {
  folders: { id: number; name: string; is_default: boolean }[];
  selectedIDs: number[];
  defaultLabel: string;
  onDefault: () => void;
  onSelect: (id: number) => void;
}) {
  return (
    <div className="favorite-folder-menu">
      <button type="button" onClick={onDefault}>
        {defaultLabel}
      </button>
      {folders.map((folder) => (
        <button key={folder.id} type="button" onClick={() => onSelect(folder.id)}>
          {selectedIDs.includes(folder.id) ? '✓ ' : ''}
          {folder.name}
          {folder.is_default ? ' 默认' : ''}
        </button>
      ))}
      {folders.length === 0 && <span>暂无收藏夹</span>}
    </div>
  );
}

function VideoControls({
  autoPlayNext,
  backgroundAudio,
  isPictureInPicture,
  order,
  muted,
  volume,
  playbackRate,
  currentTime,
  duration,
  onToggleAutoPlayNext,
  onToggleBackgroundAudio,
  onTogglePictureInPicture,
  onToggleOrder,
  onVolume,
  onPlaybackRate,
  onToggleMute,
  overlayToggle,
  closeKey,
  fullscreenControls,
}: {
  autoPlayNext: boolean;
  backgroundAudio: boolean;
  isPictureInPicture: boolean;
  order: FeedOrder;
  muted: boolean;
  volume: number;
  playbackRate: number;
  currentTime: number;
  duration: number;
  onToggleAutoPlayNext: () => void;
  onToggleBackgroundAudio: () => void;
  onTogglePictureInPicture: () => void;
  onToggleOrder: () => void;
  onVolume: (value: number) => void;
  onPlaybackRate: (value: number) => void;
  onToggleMute: () => void;
  overlayToggle: ReactNode;
  closeKey: string;
  fullscreenControls: ReactNode;
}) {
  const volumeDrag = useRangeDrag(onVolume);
  const playbackRateDrag = useRangeDrag(onPlaybackRate);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const controlsRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    setAdvancedOpen(false);
  }, [closeKey]);

  useEffect(() => {
    if (!advancedOpen) return;
    const handlePointerDown = (event: globalThis.PointerEvent) => {
      if (controlsRef.current?.contains(event.target as Node)) return;
      setAdvancedOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setAdvancedOpen(false);
    };
    document.addEventListener('pointerdown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [advancedOpen]);

  return (
    <div ref={controlsRef} className={`video-controls ${advancedOpen ? 'mobile-controls-expanded' : ''}`} aria-label="视频控制">
      <div className="control-row">
        <span className="timecode timeline-time-badge">
          {formatTime(currentTime)} / {formatTime(duration)}
        </span>
        <button
          className={`icon-button more-controls-button ${advancedOpen ? 'active-toggle' : ''}`}
          type="button"
          title="更多播放控制"
          aria-label="更多播放控制"
          aria-expanded={advancedOpen}
          onClick={() => setAdvancedOpen((open) => !open)}
        >
          <MoreHorizontal size={22} />
        </button>
        {advancedOpen && (
          <div className="advanced-control-panel" aria-label="高级播放控制">
            <label className="volume-control" title="音量">
              <button className="icon-button" type="button" onClick={onToggleMute} title={muted ? '取消静音' : '静音'} aria-label={muted ? '取消静音' : '静音'}>
                {muted ? <VolumeX size={22} /> : <Volume2 size={22} />}
              </button>
              <input
                className="feed-range-input"
                aria-label="音量"
                type="range"
                min={0}
                max={1}
                step={0.05}
                value={muted ? 0 : volume}
                {...volumeDrag}
                onChange={(event) => onVolume(Number(event.currentTarget.value))}
              />
            </label>
            <section className="advanced-panel-section" aria-label="播放设置">
              <span className="advanced-panel-heading">播放</span>
              <label className="playback-rate-control" title="播放速度">
                <span>速度 <output aria-live="off">{playbackRate.toFixed(1)}x</output></span>
                <input
                  className="feed-range-input"
                  aria-label="播放速度"
                  type="range"
                  min={1}
                  max={2}
                  step={0.1}
                  value={playbackRate}
                  {...playbackRateDrag}
                  onChange={(event) => onPlaybackRate(Number(event.currentTarget.value))}
                />
              </label>
              <div className="advanced-action-grid">
                <button
                  className={`icon-button mode-icon-button ${autoPlayNext ? 'active-toggle' : ''}`}
                  type="button"
                  onClick={onToggleAutoPlayNext}
                  title={autoPlayNext ? '关闭自动连播，改为单作品循环' : '开启自动连播'}
                  aria-label="自动连播"
                >
                  <Repeat size={18} />
                  <span>{autoPlayNext ? '自动连播' : '单作品循环'}</span>
                </button>
                <button
                  className="icon-button mode-icon-button"
                  type="button"
                  onClick={onToggleOrder}
                  title={`播放顺序：${feedOrderLabel(order)}`}
                  aria-label="播放顺序"
                >
                  <ArrowDownUp size={18} />
                  <span>{feedOrderLabel(order)}</span>
                </button>
              </div>
            </section>
            <section className="advanced-panel-section" aria-label="画面设置">
              <span className="advanced-panel-heading">画面</span>
              <div className="advanced-action-grid">
                <button
                  className={`icon-button mode-icon-button ${backgroundAudio ? 'active-toggle' : ''}`}
                  type="button"
                  onClick={onToggleBackgroundAudio}
                  title={backgroundAudio ? '关闭手机后台播放' : '开启手机后台播放'}
                  aria-label="后台播放"
                >
                  <Headphones size={18} />
                  <span>{backgroundAudio ? '后台播放' : '普通播放'}</span>
                </button>
                {overlayToggle}
              </div>
            </section>
            <section className="advanced-panel-section advanced-panel-section-compact" aria-label="窗口设置">
              <span className="advanced-panel-heading">窗口</span>
              <div className="advanced-window-actions">
                <button
                  className={`icon-button mode-icon-button ${isPictureInPicture ? 'active-toggle' : ''}`}
                  type="button"
                  onClick={onTogglePictureInPicture}
                  title={isPictureInPicture ? '退出画中画' : '画中画'}
                  aria-label={isPictureInPicture ? '退出画中画' : '画中画'}
                >
                  <PictureInPicture2 size={18} />
                  <span>{isPictureInPicture ? '画中画中' : '画中画'}</span>
                </button>
                {fullscreenControls}
              </div>
            </section>
          </div>
        )}
      </div>
    </div>
  );
}

function VideoTimeline({ currentTime, duration, onSeek }: { currentTime: number; duration: number; onSeek: (value: number) => void }) {
  const timelineDrag = useRangeDrag(onSeek);

  return (
    <div className="full-video-timeline" aria-label="视频底部进度条">
      <input
        className="timeline feed-range-input"
        aria-label="视频进度"
        type="range"
        min={0}
        max={duration || 0}
        step="0.1"
        value={Math.min(currentTime, duration || 0)}
        {...timelineDrag}
        onChange={(event) => onSeek(Number(event.currentTarget.value))}
      />
    </div>
  );
}

function MinimalTimeline({ currentTime, duration, onSeek }: { currentTime: number; duration: number; onSeek: (value: number) => void }) {
  const timelineDrag = useRangeDrag(onSeek);

  return (
    <div className="minimal-video-timeline" aria-label="极简视频进度条">
      <input
        className="timeline minimal-timeline feed-range-input"
        aria-label="视频极简进度"
        type="range"
        min={0}
        max={duration || 0}
        step="0.1"
        value={Math.min(currentTime, duration || 0)}
        {...timelineDrag}
        onChange={(event) => onSeek(Number(event.currentTarget.value))}
      />
    </div>
  );
}

function OverlayToggle({ hidden, onToggle, floating = false }: { hidden: boolean; onToggle: () => void; floating?: boolean }) {
  return (
    <button className={`icon-button overlay-toggle ${floating ? 'floating-overlay-toggle' : ''}`} type="button" onClick={onToggle} title={hidden ? '打开悬浮' : '关闭悬浮'} aria-label={hidden ? '打开悬浮' : '关闭悬浮'}>
      {hidden ? <Eye size={21} /> : <EyeOff size={21} />}
    </button>
  );
}

function FullscreenControls({
  isPageFullscreen,
  isNativeFullscreen,
  nativeFullscreenAvailable,
  onTogglePageFullscreen,
  onToggleNativeFullscreen,
}: {
  isPageFullscreen: boolean;
  isNativeFullscreen: boolean;
  nativeFullscreenAvailable: boolean;
  onTogglePageFullscreen: () => void;
  onToggleNativeFullscreen: () => void;
}) {
  return (
    <div className="fullscreen-controls" aria-label="全屏控制">
      <button className="icon-button" type="button" onClick={onTogglePageFullscreen} title={isPageFullscreen ? '退出网页全屏' : '网页全屏'} aria-label={isPageFullscreen ? '退出网页全屏' : '网页全屏'}>
        {isPageFullscreen ? <Minimize2 size={21} /> : <Maximize2 size={21} />}
      </button>
      {nativeFullscreenAvailable && (
        <button className="icon-button" type="button" onClick={onToggleNativeFullscreen} title={isNativeFullscreen ? '退出完整全屏' : '完整全屏'} aria-label={isNativeFullscreen ? '退出完整全屏' : '完整全屏'}>
          {isNativeFullscreen ? <Minimize2 size={21} /> : <Expand size={21} />}
        </button>
      )}
    </div>
  );
}

function CreatorAvatarMini({ src, name }: { src?: string; name: string }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [src]);
  if (src && !failed) {
    return <img className="creator-avatar-mini" src={src} alt="" aria-hidden="true" referrerPolicy="no-referrer" onError={() => setFailed(true)} />;
  }
  return <span className="creator-avatar-mini fallback" aria-hidden="true">{name.slice(0, 1) || '@'}</span>;
}

function formatTime(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '00:00';
  const minutes = Math.floor(value / 60);
  const seconds = Math.floor(value % 60);
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
}

function formatDate(value: string) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

function absoluteURL(path: string) {
  if (/^https?:\/\//i.test(path)) return path;
  return `${window.location.origin}${path.startsWith('/') ? path : `/${path}`}`;
}

function ensurePlaybackSessionID(ref: { current: { mediaID: number | null; id: string } }, mediaID: number) {
  if (ref.current.mediaID === mediaID && ref.current.id) {
    return ref.current.id;
  }
  ref.current = { mediaID, id: createPlaybackSessionID(mediaID) };
  return ref.current.id;
}

function createPlaybackSessionID(mediaID: number) {
  const randomID = typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  return `m${mediaID}-${randomID}`;
}

function mediaURLWithPlaybackDiagnostics(
  source: string,
  params: { playSessionID?: string; workID?: number; mediaID?: number },
) {
  if (!params.playSessionID || !source) return source;
  try {
    const url = new URL(source, window.location.origin);
    url.searchParams.set('play_session_id', params.playSessionID);
    if (params.workID) url.searchParams.set('work_id', String(params.workID));
    if (params.mediaID) url.searchParams.set('media_id', String(params.mediaID));
    if (/^https?:\/\//i.test(source)) return url.toString();
    return `${url.pathname}${url.search}${url.hash}`;
  } catch {
    const joiner = source.includes('?') ? '&' : '?';
    return `${source}${joiner}play_session_id=${encodeURIComponent(params.playSessionID)}`;
  }
}

function videoDiagnosticDetail({
  playSessionID,
  workID,
  mediaID,
  reason,
  video,
}: {
  playSessionID: string;
  workID: number;
  mediaID: number;
  reason?: string;
  video: HTMLVideoElement | null;
}) {
  return JSON.stringify({
    play_session_id: playSessionID,
    work_id: workID,
    media_id: mediaID,
    reason: reason || undefined,
    ready_state: video?.readyState ?? null,
    network_state: video?.networkState ?? null,
    paused: video?.paused ?? null,
    ended: video?.ended ?? null,
    current_time: finiteNumber(video?.currentTime),
    duration: finiteNumber(video?.duration),
    buffered: summarizeTimeRanges(video?.buffered),
    current_src: summarizeMediaURL(video?.currentSrc || video?.getAttribute('src') || ''),
  });
}

function finiteNumber(value: number | undefined) {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function summarizeTimeRanges(ranges: TimeRanges | undefined) {
  if (!ranges || ranges.length === 0) return [];
  const result: { start: number; end: number }[] = [];
  for (let index = 0; index < Math.min(ranges.length, 4); index += 1) {
    try {
      result.push({ start: finiteNumber(ranges.start(index)), end: finiteNumber(ranges.end(index)) });
    } catch {
      break;
    }
  }
  return result;
}

function summarizeMediaURL(value: string) {
  if (!value) return '';
  try {
    const url = new URL(value, window.location.origin);
    return `${url.host}${url.pathname}${url.search}`;
  } catch {
    return value;
  }
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
