import React, { useState, useEffect, useCallback, useRef } from 'react';
import { Meeting } from '../../types';
import { fetchMeetings, createMeeting, deleteMeeting } from '../../api/meetings';

const PAGE_SIZE = 20;

interface MeetingListProps {
  onSelectMeeting: (meeting: Meeting) => void;
}

export function MeetingList({ onSelectMeeting }: MeetingListProps) {
  const [meetings, setMeetings] = useState<Meeting[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [newMeetingTitle, setNewMeetingTitle] = useState('');
  const [isCreating, setIsCreating] = useState(false);
  const [nextPage, setNextPage] = useState(2);

  const scrollRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);

  const hasMore = meetings.length < total;

  useEffect(() => {
    loadMeetings();
  }, []);

  const loadMeetings = async () => {
    try {
      setIsLoading(true);
      const data = await fetchMeetings(1, PAGE_SIZE);
      setMeetings(data.meetings);
      setTotal(data.total);
      setNextPage(data.page + 1);
      setError(null);
    } catch (err) {
      setError('Failed to load meetings');
      console.error(err);
    } finally {
      setIsLoading(false);
    }
  };

  const loadMore = useCallback(async () => {
    if (isLoadingMore || !hasMore || isLoading) return;
    setIsLoadingMore(true);
    setLoadMoreError(false);
    try {
      const data = await fetchMeetings(nextPage, PAGE_SIZE);
      setMeetings((prev) => {
        const merged = [...prev];
        for (const m of data.meetings) {
          if (!merged.some((x) => x.id === m.id)) merged.push(m);
        }
        return merged;
      });
      setTotal(data.total);
      setNextPage(data.page + 1);
      setError(null);
    } catch (err) {
      setLoadMoreError(true);
      console.error(err);
    } finally {
      setIsLoadingMore(false);
    }
  }, [isLoadingMore, isLoading, hasMore, nextPage]);

  useEffect(() => {
    const sentinel = sentinelRef.current;
    const root = scrollRef.current;
    if (!sentinel || !root) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting) loadMore();
      },
      { root, rootMargin: '120px' }
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [loadMore, hasMore, meetings.length]);

  const handleCreateMeeting = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newMeetingTitle.trim()) return;

    try {
      setIsCreating(true);
      const meeting = await createMeeting(newMeetingTitle.trim());
      setMeetings((prev) => [meeting, ...prev]);
      setTotal((t) => t + 1);
      setNewMeetingTitle('');
      onSelectMeeting(meeting);
    } catch (err) {
      setError('Failed to create meeting');
      console.error(err);
    } finally {
      setIsCreating(false);
    }
  };

  const handleDeleteMeeting = async (id: number, e: React.MouseEvent) => {
    e.stopPropagation();
    if (!confirm('Are you sure you want to delete this meeting?')) return;

    try {
      await deleteMeeting(id);
      setMeetings((prev) => prev.filter((m) => m.id !== id));
      setTotal((t) => Math.max(0, t - 1));
    } catch (err) {
      setError('Failed to delete meeting');
      console.error(err);
    }
  };

  const getStatusBadge = (status: string) => {
    const colors = {
      pending: 'bg-yellow-100 text-yellow-800',
      active: 'bg-green-100 text-green-800',
      paused: 'bg-amber-100 text-amber-800',
      completed: 'bg-blue-100 text-blue-800',
    };
    return (
      <span className={`px-2 py-1 rounded-full text-xs font-medium ${colors[status as keyof typeof colors]}`}>
        {status}
      </span>
    );
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-gray-500">Loading meetings...</div>
      </div>
    );
  }

  return (
    <div className="h-screen flex flex-col max-w-4xl mx-auto p-6">
      <div className="mb-4">
        <h1 className="text-2xl font-bold text-gray-900 mb-4">Meetings</h1>

        <form onSubmit={handleCreateMeeting} className="flex gap-2">
          <input
            type="text"
            value={newMeetingTitle}
            onChange={(e) => setNewMeetingTitle(e.target.value)}
            placeholder="Enter meeting title..."
            className="flex-1 px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
            disabled={isCreating}
          />
          <button
            type="submit"
            disabled={isCreating || !newMeetingTitle.trim()}
            className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {isCreating ? 'Creating...' : 'New Meeting'}
          </button>
        </form>
      </div>

      {error && !loadMoreError && (
        <div className="mb-4 p-4 bg-red-50 text-red-700 rounded-lg flex items-center justify-between">
          <span>{error}</span>
          <button onClick={() => setError(null)} className="text-red-500 hover:text-red-700">
            ×
          </button>
        </div>
      )}

      {meetings.length === 0 ? (
        <div className="flex-1 flex items-center justify-center">
          <div className="text-center">
            <p className="text-gray-500">No meetings yet. Create your first meeting above!</p>
          </div>
        </div>
      ) : (
        <>
          <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto">
            <table className="w-full text-left">
              <thead className="sticky top-0 bg-white z-10 border-b border-gray-200">
                <tr className="text-xs uppercase tracking-wide text-gray-500">
                  <th className="py-3 pl-4 pr-2 font-medium">Meeting</th>
                  <th className="py-3 px-2 font-medium">Status</th>
                  <th className="py-3 px-2 font-medium">Created</th>
                  <th className="py-3 pl-2 pr-4 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {meetings.map((meeting) => (
                  <tr
                    key={meeting.id}
                    onClick={() => onSelectMeeting(meeting)}
                    className="h-16 border-b border-gray-100 hover:bg-blue-50/50 cursor-pointer transition-colors"
                  >
                    <td className="px-4">
                      <div className="font-medium text-gray-900 truncate">
                        <span className="text-xs text-gray-400 font-normal mr-1">#{meeting.id}</span>
                        {meeting.title}
                      </div>
                      {meeting.summary?.title && (
                        <div className="text-sm text-gray-500 truncate">{meeting.summary.title}</div>
                      )}
                    </td>
                    <td className="px-2">{getStatusBadge(meeting.status)}</td>
                    <td className="px-2 text-sm text-gray-500">
                      {new Date(meeting.created_at).toLocaleString()}
                    </td>
                    <td className="px-4 text-right">
                      <button
                        onClick={(e) => handleDeleteMeeting(meeting.id, e)}
                        className="text-gray-400 hover:text-red-500 transition-colors"
                        title="Delete meeting"
                      >
                        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                        </svg>
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>

            <div ref={sentinelRef} className="flex items-center justify-center h-12 text-sm">
              {loadMoreError ? (
                <div className="flex items-center gap-3 text-red-600">
                  <span>Failed to load more meetings.</span>
                  <button
                    onClick={() => loadMore()}
                    className="px-3 py-1 border border-red-300 rounded-md hover:bg-red-50"
                  >
                    Retry
                  </button>
                </div>
              ) : isLoadingMore ? (
                <span className="text-gray-500">Loading more meetings...</span>
              ) : !hasMore ? (
                <span className="text-gray-400">Showing all {total} meetings</span>
              ) : (
                <span className="text-gray-400">Scroll for more</span>
              )}
            </div>
          </div>

          <div className="flex items-center justify-between pt-3 border-t border-gray-200">
            <span className="text-sm text-gray-500">
              Showing {meetings.length} of {total} meetings
            </span>
          </div>
        </>
      )}
    </div>
  );
}