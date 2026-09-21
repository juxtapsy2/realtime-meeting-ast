import { Meeting } from '../types';

const API_BASE = '/api';

export async function fetchMeetings(): Promise<Meeting[]> {
  const response = await fetch(`${API_BASE}/meetings`);
  if (!response.ok) {
    throw new Error('Failed to fetch meetings');
  }
  const data = await response.json();
  return data.meetings || [];
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
