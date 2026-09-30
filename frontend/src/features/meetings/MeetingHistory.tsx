import { useState, useEffect, useCallback } from 'react';
import { Meeting, TranscriptEvent, MeetingSummary } from '../../types';
import { fetchTranscript, fetchMeeting, regenerateSummary } from '../../api/meetings';
import { useWebSocket } from '../../hooks/useWebSocket';
import { TranscriptPanel } from '../transcript/TranscriptPanel';

interface MeetingHistoryProps {
  meeting: Meeting;
  onBack: () => void;
}

export function MeetingHistory({ meeting, onBack }: MeetingHistoryProps) {
  const [transcript, setTranscript] = useState<TranscriptEvent[]>([]);
  const [summary, setSummary] = useState<MeetingSummary | null>(meeting.summary || null);
  const [regenerating, setRegenerating] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchTranscript(meeting.id)
      .then((data) => {
        if (!cancelled) {
          setTranscript(data);
          setError(null);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError('Failed to load transcript');
          console.error(err);
        }
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [meeting.id]);

  // Hydrate an already-persisted summary once (historical view via REST).
  useEffect(() => {
    let cancelled = false;
    fetchMeeting(meeting.id)
      .then((fresh) => {
        if (!cancelled && fresh.summary) {
          setSummary(fresh.summary);
        }
      })
      .catch((err) => console.error('Failed to load meeting', err));

    return () => {
      cancelled = true;
    };
  }, [meeting.id]);

  // If the summary is still being generated for this completed meeting,
  // receive it live over the WebSocket listener instead of polling REST.
  const handleMeetingSummary = useCallback((data: MeetingSummary) => {
    setSummary(data);
    setRegenerating(false);
  }, []);

  const handleRegenerate = useCallback(async () => {
    setRegenerating(true);
    setError(null);
    try {
      await regenerateSummary(meeting.id);
    } catch (err) {
      setRegenerating(false);
      setError('Failed to start summary generation');
      console.error(err);
    }
  }, [meeting.id]);

  useWebSocket({
    meetingId: meeting.id,
    onMeetingSummary: handleMeetingSummary,
  });

  return (
    <div className="h-screen flex flex-col bg-gray-50">
      <header className="bg-white border-b border-gray-200 px-6 py-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={onBack}
              className="text-gray-500 hover:text-gray-700"
            >
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 19l-7-7m0 0l7-7m-7 7h18" />
              </svg>
            </button>
            <div>
              <h1 className="text-xl font-semibold text-gray-900">{meeting.title}</h1>
              <p className="text-sm text-gray-500">
                <span className="inline-flex items-center gap-2">
                  <span className="px-2 py-0.5 bg-blue-100 text-blue-800 rounded-full text-xs font-medium">Completed</span>
                  {meeting.started_at && <>Started: {new Date(meeting.started_at).toLocaleString()}</>}
                  {meeting.ended_at && <> • Ended: {new Date(meeting.ended_at).toLocaleString()}</>}
                </span>
              </p>
            </div>
          </div>
        </div>
      </header>

      <div className="flex-1 flex overflow-hidden">
        <div className="flex-1 flex flex-col">
          <div className="px-4 py-3 bg-white border-b border-gray-200 flex items-center justify-between">
            <h2 className="text-lg font-medium text-gray-900">Meeting Transcript</h2>
            <span className="text-sm text-gray-500">
              {isLoading ? 'Loading...' : `${transcript.length} segments`}
            </span>
          </div>

          {error && (
            <div className="m-4 p-4 bg-red-50 text-red-700 rounded-lg">
              {error}
            </div>
          )}

          {!isLoading && !error && transcript.length === 0 && (
            <div className="flex-1 flex items-center justify-center">
              <p className="text-gray-500">No transcript recorded for this meeting.</p>
            </div>
          )}

          {!isLoading && transcript.length > 0 && (
            <TranscriptPanel
              transcript={transcript}
              partialText=""
              showEmptyHint={false}
              meetingStart={meeting.started_at}
            />
          )}
        </div>

        <div className="w-96 flex flex-col bg-white border-l border-gray-200">
          <div className="px-4 py-3 border-b border-gray-200">
            <h2 className="text-lg font-medium text-gray-900">Meeting Summary</h2>
          </div>
          <div className="flex-1 overflow-y-auto p-4">
            {summary ? (
              <SummaryPanel summary={summary} meeting={meeting} />
            ) : regenerating ? (
              <SummaryPending />
            ) : (
              <SummaryMissing onGenerate={handleRegenerate} />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function SummaryPanel({ summary, meeting }: { summary: MeetingSummary; meeting: Meeting }) {
  const momDate = formatMomDate(new Date(meeting.ended_at || meeting.started_at || Date.now()));

  return (
    <div className="space-y-5">
      {summary.mom_entries && summary.mom_entries.length > 0 ? (
        <div>
          <h3 className="text-sm font-medium text-gray-500 mb-1">Minutes of Meeting</h3>
          <div className="bg-gray-50 border border-gray-200 rounded-lg p-3">
            <p className="text-sm font-semibold text-gray-900 mb-2">
              {momDate} {summary.title || meeting.title} MOM:
            </p>
            <div className="space-y-2 text-sm">
              {summary.mom_entries.map((entry) => (
                <div key={entry.id}>
                  <p className="text-gray-900">
                    <span className="text-blue-500">•</span>{' '}
                    <span className="font-semibold">{entry.title}:</span> {entry.status}.
                  </p>
                  {(entry.actions || entry.eta || entry.assignee) && (
                    <p className="pl-4 mt-0.5 text-gray-600">
                      <span className="font-medium text-gray-700">Actions:</span>{' '}
                      {[entry.actions, entry.assignee ? `(${entry.assignee})` : null, entry.eta ? `ETA: ${entry.eta}` : null]
                        .filter(Boolean)
                        .join(', ')}
                    </p>
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>
      ) : (
        <>
          {summary.title && (
            <div>
              <h3 className="text-sm font-medium text-gray-500 mb-1">Title</h3>
              <p className="text-gray-900 font-medium">{summary.title}</p>
            </div>
          )}

          {summary.summary && (
            <div>
              <h3 className="text-sm font-medium text-gray-500 mb-1">Summary</h3>
              <p className="text-sm text-gray-700 whitespace-pre-line">{summary.summary}</p>
            </div>
          )}
        </>
      )}

      {summary.key_points && summary.key_points.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-gray-500 mb-2">Key Points</h3>
          <ul className="space-y-2">
            {summary.key_points.map((point, idx) => (
              <li key={idx} className="text-sm text-gray-700 flex gap-2">
                <span className="text-blue-500">•</span>
                <span>{point}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {summary.decisions && summary.decisions.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-gray-500 mb-2">Decisions</h3>
          <div className="space-y-2">
            {summary.decisions.map((d) => (
              <div key={d.id} className="p-3 bg-green-50 border border-green-200 rounded-lg">
                <p className="text-sm text-gray-900 font-medium">{d.title}</p>
                {d.description && <p className="mt-1 text-xs text-gray-600">{d.description}</p>}
              </div>
            ))}
          </div>
        </div>
      )}

      {summary.action_items && summary.action_items.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-gray-500 mb-2">Action Items</h3>
          <div className="space-y-2">
            {summary.action_items.map((a) => (
              <div key={a.id} className="p-3 bg-orange-50 border border-orange-200 rounded-lg">
                <p className="text-sm text-gray-900">{a.description}</p>
                {a.assignee && <p className="mt-1 text-xs text-gray-600">Assignee: {a.assignee}</p>}
              </div>
            ))}
          </div>
        </div>
      )}

      {summary.issues && summary.issues.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-gray-500 mb-2">Issues</h3>
          <div className="space-y-2">
            {summary.issues.map((i) => (
              <div key={i.id} className="p-3 bg-red-50 border border-red-200 rounded-lg">
                <p className="text-sm text-gray-900 font-medium">{i.title}</p>
                {i.description && <p className="mt-1 text-xs text-gray-600">{i.description}</p>}
              </div>
            ))}
          </div>
        </div>
      )}

      {summary.questions && summary.questions.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-gray-500 mb-2">Open Questions</h3>
          <div className="space-y-2">
            {summary.questions.map((q) => (
              <div key={q.id} className="p-3 bg-purple-50 border border-purple-200 rounded-lg">
                <p className="text-sm text-gray-900">{q.question}</p>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function SummaryPending() {
  return (
    <div className="py-8 text-center">
      <p className="text-gray-500 text-sm">Summary is being generated...</p>
      <p className="text-gray-400 text-xs mt-1">It will appear here automatically when ready.</p>
      <div className="mt-3 flex justify-center">
        <div className="w-5 h-5 border-2 border-gray-300 border-t-blue-500 rounded-full animate-spin" />
      </div>
    </div>
  );
}

function SummaryMissing({ onGenerate }: { onGenerate: () => void }) {
  return (
    <div className="py-8 text-center">
      <p className="text-gray-600 text-sm">No summary was generated for this meeting.</p>
      <p className="text-gray-400 text-xs mt-1">
        The transcript was preserved; you can generate the summary now.
      </p>
      <button
        onClick={onGenerate}
        className="mt-4 px-4 py-2 bg-blue-600 text-white text-sm font-medium rounded-lg hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
      >
        Generate Summary
      </button>
    </div>
  );
}

function formatMomDate(date: Date): string {
  const day = String(date.getDate()).padStart(2, '0');
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const year = date.getFullYear();
  return `${day}/${month}/${year}`;
}