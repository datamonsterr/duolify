import { BITS_PER_SECOND, pickContainer } from "./recorder";

/** One live recording. Audio exists only in memory; nothing here touches storage. */
export type RecorderHandle = {
  /** Stop and return the recording. Every microphone track is released. */
  stop(): Promise<Blob>;
  /** Stop and drop the audio. Every microphone track is released. */
  cancel(): void;
  /** Current level 0..1; also feeds the peak used by the silence gate. */
  level(): number;
  /** Highest level seen so far. */
  peak(): number;
};
export type VoicePlatform = {
  supported(): boolean;
  /** Rejects with a `NotAllowedError`-named error when the Learner denies the microphone. */
  start(): Promise<RecorderHandle>;
};

export const browserPlatform: VoicePlatform = {
  supported: () =>
    typeof navigator !== "undefined" &&
    !!navigator.mediaDevices?.getUserMedia &&
    typeof MediaRecorder !== "undefined",
  async start() {
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true },
    });
    const release = () => stream.getTracks().forEach((track) => track.stop());
    try {
      const mimeType = pickContainer((type) =>
        MediaRecorder.isTypeSupported(type),
      );
      // Safari does not document the bitrate options, so the Blob size is checked as well.
      const recorder = new MediaRecorder(stream, {
        ...(mimeType ? { mimeType } : {}),
        audioBitsPerSecond: BITS_PER_SECOND,
      });
      const chunks: Blob[] = [];
      recorder.ondataavailable = (event) => {
        if (event.data.size) chunks.push(event.data);
      };
      let context: AudioContext | undefined;
      let analyser: AnalyserNode | undefined;
      try {
        context = new AudioContext();
        analyser = context.createAnalyser();
        analyser.fftSize = 512;
        context.createMediaStreamSource(stream).connect(analyser);
        // WebKit starts a context created after a permission prompt suspended.
        void context.resume?.().catch(() => {});
      } catch {
        /* No meter: the recording itself still works. */
      }
      const samples = new Uint8Array(analyser?.fftSize ?? 0);
      let top = 0;
      // Without a running meter the silence gate has nothing to judge: report a full level.
      let metered = false;
      const close = () => {
        release();
        void context?.close().catch(() => {});
      };
      recorder.start();
      return {
        level() {
          if (!analyser) return 0;
          if (context?.state === "running") metered = true;
          analyser.getByteTimeDomainData(samples);
          let peak = 0;
          for (const s of samples) peak = Math.max(peak, Math.abs(s - 128));
          const value = Math.min(1, (peak / 128) * 1.5);
          top = Math.max(top, value);
          return value;
        },
        peak: () => (metered ? top : 1),
        stop: () =>
          new Promise<Blob>((resolve) => {
            recorder.onstop = () => {
              close();
              resolve(
                new Blob(chunks, {
                  type: recorder.mimeType || mimeType || "audio/webm",
                }),
              );
            };
            if (recorder.state === "inactive")
              recorder.onstop(new Event("stop"));
            else recorder.stop();
          }),
        cancel() {
          recorder.ondataavailable = null;
          recorder.onstop = null;
          if (recorder.state !== "inactive") recorder.stop();
          chunks.length = 0;
          close();
        },
      };
    } catch (error) {
      release();
      throw error;
    }
  },
};
