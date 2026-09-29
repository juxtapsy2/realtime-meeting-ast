import React, { useState, useEffect, useCallback } from 'react';
import { Meeting } from '../../types';
import { fetchMeetings, createMeeting, deleteMeeting, PaginatedMeetings } from '../../api/meetings';
import { DataTable, DataTableColumn } from '../../components/DataTable';

const PAGE_SIZE = 20;

interface MeetingListProps {
  onSelectMeeting: (meeting: Meeting) => void;
}

export function MeetingList({ onSelectMeeting }: MeetingListProps) {
  const [meetings, setMeetings] = useState<Meeting[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [isSyncing, setIsSyncing] = useState(false);
  const [syncError, setSyncError] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [newMeetingTitle, setNewMeetingTitle] = useState('');
  const [isCreating, setIsCreating] = useState(false);
  const [visibleCount, setVisibleCount] = useState(PAGE_SIZE);

  const loadMeetings = useCallback(async () => {
    setIsLoading(true);
    setSyncError(false);
    let first: PaginatedMeetings | null = null;
    try {
      // First page renders immediately so the UI paints fast; the remaining
      // pages sync in the background below.
      first = await fetchMeetings(1, PAGE_SIZE);
      setMeetings(first.meetings);
      setTotal(first.total);
      setError(null);
    } catch (err) {
      setError('Failed to load meetings');
      console.error(err);
      return;
    } finally {
      setIsLoading(false);
    }

    const remainingPages = Math.ceil((first.total - first.meetings.length) / PAGE_SIZE);
    if (remainingPages <= 0) return;

    setIsSyncing(true);
    try {
      for (let page = 2; page <= remainingPages + 1; page++) {
        const data = await fetchMeetings(page, PAGE_SIZE);
        if (data.meetings.length === 0) break;
        setMeetings((prev) => {
          const seen = new Set(prev.map((m) => m.id));
          const fresh = data.meetings.filter((m) => !seen.has(m.id));
          return fresh.length ? [...prev, ...fresh] : prev;
        });
      }
    } catch (err) {
      setSyncError(true);
      console.error(err);
    } finally {
      setIsSyncing(false);
    }
  }, []);

  useEffect(() => {
    void loadMeetings();
  }, [loadMeetings]);

  const loadMore = useCallback(() => {
    setVisibleCount((c) => Math.min(c + PAGE_SIZE, meetings.length));
  }, [meetings.length]);

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

  const columns: DataTableColumn<Meeting>[] = [
    { field: 'id', label: 'ID', sortable: true, sortValue: (m) => m.id, width: '5rem' },
    { field: 'title', label: 'Title', sortable: true, sortValue: (m) => m.title.toLowerCase() },
    { field: 'status', label: 'Status', sortable: true },
    { field: 'created_at', label: 'Created', sortable: true, sortValue: (m) => new Date(m.created_at).getTime(), align: 'right' },
    { field: 'actions', label: 'Actions', align: 'right' },
  ];

  const valueMappings = {
    id: (value: unknown) => <span className="text-xs text-gray-400">#{String(value)}</span>,
    status: (value: unknown) => getStatusBadge(String(value)),
    created_at: (value: unknown) => new Date(String(value)).toLocaleString(),
  };

  const customFields = {
    title: (row: Meeting) => (
      <div className="font-medium text-gray-900 truncate">
        {row.title}
        {row.summary?.title && <div className="text-sm text-gray-500 truncate">{row.summary.title}</div>}
      </div>
    ),
    actions: (row: Meeting) => (
      <button
        onClick={(e) => handleDeleteMeeting(row.id, e)}
        className="text-gray-400 hover:text-red-500 transition-colors"
        title="Delete meeting"
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
        </svg>
      </button>
    ),
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

      {error && (
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
          <DataTable
            columns={columns}
            data={meetings}
            rowKey={(m) => m.id}
            enableSort
            initialSort={{ field: 'created_at', direction: 'desc' }}
            valueMappings={valueMappings}
            customFields={customFields}
            onRowClick={onSelectMeeting}
            getRowClassName={() => 'h-16'}
            emptyMessage="No meetings yet."
            visibleLimit={visibleCount}
            onRevealMore={loadMore}
          />

          <div className="pt-3 border-t border-gray-200 flex items-center justify-between">
            <span className="text-sm text-gray-500">
              Showing {Math.min(visibleCount, meetings.length)} of {total} meetings
            </span>
            {isSyncing ? (
              <span className="text-sm text-gray-400">Syncing remaining meetings...</span>
            ) : syncError ? (
              <span className="text-sm text-red-500">
                Failed to load some meetings.{' '}
                <button onClick={() => void loadMeetings()} className="underline hover:text-red-700">
                  Retry
                </button>
              </span>
            ) : null}
          </div>
        </>
      )}
    </div>
  );
}