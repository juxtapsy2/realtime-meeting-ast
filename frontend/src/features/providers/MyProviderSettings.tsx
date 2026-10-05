import { useCallback, useEffect, useState } from 'react';
import { UserProviderUpdate } from '../../api/admin';
import { fetchOwnProvider, OwnProvider, resetOwnProvider, saveOwnProvider } from '../../api/provider';
import { UserProviderRow } from './UserProviderRow';

interface MyProviderSettingsProps {
  onBack: () => void;
}

// Self-service provider settings. Every signed-in user reaches this panel and
// it only ever shows and edits their own configuration: the server resolves
// the target from the session, so there is no other email to select here.
//
// The layout mirrors the admin page on purpose, so editing your own row is the
// same interaction whether you are in the table or in this panel.
export function MyProviderSettings({ onBack }: MyProviderSettingsProps) {
  const [own, setOwn] = useState<OwnProvider | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setOwn(await fetchOwnProvider());
      setError(null);
    } catch (err) {
      setError('Failed to load your provider settings');
      console.error(err);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function run(action: () => Promise<void>, success: string) {
    setBusy(true);
    setNotice(null);
    setError(null);
    try {
      await action();
      setNotice(success);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed');
      console.error(err);
    } finally {
      setBusy(false);
    }
  }

  async function handleSave(_email: string, patch: UserProviderUpdate) {
    await run(() => saveOwnProvider(patch), 'Provider settings saved.');
  }

  async function handleReset() {
    if (
      !confirm('Reset your provider settings to the platform defaults? Stored keys will be removed.')
    ) {
      return;
    }
    await run(() => resetOwnProvider(), 'Reset to platform provider defaults.');
  }

  if (!own) {
    return (
      <div className="h-screen flex items-center justify-center">
        <div className="text-gray-500">{error ?? 'Loading your settings...'}</div>
      </div>
    );
  }

  const platform = own.platform;
  const canEdit = platform.own_keys_ok;

  return (
    <div className="h-screen flex flex-col max-w-5xl mx-auto p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Your provider settings</h1>
          <p className="text-xs text-gray-400 mt-0.5">
            {own.user.email} · using{' '}
            {own.user.effective_source === 'own_keys' ? 'your own keys' : 'the platform defaults'}
          </p>
        </div>
        <button
          onClick={onBack}
          className="px-3 py-1.5 text-sm rounded-lg border border-gray-300 text-gray-600 hover:bg-gray-100"
        >
          Back to meetings
        </button>
      </div>

      {error && (
        <div className="mb-4 p-4 bg-red-50 text-red-700 rounded-lg whitespace-pre-wrap">{error}</div>
      )}
      {notice && (
        <div className="mb-4 p-4 bg-green-50 text-green-700 rounded-lg">{notice}</div>
      )}
      {!canEdit && (
        <div className="mb-4 p-4 bg-amber-50 text-amber-800 rounded-lg">
          Key storage is unavailable (AUTH_HMAC_SECRET is missing on this deployment), so API keys
          cannot be saved yet. Nothing here can break: changes are refused rather than stored
          unencrypted.
        </div>
      )}

      <div className="rounded-xl border border-gray-200 bg-white mb-6">
        <div className="px-4 py-3 border-b border-gray-100">
          <h2 className="font-medium text-gray-900">Platform defaults</h2>
          <p className="text-xs text-gray-400 mt-0.5">
            What you use unless you switch to your own providers below. Keys are never returned,
            only whether they exist.
          </p>
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 px-4 py-3 text-sm">
          <div>
            <div className="text-xs text-gray-400">STT</div>
            <div className="text-gray-900">{platform.stt_provider}</div>
            <div className="text-xs text-gray-400">
              {platform.google_stt
                ? 'service account'
                : platform.stt_key_set
                  ? 'key configured'
                  : 'no key'}
            </div>
          </div>
          <div>
            <div className="text-xs text-gray-400">LLM</div>
            <div className="text-gray-900">{platform.llm_provider}</div>
            <div className="text-xs text-gray-400">{platform.llm_model || 'default model'}</div>
          </div>
          <div>
            <div className="text-xs text-gray-400">LLM key</div>
            <div className="text-gray-900">
              {platform.llm_key_set ? 'configured' : 'not configured'}
            </div>
          </div>
          <div>
            <div className="text-xs text-gray-400">Your meetings</div>
            <div className="text-gray-900">
              {own.user.meetings} meeting{own.user.meetings === 1 ? '' : 's'}
            </div>
          </div>
        </div>
      </div>

      <div className="rounded-xl border border-gray-200 bg-white">
        <div className="px-4 py-3 border-b border-gray-100">
          <h2 className="font-medium text-gray-900">Your configuration</h2>
          <p className="text-xs text-gray-400 mt-0.5">
            Keys are encrypted before storage and cannot be read back. Leave a key blank to keep
            the one already stored.
          </p>
        </div>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs text-gray-400 border-b border-gray-100">
              <th className="px-4 py-2">Account</th>
              <th className="px-4 py-2">Mode</th>
              <th className="px-4 py-2">STT</th>
              <th className="px-4 py-2">LLM</th>
              <th className="px-4 py-2" />
            </tr>
          </thead>
          <tbody>
            <UserProviderRow
              user={own.user}
              canEdit={canEdit}
              busy={busy}
              onSave={handleSave}
              onReset={handleReset}
            />
          </tbody>
        </table>
      </div>
    </div>
  );
}
