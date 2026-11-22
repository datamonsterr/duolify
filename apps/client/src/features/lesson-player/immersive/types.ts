// The immersive layer renders the player's existing state and calls its existing
// actions. Nothing here owns playback logic.
export type ImmersiveSlideInfo = {
  id: string;
  label: string;
  durationMs: number;
};

export type ImmersiveView = {
  title: string;
  lessonId: string;
  courseId: string;
  index: number;
  slides: ImmersiveSlideInfo[];
  playing: boolean;
  ended: boolean;
  /** Time in the current Slide, ms. */
  timeMs: number;
  /** Current Slide's effective duration, ms. */
  durationMs: number;
  rate: number;
  captions: boolean;
  muted: boolean;
  bookmarked: boolean;
  autoAdvance: boolean;
  failed: boolean;
  /** Playback controls are inert until the Slide has loaded. */
  disabled: boolean;
  offline: boolean;
  reducedMotion: boolean;
  caption: string;
};

export type ImmersiveActions = {
  toggle: () => void;
  pause: () => void;
  previous: () => void;
  next: () => void;
  goTo: (index: number) => void;
  replay: () => void;
  cycleSpeed: () => void;
  toggleCaptions: () => void;
  toggleMute: () => void;
  toggleBookmark: () => void;
  setAutoAdvance: (on: boolean) => void;
  exit: () => void;
};
