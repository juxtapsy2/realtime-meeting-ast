import { useEffect, useRef, useCallback, useState } from 'react';
import { WebSocketEvent, TranscriptEvent, MeetingState } from '../types';

interface UseWebSocketOptions {
  meetingId: number;
  onTranscriptPartial?: (event: TranscriptEvent) => void;
  onTranscriptFinal?: (event: TranscriptEvent) => void;
  onMeetingStarted?: (data: any) => void;
  onMeetingEnded?: (data: any) => void;
  onStateUpdate?: (state: Partial<MeetingState>) => void;
  onError?: (error: Event) => void;
}

export function useWebSocket({
  meetingId,
  onTranscriptPartial,
  onTranscriptFinal,
  onMeetingStarted,
  onMeetingEnded,
  onStateUpdate,
  onError,
}: UseWebSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Store callbacks in refs to avoid re-creating WebSocket on every render
  const callbacksRef = useRef({
    onTranscriptPartial,
    onTranscriptFinal,
    onMeetingStarted,
    onMeetingEnded,
    onStateUpdate,
    onError,
  });
  callbacksRef.current = {
    onTranscriptPartial,
    onTranscriptFinal,
    onMeetingStarted,
    onMeetingEnded,
    onStateUpdate,
    onError,
  };

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      return;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/meeting/${meetingId}`;

    const ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      console.log('WebSocket connected');
      setIsConnected(true);
      setError(null);
    };

    ws.onmessage = (event) => {
      try {
        const data: WebSocketEvent = JSON.parse(event.data);
        const cbs = callbacksRef.current;

        switch (data.type) {
          case 'transcript.partial':
            cbs.onTranscriptPartial?.(data.data);
            break;
          case 'transcript.final':
            cbs.onTranscriptFinal?.(data.data);
            break;
          case 'meeting.started':
            cbs.onMeetingStarted?.(data.data);
            break;
          case 'meeting.ended':
            cbs.onMeetingEnded?.(data.data);
            break;
          case 'state.updated':
            cbs.onStateUpdate?.(data.data);
            break;
          default:
            console.log('Unknown event type:', data.type);
        }
      } catch (err) {
        console.error('Failed to parse WebSocket message:', err);
      }
    };

    ws.onerror = (event) => {
      console.error('WebSocket error:', event);
      setError('WebSocket connection error');
      callbacksRef.current.onError?.(event);
    };

    ws.onclose = () => {
      console.log('WebSocket disconnected');
      setIsConnected(false);
      wsRef.current = null;
    };

    wsRef.current = ws;
  }, [meetingId]);

  const disconnect = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
  }, []);

  const sendAudio = useCallback((audioData: ArrayBuffer) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(audioData);
    }
  }, []);

  const sendCommand = useCallback((type: string, data?: Record<string, unknown>) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type, ...data }));
    }
  }, []);

  useEffect(() => {
    connect();
    return () => {
      disconnect();
    };
  }, [connect, disconnect]);

  return {
    isConnected,
    error,
    sendAudio,
    sendCommand,
  };
}
