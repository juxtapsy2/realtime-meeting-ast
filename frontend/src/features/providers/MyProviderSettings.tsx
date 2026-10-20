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
        <div className="text-muted">{error ?? 'Loading your settings...'}</div>
      </div>
    );
  }

  const platform = own.platform;
  const canEdit = platform.own_keys_ok;

  return (
    <div className="h-screen flex flex-col max-w-5xl mx-auto p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-ink">Your provider settings</h1>
          <p className="text-xs text-muted mt-0.5">
            {own.user.email} · using{' '}
            {own.user.effective_source === 'own_keys' ? 'your own keys' : 'the platform defaults'}
          </p>
        </div>
        <button
          onClick={onBack}
          className="px-3 py-1.5 text-sm rounded-lg border border-steel/50 text-ink hover:bg-canvas transition-colors"
        >
          Back to meetings
        </button>
      </div>

      {error && (
        <div className="mb-4 p-4 bg-coral-100 text-coral-700 rounded-lg whitespace-pre-wrap">{error}</div>
      )}
      {notice && (
        <div className="mb-4 p-4 bg-brand-100 text-brand-700 rounded-lg">{notice}</div>
      )}
      {!canEdit && (
        <div className="mb-4 p-4 bg-steel/25 text-ink rounded-lg">
          Key storage is unavailable (AUTH_HMAC_SECRET is missing on this deployment), so API keys
          cannot be saved yet. Nothing here can break: changes are refused rather than stored
          unencrypted.
        </div>
      )}

      <div className="rounded-xl border border-steel/30 bg-canvas mb-6">
        <div className="px-4 py-3 border-b border-wash">
          <h2 className="font-medium text-ink">Platform defaults</h2>
          <p className="text-xs text-muted mt-0.5">
            What you use unless you switch to your own providers below. Keys are never returned,
            only whether they exist.
          </p>
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 px-4 py-3 text-sm">
          <div>
            <div className="text-xs text-muted">STT</div>
            <div className="text-ink">{platform.stt_provider}</div>
            <div className="text-xs text-muted">
              {platform.google_stt
                ? 'service account'
                : platform.stt_key_set
                  ? 'key configured'
                  : 'no key'}
            </div>
          </div>
          <div>
            <div className="text-xs text-muted">LLM</div>
            <div className="text-ink">{platform.llm_provider}</div>
            <div className="text-xs text-muted">{platform.llm_model || 'default model'}</div>
          </div>
          <div>
            <div className="text-xs text-muted">LLM key</div>
            <div className="text-ink">
              {platform.llm_key_set ? 'configured' : 'not configured'}
            </div>
          </div>
          <div>
            <div className="text-xs text-muted">Your meetings</div>
            <div className="text-ink">
              {own.user.meetings} meeting{own.user.meetings === 1 ? '' : 's'}
            </div>
          </div>
        </div>
      </div>

      <div className="rounded-xl border border-steel/30 bg-canvas">
        <div className="px-4 py-3 border-b border-wash">
          <h2 className="font-medium text-ink">Your configuration</h2>
          <p className="text-xs text-muted mt-0.5">
            Keys are encrypted before storage and cannot be read back. Leave a key blank to keep
            the one already stored.
          </p>
          <p className="text-xs text-muted mt-1">
            Only these providers are supported today — LLM: groq, openai, gemini · STT: deepgram,
            openai, groq, google. Any other value falls back to the platform default.
          </p>
        </div>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs text-muted border-b border-wash">
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
