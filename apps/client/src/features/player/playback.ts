export type PlaybackState = {
  time: number;
  playing: boolean;
  ended: boolean;
  watch: number;
  rate: number;
};

// Parent-owned media clock. Watch time counts real playing time, never seeks.
export class PlaybackClock {
  private state: PlaybackState;
  private last = 0;
  private listeners = new Set<() => void>();
  constructor(
    private duration: number,
    time: number,
  ) {
    this.state = {
      time: Math.max(0, Math.min(duration, time)),
      playing: false,
      ended: time === duration,
      watch: 0,
      rate: 1,
    };
  }
  snapshot = () => this.state;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  private update(value: Partial<PlaybackState>) {
    this.state = { ...this.state, ...value };
    this.listeners.forEach((listener) => listener());
  }
  tick(now: number) {
    if (!this.state.playing) return;
    const delta = Math.max(0, now - this.last);
    this.last = now;
    const watched = Math.min(
      delta,
      (this.duration - this.state.time) / this.state.rate,
    );
    const time = Math.min(
      this.duration,
      this.state.time + delta * this.state.rate,
    );
    this.update({
      time,
      watch: this.state.watch + watched,
      playing: time < this.duration,
      ended: time === this.duration,
    });
  }
  play(now: number) {
    if (this.state.ended) return;
    this.last = now;
    this.update({ playing: true });
  }
  pause(now: number) {
    this.tick(now);
    this.update({ playing: false });
  }
  seek(time: number, now: number) {
    if (!Number.isFinite(time)) return;
    this.tick(now);
    this.update({
      time: Math.max(0, Math.min(this.duration, time)),
      ended: false,
    });
  }
  rate(rate: number, now: number) {
    if (![0.75, 1, 1.25, 1.5, 2].includes(rate)) return;
    this.tick(now);
    this.update({ rate });
  }
  replay(now: number) {
    this.last = now;
    this.update({ time: 0, playing: true, ended: false, watch: 0 });
  }
  end() {
    if (this.state.ended) return false;
    this.update({ time: this.duration, playing: false, ended: true });
    return true;
  }
}
