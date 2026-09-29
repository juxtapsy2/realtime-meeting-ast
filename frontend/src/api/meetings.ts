import { Meeting, TranscriptEvent } from '../types';

const API_BASE = '/api';

export interface PaginatedMeetings {
  meetings: Meeting[];
  total: number;
  page: number;
  limit: number;
}

export async function fetchMeetings(page: number = 1, limit: number = 20): Promise<PaginatedMeetings> {
  const response = await fetch(`${API_BASE}/meetings?page=${page}&limit=${limit}`);
  if (!response.ok) {
    throw new Error('Failed to fetch meetings');
  }
  const data = await response.json();
  return {
    meetings: data.meetings || [],
    total: data.total ?? 0,
    page: data.page ?? page,
    limit: data.limit ?? limit,
  };
}

export async function fetchTranscript(meetingId: number): Promise<TranscriptEvent[]> {
  const response = await fetch(`${API_BASE}/transcript/${meetingId}`);
  if (!response.ok) {
    throw new Error('Failed to fetch transcript');
  }
  return response.json();
}

export async function fetchMeeting(id: number): Promise<Meeting> {
  const response = await fetch(`${API_BASE}/meetings/${id}`);
  if (!response.ok) {
    throw new Error('Failed to fetch meeting');
  }
  const data = await response.json();
  return data.meeting;
}

export async function createMeeting(title: string, projectId?: number): Promise<Meeting> {
  const response = await fetch(`${API_BASE}/meetings`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ title, project_id: projectId }),
  });
  if (!response.ok) {
    throw new Error('Failed to create meeting');
  }
  const data = await response.json();
  return data.meeting;
}

export async function updateMeeting(id: number, title: string): Promise<Meeting> {
  const response = await fetch(`${API_BASE}/meetings/${id}`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ title }),
  });
  if (!response.ok) {
    throw new Error('Failed to update meeting');
  }
  const data = await response.json();
  return data.meeting;
}

export async function deleteMeeting(id: number): Promise<void> {
  const response = await fetch(`${API_BASE}/meetings/${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    throw new Error('Failed to delete meeting');
  }
}
