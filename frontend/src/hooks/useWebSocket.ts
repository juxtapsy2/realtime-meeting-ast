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

        switch (data.type) {
          case 'transcript.partial':
            onTranscriptPartial?.(data.data);
            break;
          case 'transcript.final':
            onTranscriptFinal?.(data.data);
            break;
          case 'meeting.started':
            onMeetingStarted?.(data.data);
            break;
          case 'meeting.ended':
            onMeetingEnded?.(data.data);
            break;
          case 'state.updated':
            onStateUpdate?.(data.data);
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
      onError?.(event);
    };

    ws.onclose = () => {
      console.log('WebSocket disconnected');
      setIsConnected(false);
      wsRef.current = null;
    };

    wsRef.current = ws;
  }, [meetingId, onTranscriptPartial, onTranscriptFinal, onMeetingStarted, onMeetingEnded, onStateUpdate, onError]);

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

  const sendCommand = useCallback((type: string, data?: any) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type, ...data }));
    }
  }, []);

  useEffect(() => {
    return () => {
      disconnect();
    };
  }, [disconnect]);

  return {
    isConnected,
    error,
    connect,
    disconnect,
    sendAudio,
    sendCommand,
  };
}
