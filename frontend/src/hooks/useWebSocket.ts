import { useEffect, useRef, useCallback, useState } from 'react';
import { WebSocketEvent, TranscriptEvent, MeetingState } from '../types';

interface UseWebSocketOptions {
  meetingId: number;
  onTranscriptPartial?: (event: TranscriptEvent) => void;
  onTranscriptFinal?: (event: TranscriptEvent) => void;
  onMeetingStarted?: (data: any) => void;
  onMeetingEnded?: (data: any) => void;
  onMeetingPaused?: (data: any) => void;
  onMeetingResumed?: (data: any) => void;
  onStateUpdate?: (state: Partial<MeetingState>) => void;
  onMeetingSummary?: (data: any) => void;
  onMeetingSummaryFailed?: (data: { message?: string }) => void;
  onReconnected?: () => void;
  onError?: (error: Event) => void;
}

export function useWebSocket({
  meetingId,
  onTranscriptPartial,
  onTranscriptFinal,
  onMeetingStarted,
  onMeetingEnded,
  onMeetingPaused,
  onMeetingResumed,
  onStateUpdate,
  onMeetingSummary,
  onMeetingSummaryFailed,
  onReconnected,
  onError,
}: UseWebSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [isReconnecting, setIsReconnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Store callbacks in refs to avoid re-creating WebSocket on every render
  const callbacksRef = useRef({
    onTranscriptPartial,
    onTranscriptFinal,
    onMeetingStarted,
    onMeetingEnded,
    onMeetingPaused,
    onMeetingResumed,
    onStateUpdate,
    onMeetingSummary,
    onMeetingSummaryFailed,
    onReconnected,
    onError,
  });
  callbacksRef.current = {
    onTranscriptPartial,
    onTranscriptFinal,
    onMeetingStarted,
    onMeetingEnded,
    onMeetingPaused,
    onMeetingResumed,
    onStateUpdate,
    onMeetingSummary,
    onMeetingSummaryFailed,
    onReconnected,
    onError,
  };

  useEffect(() => {
    let disposed = false;
    let attempt = 0;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    // Distinguishes the first connection from later ones so backfill
    // (onReconnected) only runs after an actual drop.
    let hasConnectedOnce = false;

    // Close any existing connection before opening a new one
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/meeting/${meetingId}`;

    const clearReconnectTimer = () => {
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
    };

    const connect = () => {
      if (disposed) return;
      clearReconnectTimer();

      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        if (disposed) return;
        console.log('WebSocket connected');
        const wasReconnect = hasConnectedOnce;
        hasConnectedOnce = true;
        attempt = 0;
        setIsConnected(true);
        setIsReconnecting(false);
        setError(null);
        if (wasReconnect) {
          callbacksRef.current.onReconnected?.();
        }
      };

      ws.onmessage = (event) => {
        if (disposed) return;
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
            case 'meeting.paused':
              cbs.onMeetingPaused?.(data.data);
              break;
            case 'meeting.resumed':
              cbs.onMeetingResumed?.(data.data);
              break;
            case 'state.updated':
              cbs.onStateUpdate?.(data.data);
              break;
            case 'meeting.summary':
              cbs.onMeetingSummary?.(data.data);
              break;
            case 'meeting.summary.failed':
              cbs.onMeetingSummaryFailed?.(data.data);
              break;
            default:
              console.log('Unknown event type:', data.type);
          }
        } catch (err) {
          console.error('Failed to parse WebSocket message:', err);
        }
      };

      ws.onerror = (event) => {
        if (disposed) return;
        console.error('WebSocket error:', event);
        setError('WebSocket connection error');
        callbacksRef.current.onError?.(event);
      };

      ws.onclose = () => {
        if (disposed) return;
        console.log('WebSocket disconnected');
        setIsConnected(false);
        if (wsRef.current === ws) {
          wsRef.current = null;
        }
        scheduleReconnect();
      };
    };

    // Exponential backoff: 1s, 2s, 4s, 8s, capped at 15s. The counter
    // resets on a successful open, so brief blips recover quickly without
    // hammering a server that stays down.
    const scheduleReconnect = () => {
      if (disposed) return;
      setIsReconnecting(true);
      const delay = Math.min(1000 * 2 ** attempt, 15000);
      attempt += 1;
      reconnectTimer = setTimeout(connect, delay);
    };

    // A restored network (wifi back, laptop wake) should reconnect
    // immediately instead of waiting out the backoff.
    const handleOnline = () => {
      if (disposed || wsRef.current) return;
      clearReconnectTimer();
      attempt = 0;
      connect();
    };

    connect();
    window.addEventListener('online', handleOnline);
    return () => {
      disposed = true;
      clearReconnectTimer();
      window.removeEventListener('online', handleOnline);
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [meetingId]);

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

  return {
    isConnected,
    isReconnecting,
    error,
    sendAudio,
    sendCommand,
  };
}
