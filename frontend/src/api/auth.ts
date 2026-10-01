const API_BASE = '/api';

export type Role = 'user' | 'admin' | 'superadmin';

export type CurrentUser = { email: string; role: Role; isAdmin: boolean; isSuperAdmin: boolean };

export async function fetchCurrentUser(): Promise<CurrentUser | null> {
  const response = await fetch(`${API_BASE}/auth/me`);
  if (!response.ok) {
    return null;
  }
  const data = await response.json();
  if (!data.authenticated) {
    return null;
  }
  return {
    email: data.email || '',
    role: (data.role || 'user') as Role,
    isAdmin: Boolean(data.is_admin),
    isSuperAdmin: Boolean(data.is_superadmin),
  };
}

export async function logout(): Promise<void> {
  await fetch(`${API_BASE}/auth/logout`, { method: 'POST' });
}
