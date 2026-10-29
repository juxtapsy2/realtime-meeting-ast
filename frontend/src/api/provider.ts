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

// Asks the provider (via the backend) which model IDs are available, so the
// settings UI can offer a picker instead of free-typing model names. A typed
// key is sent to preview models before saving it; omit it to use the stored
// key. The body may carry that key, so it is sealed like every provider write.
export async function fetchOwnLLMModels(provider: string, typedKey?: string): Promise<string[]> {
  const body: { llm_provider: string; llm_api_key?: string } = { llm_provider: provider };
  if (typedKey) body.llm_api_key = typedKey;
  const response = await fetch(`${API_BASE}/user/provider/models`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: await sealJSON(body),
  });
  if (!response.ok) {
    throw await readError(response, 'Failed to fetch models');
  }
  const data = (await response.json()) as { models: string[] };
  return data.models;
}

// One saved STT/LLM configuration. Keys are never returned — only whether
// each is stored.
export type ProviderCombo = {
  name: string;
  stt_provider: string;
  llm_provider: string;
  llm_model: string;
  stt_key_set: boolean;
  llm_key_set: boolean;
  updated_at?: string;
};

// active_kind + active_name are a pair rather than one string, so a combo
// literally named "system" or "custom" can never be confused with those
// states.
export type ComboList = {
  active_kind: 'system' | 'custom' | 'combo';
  active_name?: string;
  combos: ProviderCombo[];
};

export async function fetchOwnCombos(): Promise<ComboList> {
  const response = await fetch(`${API_BASE}/user/provider/combos`);
  if (!response.ok) {
    throw await readError(response, 'Failed to load your saved configurations');
  }
  return (await response.json()) as ComboList;
}

// Snapshots the caller's currently saved configuration under a name (the
// server refuses when there is no stored configuration with a key yet).
export async function saveOwnCombo(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/user/provider/combos`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: await sealJSON({ name }),
  });
  if (!response.ok) {
    throw await readError(response, 'Failed to save the configuration');
  }
}

// Switches the stored configuration to a combo: providers, model, and stored
// keys are copied server-side, so nothing is retyped.
export async function applyOwnCombo(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/user/provider/combos/apply`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: await sealJSON({ name }),
  });
  if (!response.ok) {
    throw await readError(response, 'Failed to switch configuration');
  }
}

export async function deleteOwnCombo(name: string): Promise<void> {
  const response = await fetch(
    `${API_BASE}/user/provider/combos?name=${encodeURIComponent(name)}`,
    { method: 'DELETE' },
  );
  if (!response.ok) {
    throw await readError(response, 'Failed to delete the configuration');
  }
}
