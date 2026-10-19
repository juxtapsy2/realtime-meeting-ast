import { useRef, useCallback, useState } from 'react';

interface UseMicrophoneOptions {
  onAudioData?: (data: ArrayBuffer) => void;
  sampleRate?: number;
  channelCount?: number;
}

export type AudioSource = 'microphone' | 'system';

export function useMicrophone({
  onAudioData,
  sampleRate = 16000,
  channelCount = 1,
}: UseMicrophoneOptions = {}) {
  const streamRef = useRef<MediaStream | null>(null);
  const audioContextRef = useRef<AudioContext | null>(null);
  const processorRef = useRef<ScriptProcessorNode | null>(null);
  const watchdogRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const visibilityHandlerRef = useRef<(() => void) | null>(null);
  const [isActive, setIsActive] = useState(false);
  const [source, setSource] = useState<AudioSource | null>(null);
  const [error, setError] = useState<string | null>(null);

  const clearWatchdog = useCallback(() => {
    if (watchdogRef.current !== null) {
      clearInterval(watchdogRef.current);
      watchdogRef.current = null;
    }
    if (visibilityHandlerRef.current !== null) {
      document.removeEventListener('visibilitychange', visibilityHandlerRef.current);
      visibilityHandlerRef.current = null;
    }
  }, []);

  const stopInternal = useCallback(() => {
    clearWatchdog();

    // Stop processor
    if (processorRef.current) {
      processorRef.current.disconnect();
      processorRef.current = null;
    }

    // Close audio context
    if (audioContextRef.current) {
      audioContextRef.current.close();
      audioContextRef.current = null;
    }

    // Stop stream tracks
    if (streamRef.current) {
      streamRef.current.getTracks().forEach((track) => track.stop());
      streamRef.current = null;
    }
  }, [clearWatchdog]);

  const start = useCallback(
    async (nextSource: AudioSource) => {
      // Switch sources: stop whatever is currently capturing.
      stopInternal();
      setIsActive(false);
      setSource(null);

      try {
        let stream: MediaStream;

        if (nextSource === 'microphone') {
          stream = await navigator.mediaDevices.getUserMedia({
            audio: {
              sampleRate,
              channelCount,
              echoCancellation: true,
              noiseSuppression: true,
              autoGainControl: true,
            },
          });
        } else {
          // Capture system/tab audio so playback through headphones is
          // picked up even when no microphone signal reaches the mic.
          const displayStream = await navigator.mediaDevices.getDisplayMedia({
            audio: true,
            video: true,
          } as MediaStreamConstraints);

          // We only need the audio; discard video tracks (requesting a video
          // track is required for the browser picker to include audio).
          displayStream.getVideoTracks().forEach((track) => track.stop());
          stream = new MediaStream(displayStream.getAudioTracks());
        }

        streamRef.current = stream;

        // Create audio context
        const audioContext = new AudioContext({
          sampleRate,
        });
        audioContextRef.current = audioContext;

        // Create audio source from stream
        const sourceNode = audioContext.createMediaStreamSource(stream);

        // Create script processor for capturing audio data
        // Buffer size 4096, input channels = channelCount, output channels = 1
        const processor = audioContext.createScriptProcessor(4096, channelCount, 1);
        processorRef.current = processor;

        processor.onaudioprocess = (event) => {
          const inputBuffer = event.inputBuffer;
          const inputData = inputBuffer.getChannelData(0);

          // Convert float32 to int16 (linear16)
          const int16Buffer = new Int16Array(inputData.length);
          for (let i = 0; i < inputData.length; i++) {
            const s = Math.max(-1, Math.min(1, inputData[i]));
            int16Buffer[i] = s < 0 ? s * 0x8000 : s * 0x7fff;
          }

          // Send audio data
          onAudioData?.(int16Buffer.buffer);
        };

        // Connect source to processor, processor to a silent gain node (activates processing without playback feedback)
        const silentGain = audioContext.createGain();
        silentGain.gain.value = 0;
        sourceNode.connect(processor);
        processor.connect(silentGain);
        silentGain.connect(audioContext.destination);

        // Track OS/browser-level capture failures. ScriptProcessor keeps
        // firing even when the context is suspended or a device is
        // unplugged, so without this the UI shows an active mic while
        // no audio reaches onAudioData (the meeting-20 failure mode).
        clearWatchdog();
        watchdogRef.current = setInterval(() => {
          const ctx = audioContextRef.current;
          const current = streamRef.current;
          if (!ctx || !current) return;

          if (ctx.state === 'suspended') {
            ctx.resume().catch((err) => {
              console.error('Failed to resume AudioContext:', err);
            });
          }

          const liveTrack = current.getAudioTracks().some((track) => track.readyState === 'live');
          if (!liveTrack || ctx.state === 'closed') {
            console.error('Audio capture stopped unexpectedly');
            stopInternal();
            setIsActive(false);
            setSource(null);
            setError('Audio capture stopped unexpectedly. Please restart the microphone.');
          }
        }, 2000);

        // Background tabs throttle timers AND suspend AudioContexts, so
        // resume eagerly when the tab becomes visible again instead of
        // waiting for the (also-throttled) watchdog.
        const handleVisibility = () => {
          if (document.visibilityState === 'visible' && audioContextRef.current?.state === 'suspended') {
            audioContextRef.current.resume().catch((err) => {
              console.error('Failed to resume AudioContext:', err);
            });
          }
        };
        visibilityHandlerRef.current = handleVisibility;
        document.addEventListener('visibilitychange', handleVisibility);

        setIsActive(true);
        setSource(nextSource);
        setError(null);

        return true;
      } catch (err) {
        console.error('Failed to start audio capture:', err);
        setError(err instanceof Error ? err.message : 'Failed to access audio');
        return false;
      }
    },
    [sampleRate, channelCount, onAudioData, stopInternal, clearWatchdog]
  );

  const stop = useCallback(() => {
    stopInternal();
    setIsActive(false);
    setSource(null);
  }, [stopInternal]);

  const getStream = useCallback(() => {
    return streamRef.current;
  }, []);

  return {
    isActive,
    source,
    error,
    start,
    stop,
    getStream,
  };
}