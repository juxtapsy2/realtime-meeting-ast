const API_BASE = '/api';

export async function fetchCurrentUser(): Promise<string | null> {
  const response = await fetch(`${API_BASE}/auth/me`);
  if (!response.ok) {
    return null;
  }
  const data = await response.json();
  return data.authenticated ? (data.email || null) : null;
}

export async function logout(): Promise<void> {
  await fetch(`${API_BASE}/auth/logout`, { method: 'POST' });
}