import { useEffect, useRef } from 'react';
import { TranscriptEvent } from '../../types';

interface TranscriptPanelProps {
  transcript: TranscriptEvent[];
  partialText: string;
  showEmptyHint?: boolean;
  /** Meeting start (ISO). When provided, timestamps render as UTC+7 wall-clock times. */
  meetingStart?: string;
}

const BANGKOK_TZ = 'Asia/Bangkok';

const clockFormatter = new Intl.DateTimeFormat('en-GB', {
  timeZone: BANGKOK_TZ,
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
});

const formatElapsed = (seconds: number) => {
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
};

function formatTime(seconds: number, meetingStart?: string): string {
  if (!meetingStart) return formatElapsed(seconds);
  const base = new Date(meetingStart).getTime();
  if (Number.isNaN(base)) return formatElapsed(seconds);
  return clockFormatter.format(new Date(base + seconds * 1000));
}

export function TranscriptPanel({ transcript, partialText, showEmptyHint = true, meetingStart }: TranscriptPanelProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [transcript, partialText]);

  return (
    <div
      ref={containerRef}
      className="flex-1 overflow-y-auto p-4 space-y-4"
    >
      {transcript.length === 0 && !partialText && showEmptyHint && (
        <div className="text-center text-muted py-12">
          <p>Waiting for transcript...</p>
          <p className="text-sm mt-2 text-muted">Start the meeting and enable your microphone</p>
        </div>
      )}

      {transcript.map((event, index) => (
        <div
          key={`${index}:${event.segment_id}`}
          className="flex gap-3"
        >
          <div className="flex-shrink-0 w-16 text-xs text-muted pt-1">
            {formatTime(event.start_time, meetingStart)}
          </div>
          <div className="flex-1">
            <div className="inline-block px-3 py-2 bg-canvas border border-steel/40 rounded-lg">
              <p className="text-sm text-ink leading-relaxed">{event.text}</p>
            </div>
            {event.confidence !== undefined && (
              <div className="mt-1 text-xs text-muted">
                Confidence: {Math.round(event.confidence * 100)}%
              </div>
            )}
          </div>
        </div>
      ))}

      {partialText && (
        <div className="flex gap-3 opacity-60">
          <div className="flex-shrink-0 w-16 text-xs text-muted pt-1">
            --
          </div>
          <div className="flex-1">
            <div className="inline-block px-3 py-2 bg-brand-100 border border-brand-300 rounded-lg">
              <p className="text-sm text-muted italic leading-relaxed">{partialText}</p>
            </div>
            <div className="mt-1 text-xs text-muted">
              Listening...
            </div>
          </div>
        </div>
      )}
    </div>
  );
}