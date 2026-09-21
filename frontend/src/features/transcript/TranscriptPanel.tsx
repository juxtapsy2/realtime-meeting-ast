import React, { useEffect, useRef } from 'react';
import { TranscriptEvent } from '../../types';

interface TranscriptPanelProps {
  transcript: TranscriptEvent[];
  partialText: string;
}

export function TranscriptPanel({ transcript, partialText }: TranscriptPanelProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [transcript, partialText]);

  const formatTime = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = Math.floor(seconds % 60);
    return `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
  };

  return (
    <div
      ref={containerRef}
      className="flex-1 overflow-y-auto p-4 space-y-4"
    >
      {transcript.length === 0 && !partialText && (
        <div className="text-center text-gray-500 py-12">
          <p>Waiting for transcript...</p>
          <p className="text-sm mt-2">Start the meeting and enable your microphone</p>
        </div>
      )}

      {transcript.map((event) => (
        <div
          key={event.segment_id}
          className="flex gap-3"
        >
          <div className="flex-shrink-0 w-16 text-xs text-gray-500 pt-1">
            {formatTime(event.start_time)}
          </div>
          <div className="flex-1">
            <div className="inline-block px-3 py-2 bg-gray-100 rounded-lg">
              <p className="text-gray-900">{event.text}</p>
            </div>
            {event.confidence !== undefined && (
              <div className="mt-1 text-xs text-gray-400">
                Confidence: {Math.round(event.confidence * 100)}%
              </div>
            )}
          </div>
        </div>
      ))}

      {partialText && (
        <div className="flex gap-3 opacity-60">
          <div className="flex-shrink-0 w-16 text-xs text-gray-500 pt-1">
            --
          </div>
          <div className="flex-1">
            <div className="inline-block px-3 py-2 bg-yellow-50 border border-yellow-200 rounded-lg">
              <p className="text-gray-700 italic">{partialText}</p>
            </div>
            <div className="mt-1 text-xs text-gray-400">
              Listening...
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
