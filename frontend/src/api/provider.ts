import { sealJSON } from '../lib/sealedbox';
import type { PlatformConfig, UserProviderSummary, UserProviderUpdate } from './admin';

const API_BASE = '/api';

// The self-service view of one signed-in user's provider configuration. The
// server takes the identity from the session and ignores any email in the
// update payload, so this type never names another user.
export type OwnProvider = {
  user: UserProviderSummary;
  platform: PlatformConfig;
};

async function readError(response: Response, fallback: string): Promise<Error> {
  const text = await response.text();
  return new Error(text || `${fallback} (status ${response.status})`);
}

export async function fetchOwnProvider(): Promise<OwnProvider> {
  const response = await fetch(`${API_BASE}/user/provider`);
  if (!response.ok) {
    throw new Error(`Provider request failed with status ${response.status}`);
  }
  return (await response.json()) as OwnProvider;
}

// Keys are write-only: send one to set or replace it, omit to keep the stored
// key, or set clear_*_key to remove it. The body can carry secrets, so it is
// sealed with the backend's published key the same way the admin update is.
export async function saveOwnProvider(update: UserProviderUpdate): Promise<void> {
  const response = await fetch(`${API_BASE}/user/provider`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: await sealJSON(update),
  });
  if (!response.ok) {
    throw await readError(response, 'Failed to save your provider settings');
  }
}

export async function resetOwnProvider(): Promise<void> {
  const response = await fetch(`${API_BASE}/user/provider`, { method: 'DELETE' });
  if (!response.ok) {
    throw await readError(response, 'Failed to reset your provider settings');
  }
}
