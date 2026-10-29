import { useState, useEffect, useCallback } from 'react';
import { Meeting, TranscriptEvent, MeetingSummary, MeetingDocument } from '../../types';
import {
  fetchTranscript,
  fetchMeeting,
  regenerateSummary,
  uploadMeetingDocuments,
  clearMeetingDocuments,
} from '../../api/meetings';
import { useWebSocket } from '../../hooks/useWebSocket';
import { TranscriptPanel } from '../transcript/TranscriptPanel';
import { DocumentsPanel } from './DocumentsPanel';

interface MeetingHistoryProps {
  meeting: Meeting;
  onBack: () => void;
}

// The server auto-starts MoM generation the moment a meeting is completed.
// Generation is short (seconds), so a completed meeting without a summary and
// with a recent end time is almost certainly mid-generation: show the pending
// spinner for that window instead of "no summary". The spinner only applies to
// meetings that have a transcript: an empty recording (an upload-for-MoM
// meeting) returns from generation silently, and document uploads happen
// outside any generation run — there the upload panel and Generate button
// stay visible and the user starts the run explicitly.
const SUMMARY_AUTO_PENDING_MS = 2 * 60 * 1000;
// Safety cap: the server broadcasts meeting.summary / meeting.summary.failed
// on both outcomes, so if neither arrives (e.g. the server died mid-run) the
// UI falls back to manual generation instead of spinning forever.
const SUMMARY_AUTO_PENDING_MAX_MS = 30 * 1000;

// Meetings just stepped out of the room carry a completion status without an
// ended_at timestamp yet; those count as freshly ended too.
const autoPendingOnOpen = (meeting: Meeting): boolean => {
  const { status, summary, ended_at } = meeting;
  if (status !== 'completed' || summary) return false;
  if (!ended_at) return true;
  return Date.now() - new Date(ended_at).getTime() < SUMMARY_AUTO_PENDING_MS;
};

export function MeetingHistory({ meeting, onBack }: MeetingHistoryProps) {
  const [transcript, setTranscript] = useState<TranscriptEvent[]>([]);
  const [summary, setSummary] = useState<MeetingSummary | null>(meeting.summary || null);
  const [autoPending, setAutoPending] = useState<boolean>(autoPendingOnOpen(meeting));
  const [documents, setDocuments] = useState<MeetingDocument[]>(meeting.documents || []);
  const [regenerating, setRegenerating] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  // Persistent failure notice: the toast is gone after 6s, but a user who
  // returns to the tab later must still see why there is no summary.
  const [summaryError, setSummaryError] = useState<string | null>(null);
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
        if (!cancelled) {
          if (fresh.summary) {
            setSummary(fresh.summary);
          }
          setDocuments(fresh.documents || []);
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
    setSummaryError(null);
    setToast(null);
  }, []);

  // Generation failed server-side: stop the pending state and tell the user
  // why, instead of leaving them waiting on a spinner that will never resolve.
  const handleSummaryFailed = useCallback((data: { message?: string }) => {
    const message = data?.message || 'Summary generation failed. Please retry.';
    setAutoPending(false);
    setRegenerating(false);
    setSummaryError(message);
    setToast(message);
  }, []);

  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(null), 6000);
    return () => clearTimeout(timer);
  }, [toast]);

  // See SUMMARY_AUTO_PENDING_MAX_MS above.
  useEffect(() => {
    if (!autoPending) return;
    const timer = setTimeout(() => setAutoPending(false), SUMMARY_AUTO_PENDING_MAX_MS);
    return () => clearTimeout(timer);
  }, [autoPending]);

  const handleRegenerate = useCallback(async () => {
    setRegenerating(true);
    setError(null);
    setSummaryError(null);
    try {
      await regenerateSummary(meeting.id);
    } catch (err) {
      setRegenerating(false);
      setSummaryError('Failed to start summary generation');
      setError('Failed to start summary generation');
      console.error(err);
    }
  }, [meeting.id]);

  const handleUploadDocuments = useCallback(
    async (files: File[]) => {
      const docs = await uploadMeetingDocuments(meeting.id, files);
      setDocuments(docs);
    },
    [meeting.id],
  );

  const handleClearDocuments = useCallback(async () => {
    await clearMeetingDocuments(meeting.id);
    setDocuments([]);
  }, [meeting.id]);

  // A summary (or regeneration) may have completed while the socket was
  // down; the WS event is gone, so resync from REST on reconnect.
  const handleReconnected = useCallback(() => {
    fetchMeeting(meeting.id)
      .then((fresh) => {
        if (fresh.summary) {
          setSummary(fresh.summary);
          // A later retry succeeded while the socket was down; the old
          // failure banner must not outlive it.
          setSummaryError(null);
        }
        setDocuments(fresh.documents || []);
      })
      .catch((err) => console.error('Failed to resync meeting', err));
  }, [meeting.id]);

  useWebSocket({
    meetingId: meeting.id,
    onMeetingSummary: handleMeetingSummary,
    onMeetingSummaryFailed: handleSummaryFailed,
    onReconnected: handleReconnected,
  });

  return (
    <div className="flex-1 min-h-0 flex flex-col bg-shell">
      <header className="bg-canvas border-b border-steel/30 px-6 py-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={onBack}
              className="text-muted hover:text-ink transition-colors"
            >
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 19l-7-7m0 0l7-7m-7 7h18" />
              </svg>
            </button>
            <div>
              <h1 className="text-xl font-semibold tracking-tight text-ink">{meeting.title}</h1>
              <p className="text-sm text-muted">
                <span className="inline-flex items-center gap-2">
                  <span className="px-2 py-0.5 bg-canvas border border-steel/40 rounded-full text-xs font-medium text-ink">Completed</span>
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
          <div className="px-4 py-3 bg-canvas border-b border-steel/30 flex items-center justify-between">
            <h2 className="text-lg font-medium text-ink">Meeting Transcript</h2>
            <span className="text-sm text-muted">
              {isLoading ? 'Loading...' : `${transcript.length} segments`}
            </span>
          </div>

          {error && (
            <div className="m-4 p-4 bg-coral-100 text-coral-700 rounded-lg border border-coral-300">
              {error}
            </div>
          )}

          {!isLoading && !error && transcript.length === 0 && (
            <div className="flex-1 flex flex-col items-center justify-center gap-1">
              <p className="text-muted">No transcript recorded for this meeting.</p>
              <p className="text-xs text-muted">
                Upload meeting documents on the right to generate the summary from files instead.
              </p>
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

        <div className="w-96 flex flex-col bg-canvas border-l border-steel/30">
          <div className="px-4 py-3 border-b border-steel/30">
            <h2 className="text-lg font-medium text-ink">Meeting Summary</h2>
          </div>
          <div className="max-h-64 overflow-y-auto border-b border-steel/30">
            {summary ? null : (
              <DocumentsPanel
                documents={documents}
                onUpload={handleUploadDocuments}
                onClear={handleClearDocuments}
              />
            )}
          </div>
          <div className="flex-1 overflow-y-auto p-4">
            {summaryError && (
              <div
                role="alert"
                className="mb-4 p-3 bg-coral-100 border border-coral-300 rounded-lg text-sm text-coral-700"
              >
                <span className="font-semibold">Summary generation failed.</span>{' '}
                {summaryError}
              </div>
            )}
            {summary ? (
              <SummaryPanel summary={summary} meeting={meeting} />
            ) : regenerating || (autoPending && (isLoading || transcript.length > 0)) ? (
              <SummaryPending />
            ) : (
              <SummaryMissing
                onGenerate={handleRegenerate}
                canGenerate={transcript.length > 0 || documents.length > 0}
              />
            )}
          </div>
        </div>
      </div>

      {toast && (
        <div
          role="alert"
          className="fixed top-4 right-4 z-50 max-w-sm bg-ink text-canvas text-sm rounded-lg shadow-lg px-4 py-3"
        >
          {toast}
        </div>
      )}
    </div>
  );
}

function SummaryPanel({ summary, meeting }: { summary: MeetingSummary; meeting: Meeting }) {
  const momDate = formatMomDate(new Date(meeting.ended_at || meeting.started_at || Date.now()));

  return (
    <div className="space-y-5">
      {summary.mom_entries && summary.mom_entries.length > 0 ? (
        <div>
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted mb-1">Minutes of Meeting</h3>
          <div className="bg-wash/30 border border-steel/30 rounded-lg p-3">
            <p className="text-sm font-semibold text-ink mb-2">
              {momDate} {summary.title || meeting.title} MOM:
            </p>
            <div className="space-y-2 text-sm">
              {summary.mom_entries.map((entry, idx) => (
                <div key={entry.id || `entry-${idx}`}>
                  <p className="text-ink">
                    <span className="text-brand-500">•</span>{' '}
                    <span className="font-semibold">
                      {entry.id ? `[${entry.id}] ` : ''}
                      {entry.title}:
                    </span>{' '}
                    {entry.status}.
                  </p>
                  {(entry.actions || entry.eta || entry.assignee) && (
                    <p className="pl-4 mt-0.5 text-muted">
                      <span className="font-medium text-ink">Actions:</span>{' '}
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
              <h3 className="text-xs font-semibold uppercase tracking-wider text-muted mb-1">Title</h3>
              <p className="text-ink font-medium">{summary.title}</p>
            </div>
          )}

          {summary.summary && (
            <div>
              <h3 className="text-xs font-semibold uppercase tracking-wider text-muted mb-1">Summary</h3>
              <p className="text-sm text-ink whitespace-pre-line">{summary.summary}</p>
            </div>
          )}
        </>
      )}

      {summary.key_points && summary.key_points.length > 0 && (
        <div>
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted mb-2">Key Points</h3>
          <ul className="space-y-2">
            {summary.key_points.map((point, idx) => (
              <li key={idx} className="text-sm text-ink flex gap-2">
                <span className="text-brand-500">•</span>
                <span>{point}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {summary.decisions && summary.decisions.length > 0 && (
        <div>
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted mb-2">Decisions</h3>
          <div className="space-y-2">
            {summary.decisions.map((d) => (
              <div key={d.id} className="p-3 bg-canvas border border-steel/30 border-l-2 border-l-ink rounded-lg">
                <p className="text-sm text-ink font-medium">{d.title}</p>
                {d.description && <p className="mt-1 text-xs text-muted">{d.description}</p>}
              </div>
            ))}
          </div>
        </div>
      )}

      {summary.action_items && summary.action_items.length > 0 && (
        <div>
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted mb-2">Action Items</h3>
          <div className="space-y-2">
            {summary.action_items.map((a) => (
              <div key={a.id} className="p-3 bg-canvas border border-steel/30 border-l-2 border-l-brand-500 rounded-lg">
                <p className="text-sm text-ink">{a.description}</p>
                {a.assignee && <p className="mt-1 text-xs text-muted">Assignee: {a.assignee}</p>}
              </div>
            ))}
          </div>
        </div>
      )}

      {summary.issues && summary.issues.length > 0 && (
        <div>
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted mb-2">Issues</h3>
          <div className="space-y-2">
            {summary.issues.map((i) => (
              <div key={i.id} className="p-3 bg-canvas border border-steel/30 border-l-2 border-l-coral-500 rounded-lg">
                <p className="text-sm text-ink font-medium">{i.title}</p>
                {i.description && <p className="mt-1 text-xs text-muted">{i.description}</p>}
              </div>
            ))}
          </div>
        </div>
      )}

      {summary.questions && summary.questions.length > 0 && (
        <div>
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted mb-2">Open Questions</h3>
          <div className="space-y-2">
            {summary.questions.map((q) => (
              <div key={q.id} className="p-3 bg-canvas border border-steel/30 border-l-2 border-l-steel rounded-lg">
                <p className="text-sm text-ink">{q.question}</p>
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
      <p className="text-muted text-sm">Summary is being generated...</p>
      <p className="text-muted text-xs mt-1">It will appear here automatically when ready.</p>
      <div className="mt-3 flex justify-center">
        <div className="w-5 h-5 border-2 border-steel/40 border-t-brand-500 rounded-full animate-spin" />
      </div>
    </div>
  );
}

function SummaryMissing({ onGenerate, canGenerate }: { onGenerate: () => void; canGenerate: boolean }) {
  return (
    <div className="py-8 text-center">
      <p className="text-ink text-sm">No summary was generated for this meeting.</p>
      <p className="text-muted text-xs mt-1">
        {canGenerate
          ? 'The transcript or uploaded documents are preserved; you can generate the summary now.'
          : 'Upload meeting documents or use a transcript to have evidence to summarize.'}
      </p>
      <button
        onClick={onGenerate}
        disabled={!canGenerate}
        className="mt-4 px-4 py-2 bg-brand-500 text-ink text-sm font-semibold rounded-lg hover:bg-brand-600 hover:text-canvas transition-colors focus:outline-none focus:ring-2 focus:ring-brand-500 focus:ring-offset-2 focus:ring-offset-canvas disabled:opacity-50 disabled:cursor-not-allowed"
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