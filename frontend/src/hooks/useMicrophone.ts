import { useRef, useCallback, useState } from 'react';

interface UseMicrophoneOptions {
  onAudioData?: (data: ArrayBuffer) => void;
  sampleRate?: number;
  channelCount?: number;
}

export function useMicrophone({
  onAudioData,
  sampleRate = 16000,
  channelCount = 1,
}: UseMicrophoneOptions = {}) {
  const streamRef = useRef<MediaStream | null>(null);
  const audioContextRef = useRef<AudioContext | null>(null);
  const processorRef = useRef<ScriptProcessorNode | null>(null);
  const [isActive, setIsActive] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const start = useCallback(async () => {
    try {
      // Request microphone access
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          sampleRate,
          channelCount,
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      });

      streamRef.current = stream;

      // Create audio context
      const audioContext = new AudioContext({
        sampleRate,
      });
      audioContextRef.current = audioContext;

      // Create audio source from stream
      const source = audioContext.createMediaStreamSource(stream);

      // Create script processor for capturing audio data
      // Buffer size 4096, input channels = channelCount, output channels = 0
      const processor = audioContext.createScriptProcessor(4096, channelCount, 0);
      processorRef.current = processor;

      processor.onaudioprocess = (event) => {
        const inputBuffer = event.inputBuffer;
        const inputData = inputBuffer.getChannelData(0);

        // Convert float32 to int16 (linear16)
        const int16Buffer = new Int16Array(inputData.length);
        for (let i = 0; i < inputData.length; i++) {
          const s = Math.max(-1, Math.min(1, inputData[i]));
          int16Buffer[i] = s < 0 ? s * 0x8000 : s * 0x7FFF;
        }

        // Send audio data
        onAudioData?.(int16Buffer.buffer);
      };

      // Connect nodes
      source.connect(processor);
      processor.connect(audioContext.destination);

      setIsActive(true);
      setError(null);

      return true;
    } catch (err) {
      console.error('Failed to start microphone:', err);
      setError(err instanceof Error ? err.message : 'Failed to access microphone');
      return false;
    }
  }, [sampleRate, channelCount, onAudioData]);

  const stop = useCallback(() => {
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

    setIsActive(false);
  }, []);

  const getStream = useCallback(() => {
    return streamRef.current;
  }, []);

  return {
    isActive,
    error,
    start,
    stop,
    getStream,
  };
}
