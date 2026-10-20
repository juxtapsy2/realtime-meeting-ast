import { useEffect, useState } from 'react';
import { UserProviderSummary, UserProviderUpdate } from '../../api/admin';

// Per-user provider configuration. API keys are write-only: a stored key is
// only ever reported as "set", never displayed, and the field is left blank
// unless the user types a new key.
//
// The same row renders in the admin table (editing anyone, superadmin only)
// and in the self-service panel (editing yourself), so the caller decides
// whether it is editable rather than the component knowing who is viewing it.
export function UserProviderRow({
  user,
  canEdit,
  busy,
  onSave,
  onReset,
}: {
  user: UserProviderSummary;
  canEdit: boolean;
  busy: boolean;
  onSave: (email: string, patch: UserProviderUpdate) => void;
  onReset: (email: string) => void;
}) {
  const [useOwn, setUseOwn] = useState(user.use_own_keys);
  const [sttProvider, setSttProvider] = useState(user.stt_provider);
  const [sttKey, setSttKey] = useState('');
  const [llmProvider, setLlmProvider] = useState(user.llm_provider);
  const [llmModel, setLlmModel] = useState(user.llm_model);
  const [llmKey, setLlmKey] = useState('');

  useEffect(() => {
    setUseOwn(user.use_own_keys);
    setSttProvider(user.stt_provider);
    setLlmProvider(user.llm_provider);
    setLlmModel(user.llm_model);
    setSttKey('');
    setLlmKey('');
  }, [user]);

  function handleSave() {
    const patch: UserProviderUpdate = { email: user.email, use_own_keys: useOwn };
    if (sttProvider.trim()) patch.stt_provider = sttProvider.trim();
    if (llmProvider.trim()) patch.llm_provider = llmProvider.trim();
    if (llmModel.trim()) patch.llm_model = llmModel.trim();
    // Only send a key when one was typed: omitting it keeps the stored key.
    if (sttKey.trim()) patch.stt_api_key = sttKey.trim();
    if (llmKey.trim()) patch.llm_api_key = llmKey.trim();
    onSave(user.email, patch);
  }

  return (
    <tr className="border-b border-wash last:border-0 align-top">
      <td className="px-4 py-3">
        <div className="text-sm font-medium text-ink">{user.email}</div>
        <div className="text-xs text-muted">
          {user.meetings} meeting{user.meetings === 1 ? '' : 's'} ·{' '}
          {user.effective_source === 'own_keys' ? 'own keys' : 'platform defaults'}
        </div>
        {user.updated_at && (
          <div className="text-xs text-muted">
            updated {new Date(user.updated_at).toLocaleString()}
            {user.updated_by ? ` by ${user.updated_by}` : ''}
          </div>
        )}
      </td>
      <td className="px-4 py-3">
        <label className="flex items-center gap-2 text-sm text-ink">
          <input
            type="checkbox"
            checked={useOwn}
            disabled={!canEdit}
            onChange={(e) => setUseOwn(e.target.checked)}
          />
          Use own API keys
        </label>
      </td>
      <td className="px-4 py-3">
        <div className="space-y-1">
          <input
            type="text"
            value={sttProvider}
            disabled={!canEdit}
            onChange={(e) => setSttProvider(e.target.value)}
            placeholder="stt provider"
            className="w-full px-2 py-1 text-xs border border-steel/50 rounded-lg bg-canvas text-ink placeholder:text-steel focus:outline-none focus:ring-2 focus:ring-brand-500 disabled:bg-wash disabled:text-muted"
          />
          <input
            type="password"
            value={sttKey}
            disabled={!canEdit}
            onChange={(e) => setSttKey(e.target.value)}
            placeholder={user.stt_key_set ? '•••••••• (stored)' : 'no STT key'}
            className="w-full px-2 py-1 text-xs border border-steel/50 rounded-lg bg-canvas text-ink placeholder:text-steel focus:outline-none focus:ring-2 focus:ring-brand-500 disabled:bg-wash disabled:text-muted"
          />
        </div>
      </td>
      <td className="px-4 py-3">
        <div className="space-y-1">
          <input
            type="text"
            value={llmProvider}
            disabled={!canEdit}
            onChange={(e) => setLlmProvider(e.target.value)}
            placeholder="llm provider"
            className="w-full px-2 py-1 text-xs border border-steel/50 rounded-lg bg-canvas text-ink placeholder:text-steel focus:outline-none focus:ring-2 focus:ring-brand-500 disabled:bg-wash disabled:text-muted"
          />
          <input
            type="text"
            value={llmModel}
            disabled={!canEdit}
            onChange={(e) => setLlmModel(e.target.value)}
            placeholder="llm model"
            className="w-full px-2 py-1 text-xs border border-steel/50 rounded-lg bg-canvas text-ink placeholder:text-steel focus:outline-none focus:ring-2 focus:ring-brand-500 disabled:bg-wash disabled:text-muted"
          />
          <input
            type="password"
            value={llmKey}
            disabled={!canEdit}
            onChange={(e) => setLlmKey(e.target.value)}
            placeholder={user.llm_key_set ? '•••••••• (stored)' : 'no LLM key'}
            className="w-full px-2 py-1 text-xs border border-steel/50 rounded-lg bg-canvas text-ink placeholder:text-steel focus:outline-none focus:ring-2 focus:ring-brand-500 disabled:bg-wash disabled:text-muted"
          />
        </div>
      </td>
      <td className="px-4 py-3">
        {canEdit && (
          <div className="flex flex-col gap-1">
            <button
              onClick={handleSave}
              disabled={busy}
              className="text-xs font-semibold text-brand-700 hover:underline disabled:opacity-50"
            >
              save
            </button>
            {user.stt_key_set && (
              <button
                onClick={() =>
                  onSave(user.email, {
                    email: user.email,
                    use_own_keys: useOwn,
                    clear_stt_key: true,
                  })
                }
                disabled={busy}
                className="text-xs text-muted hover:text-coral-600 disabled:opacity-50"
              >
                clear STT key
              </button>
            )}
            {user.llm_key_set && (
              <button
                onClick={() =>
                  onSave(user.email, {
                    email: user.email,
                    use_own_keys: useOwn,
                    clear_llm_key: true,
                  })
                }
                disabled={busy}
                className="text-xs text-muted hover:text-coral-600 disabled:opacity-50"
              >
                clear LLM key
              </button>
            )}
            {(user.use_own_keys || user.stt_key_set || user.llm_key_set) && (
              <button
                onClick={() => onReset(user.email)}
                disabled={busy}
                className="text-xs text-muted hover:text-coral-600 disabled:opacity-50"
              >
                reset
              </button>
            )}
          </div>
        )}
      </td>
    </tr>
  );
}
