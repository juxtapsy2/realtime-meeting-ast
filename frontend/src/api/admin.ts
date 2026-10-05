import { sealJSON } from '../lib/sealedbox';

const API_BASE = '/api';

export type AdminSetting = { key: string; secret: boolean; set: boolean; value: string };

export type AuditEntry = {
  admin_email: string;
  role: string;
  action: string;
  setting_key: string;
  created_at: string;
};

export type UserProviderSummary = {
  email: string;
  use_own_keys: boolean;
  stt_provider: string;
  stt_key_set: boolean;
  llm_provider: string;
  llm_model: string;
  llm_key_set: boolean;
  updated_by?: string;
  updated_at?: string;
  meetings: number;
  effective_source: 'platform' | 'own_keys';
};

// Platform defaults come from deployment secrets. The API never returns key
// values, only whether one is configured.
export type PlatformConfig = {
  stt_provider: string;
  stt_key_set: boolean;
  llm_provider: string;
  llm_model: string;
  llm_key_set: boolean;
  google_stt: boolean;
  own_keys_ok: boolean;
};

export type AdminMonitor = {
  settings: AdminSetting[];
  database: string;
  meetings: number;
  auth_gate: string;
  admins: string[];
  superadmin: string;
  viewer: string;
  viewer_role: string;
  platform: PlatformConfig;
  users: UserProviderSummary[];
  audit: AuditEntry[];
};

export type AdminList = {
  admins: string[];
  superadmin: string;
};

// A JSON null renders as null, and `null.length` crashes the admin page. The
// backend and frontend deploy independently, so normalize collections here
// instead of trusting every response shape to match this build.
function asArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

async function readError(response: Response, fallback: string): Promise<Error> {
  const text = await response.text();
  return new Error(text || `${fallback} (status ${response.status})`);
}

export async function fetchAdminMonitor(): Promise<AdminMonitor> {
  const response = await fetch(`${API_BASE}/admin/monitor`);
  if (!response.ok) {
    throw new Error(`Monitor request failed with status ${response.status}`);
  }
  const data = (await response.json()) as AdminMonitor;
  return {
    ...data,
    settings: asArray(data.settings),
    admins: asArray(data.admins),
    users: asArray(data.users),
    audit: asArray(data.audit),
  };
}

// Platform settings update. No API key is accepted here: platform keys live in
// deployment secrets, and user keys are managed per user.
export async function updateAdminSettings(payload: Record<string, string>): Promise<void> {
  const response = await fetch(`${API_BASE}/admin/settings`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    throw await readError(response, 'Settings update failed');
  }
}

export type UserProviderUpdate = {
  email: string;
  use_own_keys?: boolean;
  stt_provider?: string;
  stt_api_key?: string;
  clear_stt_key?: boolean;
  llm_provider?: string;
  llm_model?: string;
  llm_api_key?: string;
  clear_llm_key?: boolean;
};

// API keys are write-only: send one to set or replace it, omit to keep the
// stored key, or set clear_*_key to remove it. The body can carry secrets, so
// it is sealed with the backend's published key rather than sent as plain
// JSON; this is the only admin endpoint that does so, since platform settings
// contain no key values.
export async function saveUserProvider(update: UserProviderUpdate): Promise<void> {
  const response = await fetch(`${API_BASE}/admin/users`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: await sealJSON(update),
  });
  if (!response.ok) {
    throw await readError(response, 'Failed to save user configuration');
  }
}

export async function resetUserProvider(email: string): Promise<void> {
  const response = await fetch(
    `${API_BASE}/admin/users?email=${encodeURIComponent(email)}`,
    { method: 'DELETE' },
  );
  if (!response.ok) {
    throw await readError(response, 'Failed to reset user configuration');
  }
}

export async function fetchAdmins(): Promise<AdminList> {
  const response = await fetch(`${API_BASE}/admin/admins`);
  if (!response.ok) {
    throw new Error(`Admin list request failed with status ${response.status}`);
  }
  const data = (await response.json()) as AdminList;
  return { ...data, admins: asArray(data.admins) };
}

export async function addAdmin(email: string): Promise<void> {
  const response = await fetch(`${API_BASE}/admin/admins`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email }),
  });
  if (!response.ok) {
    throw await readError(response, 'Failed to add admin');
  }
}

export async function removeAdmin(email: string): Promise<void> {
  const response = await fetch(
    `${API_BASE}/admin/admins?email=${encodeURIComponent(email)}`,
    { method: 'DELETE' },
  );
  if (!response.ok) {
    throw await readError(response, 'Failed to remove admin');
  }
}
