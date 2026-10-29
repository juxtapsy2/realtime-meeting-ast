import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { UserProviderUpdate } from '../../api/admin';
import {
  applyOwnCombo,
  ComboList,
  deleteOwnCombo,
  fetchOwnCombos,
  fetchOwnLLMModels,
  fetchOwnProvider,
  OwnProvider,
  resetOwnProvider,
  saveOwnCombo,
  saveOwnProvider,
} from '../../api/provider';
import { UserProviderRow } from './UserProviderRow';

interface MyProviderSettingsProps {
  onBack: () => void;
}

// Default combo name derived from a complete saved configuration, e.g.
// "Google STT - gemini-3.8-flash Combo".
function defaultComboName(sttProvider: string, llmModel: string): string {
  const stt = sttProvider
    ? sttProvider.charAt(0).toUpperCase() + sttProvider.slice(1)
    : 'Platform';
  return `${stt} STT - ${llmModel} Combo`;
}

type Loaded = { own: OwnProvider; combos: ComboList };

// Self-service provider settings. Every signed-in user reaches this panel and
// it only ever shows and edits their own configuration: the server resolves
// the target from the session, so there is no other email to select here.
//
// The layout mirrors the admin page on purpose, so editing your own row is the
// same interaction whether you are in the table or in this panel.
export function MyProviderSettings({ onBack }: MyProviderSettingsProps) {
  const [own, setOwn] = useState<OwnProvider | null>(null);
  const [combos, setCombos] = useState<ComboList | null>(null);
  const [combosOpen, setCombosOpen] = useState(true);
  const [comboName, setComboName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async (): Promise<Loaded | null> => {
    try {
      const [ownData, comboData] = await Promise.all([fetchOwnProvider(), fetchOwnCombos()]);
      setOwn(ownData);
      setCombos(comboData);
      setError(null);
      return { own: ownData, combos: comboData };
    } catch (err) {
      setError('Failed to load your provider settings');
      console.error(err);
      return null;
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // Runs one action, then reloads so rows, platform state, and combos stay in
  // sync. Returns the freshly loaded state on success, null on failure.
  async function run(action: () => Promise<void>, success: string): Promise<Loaded | null> {
    setBusy(true);
    setNotice(null);
    setError(null);
    try {
      await action();
      setNotice(success);
      const data = await load();
      return data;
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed');
      console.error(err);
      return null;
    } finally {
      setBusy(false);
    }
  }

  async function handleSave(_email: string, patch: UserProviderUpdate) {
    const data = await run(() => saveOwnProvider(patch), 'Provider settings saved.');
    if (!data) return;
    // Auto-snapshot a combo once the configuration is complete (at least one
    // key and a model are stored), so the user never has to name and save the
    // same setup a second time. Never overwrites an existing combo with the
    // same name — delete it and save again to refresh — and never fails the
    // row save if the snapshot itself fails.
    const user = data.own.user;
    const hasKey = user.stt_key_set || user.llm_key_set;
    if (!user.llm_model || !hasKey) return;
    const name = defaultComboName(user.stt_provider, user.llm_model);
    if (data.combos.combos.some((c) => c.name === name)) return;
    setBusy(true);
    try {
      await saveOwnCombo(name);
      await load();
      setNotice(`Saved "${name}" as a combo.`);
    } catch (err) {
      // Best-effort: the row save already succeeded.
      console.error(err);
    } finally {
      setBusy(false);
    }
  }

  async function handleReset() {
    if (
      !confirm('Reset your provider settings to the platform defaults? Stored keys will be removed.')
    ) {
      return;
    }
    await run(() => resetOwnProvider(), 'Reset to platform provider defaults.');
  }

  // The server takes the identity from the session; the row's email is
  // ignored on this endpoint.
  async function handleFetchModels(_email: string, provider: string, typedKey: string) {
    return fetchOwnLLMModels(provider, typedKey || undefined);
  }

  async function handleSwitchSystem() {
    if (!own) return;
    // Non-destructive: keeps the stored keys, just stops using them.
    await run(
      () => saveOwnProvider({ email: own.user.email, use_own_keys: false }),
      'Switched to the system default.',
    );
  }

  async function handleSwitchCombo(name: string) {
    await run(() => applyOwnCombo(name), `Switched to "${name}".`);
  }

  async function handleDeleteCombo(name: string) {
    if (!confirm(`Delete the saved configuration "${name}"?`)) return;
    await run(() => deleteOwnCombo(name), `Deleted "${name}".`);
  }

  async function handleSaveCombo() {
    const name = comboName.trim();
    if (!name) return;
    if (await run(() => saveOwnCombo(name), `Saved "${name}" as a combo.`)) {
      setComboName('');
    }
  }

  if (!own || !combos) {
    return (
      <div className="flex-1 min-h-0 flex items-center justify-center">
        <div className="text-muted">{error ?? 'Loading your settings...'}</div>
      </div>
    );
  }

  const platform = own.platform;
  const canEdit = platform.own_keys_ok;
  const systemActive = combos.active_kind === 'system';
  const activeCombo = combos.active_kind === 'combo' ? (combos.active_name ?? '') : '';

  // One row of the expansion list: click to switch to it (the active row is
  // marked and inert); Delete sits beside combos only.
  function comboRow(
    key: string,
    title: string,
    summary: ReactNode,
    isActive: boolean,
    onSwitch: () => void,
    onDelete?: () => void,
  ) {
    return (
      <li key={key} className="flex items-center gap-2">
        <button
          type="button"
          onClick={onSwitch}
          disabled={!canEdit || busy || isActive}
          className={`flex-1 flex items-center justify-between gap-3 px-3 py-2 rounded-lg text-left transition-colors disabled:cursor-default ${
            isActive ? 'bg-brand-100' : canEdit && !busy ? 'hover:bg-wash/60' : ''
          }`}
        >
          <span>
            <span className="text-sm font-medium text-ink">{title}</span>
            <span className="block text-xs text-muted">{summary}</span>
          </span>
          {isActive ? (
            <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-brand-300 !text-ink shrink-0">
              Active
            </span>
          ) : canEdit && !busy ? (
            <span className="text-xs text-brand-700 shrink-0">Switch</span>
          ) : null}
        </button>
        {onDelete && (
          <button
            type="button"
            onClick={onDelete}
            disabled={!canEdit || busy}
            className="px-2 py-1 text-xs text-muted hover:text-coral-600 transition-colors disabled:opacity-50 shrink-0"
          >
            Delete
          </button>
        )}
      </li>
    );
  }

  return (
    <div className="flex-1 min-h-0 overflow-y-auto flex flex-col max-w-7xl mx-auto p-6">
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
        {/* The header doubles as the toggle for the saved-configurations
            expansion list shown below the table. */}
        <button
          type="button"
          onClick={() => setCombosOpen((open) => !open)}
          aria-expanded={combosOpen}
          className="w-full text-left px-4 py-3 border-b border-wash hover:bg-wash/40 transition-colors"
        >
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="font-medium text-ink">Your configuration</div>
              <p className="text-xs text-muted mt-0.5">
                Keys are encrypted before storage and cannot be read back. Leave a key blank to keep
                the one already stored.
              </p>
              <p className="text-xs text-muted mt-1">
                Only these providers are supported today — LLM: groq, openai, gemini · STT: deepgram,
                openai, groq, google. Any other value falls back to the platform default.
              </p>
            </div>
            <span className="flex items-center gap-1.5 text-xs text-brand-700 whitespace-nowrap shrink-0">
              Saved configurations ({combos.combos.length})
              <svg
                className={`w-4 h-4 transition-transform ${combosOpen ? 'rotate-180' : ''}`}
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
                aria-hidden
              >
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
              </svg>
            </span>
          </div>
        </button>
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
              onFetchModels={handleFetchModels}
            />
          </tbody>
        </table>

        {combosOpen && (
          <div className="border-t border-wash">
            <div className="px-4 pt-3 text-xs text-muted">
              Click a configuration to switch to it — providers, model, and stored keys change
              together, nothing is retyped. Saving a complete configuration (an API key and a model)
              stores it as a combo automatically; the field below saves one under a custom name.
            </div>
            <ul className="px-4 py-2 space-y-1">
              {comboRow(
                '__system__',
                'System default (platform)',
                <>
                  {platform.stt_provider} → {platform.llm_provider}
                  {platform.llm_model ? ` / ${platform.llm_model}` : ''}
                </>,
                systemActive,
                () => void handleSwitchSystem(),
              )}
              {combos.combos.map((c) =>
                comboRow(
                  c.name,
                  c.name,
                  <>
                    {c.stt_provider || 'platform STT'} → {c.llm_provider || 'platform LLM'}
                    {c.llm_model ? ` / ${c.llm_model}` : ''}
                    {c.stt_key_set || c.llm_key_set ? ' · keys stored' : ''}
                  </>,
                  activeCombo === c.name,
                  () => void handleSwitchCombo(c.name),
                  () => void handleDeleteCombo(c.name),
                ),
              )}
              {combos.combos.length === 0 && (
                <li className="px-3 py-2 text-sm text-muted">
                  No saved configurations yet — add an API key and a model, hit Save in the table
                  above, and it will appear here automatically.
                </li>
              )}
            </ul>
            {combos.active_kind === 'custom' && (
              <div className="px-4 pb-2 text-xs text-muted">
                Active: custom configuration — your own keys, not matching any saved combo.
              </div>
            )}
            <div className="px-4 pb-3 flex items-end gap-2">
              <div>
                <div className="text-xs text-muted mb-1">Save current as</div>
                <input
                  type="text"
                  value={comboName}
                  onChange={(e) => setComboName(e.target.value)}
                  maxLength={100}
                  placeholder="combo name"
                  disabled={!canEdit}
                  className="w-56 px-3 py-2 text-sm border border-steel/50 rounded-lg bg-canvas text-ink placeholder:text-steel focus:outline-none focus:ring-2 focus:ring-brand-500 disabled:bg-wash disabled:text-muted"
                />
              </div>
              <button
                onClick={handleSaveCombo}
                disabled={!canEdit || busy || !comboName.trim()}
                className="px-4 py-2 border border-steel/50 text-ink text-sm rounded-lg hover:bg-wash transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Save as combo
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
