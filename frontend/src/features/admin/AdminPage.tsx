import { useCallback, useEffect, useState } from "react";
import {
  addAdmin,
  AdminMonitor,
  AuditEntry,
  fetchAdminMonitor,
  removeAdmin,
  resetUserProvider,
  saveUserProvider,
  updateAdminSettings,
  UserProviderUpdate,
} from "../../api/admin";
import { resetOwnProvider, saveOwnProvider } from "../../api/provider";
import { EmailTagInput } from "./EmailTagInput";
import { UserProviderRow } from "../providers/UserProviderRow";

interface AdminPageProps {
  onBack: () => void;
  isSuperAdmin: boolean;
}

function StatusBadge({ ok }: { ok: boolean }) {
  return (
    <span
      className={`px-2 py-0.5 rounded-full text-xs font-medium ${
        ok ? "bg-green-100 text-green-700" : "bg-red-100 text-red-700"
      }`}
    >
      {ok ? "ok" : "error"}
    </span>
  );
}

// The allowlist is stored as one comma-separated string. Splitting it here
// mirrors auth.ParseAllowed on the backend, so what is shown is what is in
// effect. Duplicates are dropped so the list never shows an address twice.
function splitAllowlist(raw: string | undefined): string[] {
  if (!raw) return [];
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of raw.split(",")) {
    const email = part.trim().toLowerCase();
    if (email === "" || seen.has(email)) continue;
    seen.add(email);
    out.push(email);
  }
  return out;
}

export function AdminPage({ onBack, isSuperAdmin }: AdminPageProps) {
  const [monitor, setMonitor] = useState<AdminMonitor | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [newAdminEmail, setNewAdminEmail] = useState("");
  // null until the first monitor load, then the editable copy of the allowlist.
  // The server value is re-synced on every load, so a save that succeeded cannot
  // be silently discarded, but unsaved edits survive unrelated reloads.
  const [allowedEmails, setAllowedEmails] = useState<string[] | null>(null);
  const [allowedDirty, setAllowedDirty] = useState(false);

  const load = useCallback(async () => {
    try {
      const next = await fetchAdminMonitor();
      setMonitor(next);
      setAllowedEmails((current) => {
        // Re-seed from the server only when the user has no pending edit, so an
        // in-progress change is not wiped by a background refresh.
        if (current !== null) return current;
        return splitAllowlist(
          next.settings.find((s) => s.key === "ALLOWED_EMAILS")?.value,
        );
      });
      setError(null);
    } catch (err) {
      setError("Failed to load admin monitor");
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
      setError(err instanceof Error ? err.message : "Request failed");
      console.error(err);
    } finally {
      setBusy(false);
    }
  }

  async function handleAddAdmin() {
    const email = newAdminEmail.trim();
    if (!email) return;
    await run(async () => {
      await addAdmin(email);
      setNewAdminEmail("");
    }, `Granted admin role to ${email}.`);
  }

  async function handleSaveAllowed(next: string[]) {
    setAllowedEmails(next);
    setAllowedDirty(true);
  }

  async function handleSaveAllowedCommit() {
    if (allowedEmails === null) return;
    if (allowedEmails.length === 0) {
      // Saving an empty list clears the database override. If the deployment
      // has no ALLOWED_EMAILS either, that reopens the gate to everyone, so
      // this cannot be a single unconfirmed click.
      const envFallback = allowedSetting?.value.trim() !== "";
      const warning = envFallback
        ? "This removes the override and reverts to the deployment allowlist, which may be different."
        : "This reverts to the deployment allowlist, which is empty — the access gate will be OPEN for everyone.";
      if (!confirm(`${warning}\n\nSave anyway?`)) return;
    }
    await run(async () => {
      await updateAdminSettings({ ALLOWED_EMAILS: allowedEmails.join(",") });
      setAllowedDirty(false);
    }, "Access list saved and applied.");
  }

  async function handleResetAllowed() {
    if (
      !confirm(
        "Remove the ALLOWED_EMAILS override and revert to the deployment value?",
      )
    )
      return;
    await run(async () => {
      await updateAdminSettings({ ALLOWED_EMAILS: "" });
      setAllowedDirty(false);
    }, "ALLOWED_EMAILS reverted to the deployment value.");
  }

  async function handleRemoveAdmin(email: string) {
    await run(async () => {
      await removeAdmin(email);
    }, `Revoked admin role from ${email}.`);
  }

  // Editing your own row goes through the self-service endpoint:
  // /api/admin/users is superadmin only, so an admin editing their own row
  // there would be refused. Everyone else's row stays on the admin endpoint.
  function isOwnRow(email: string) {
    const viewer = monitor?.viewer ?? "";
    return viewer !== "" && email.toLowerCase() === viewer.toLowerCase();
  }

  async function handleSaveUser(email: string, patch: UserProviderUpdate) {
    await run(async () => {
      if (isOwnRow(email)) {
        await saveOwnProvider(patch);
      } else {
        await saveUserProvider(patch);
      }
    }, `Saved provider settings for ${email}.`);
  }

  async function handleResetUser(email: string) {
    await run(async () => {
      if (isOwnRow(email)) {
        await resetOwnProvider();
      } else {
        await resetUserProvider(email);
      }
    }, `Reset ${email} to platform provider defaults.`);
  }

  // The only runtime setting still editable here is the access list, which has
  // its own editor below. Provider and model are per user, in the table above.
  const allowedSetting = monitor?.settings.find(
    (s) => s.key === "ALLOWED_EMAILS",
  );

  if (!monitor) {
    return (
      <div className="h-screen flex items-center justify-center">
        <div className="text-gray-500">{error ?? "Loading admin panel..."}</div>
      </div>
    );
  }

  return (
    <div className="h-screen flex flex-col max-w-5xl mx-auto p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Admin</h1>
          <p className="text-xs text-gray-400 mt-0.5">
            Signed in as {monitor.viewer || "unknown"} · role:{" "}
            {monitor.viewer_role}
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
        <div className="mb-4 p-4 bg-red-50 text-red-700 rounded-lg whitespace-pre-wrap">
          {error}
        </div>
      )}
      {notice && (
        <div className="mb-4 p-4 bg-green-50 text-green-700 rounded-lg">
          {notice}
        </div>
      )}
      {!isSuperAdmin && (
        <div className="mb-4 p-4 bg-amber-50 text-amber-800 rounded-lg">
          You have the admin role. You can edit the access list, and you can
          edit your own row under User API keys or in Provider settings.
          Changing platform provider defaults, other users' keys, and
          administrators requires the superadmin role.
        </div>
      )}

      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-6">
        <div className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="text-xs text-gray-400 mb-1">Database</div>
          <StatusBadge ok={monitor.database === "ok"} />
        </div>
        <div className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="text-xs text-gray-400 mb-1">Auth gate</div>
          <span
            className={`px-2 py-0.5 rounded-full text-xs font-medium ${
              monitor.auth_gate === "enabled"
                ? "bg-green-100 text-green-700"
                : "bg-amber-100 text-amber-700"
            }`}
          >
            {monitor.auth_gate}
          </span>
        </div>
        <div className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="text-xs text-gray-400 mb-1">Meetings</div>
          <div className="text-lg font-medium text-gray-900">
            {monitor.meetings >= 0 ? monitor.meetings : "—"}
          </div>
        </div>
        <div className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="text-xs text-gray-400 mb-1">Users</div>
          <div className="text-lg font-medium text-gray-900">
            {monitor.users.length}
          </div>
          <div className="text-xs text-gray-400">
            {monitor.users.filter((u) => u.use_own_keys).length} on own keys
          </div>
        </div>
      </div>

      <div className="rounded-xl border border-gray-200 bg-white mb-6">
        <div className="px-4 py-3 border-b border-gray-100">
          <h2 className="font-medium text-gray-900">Platform defaults</h2>
          <p className="text-xs text-gray-400 mt-0.5">
            Supplied by deployment secrets. Keys are never returned, only
            whether they exist.
          </p>
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 px-4 py-3 text-sm">
          <div>
            <div className="text-xs text-gray-400">STT</div>
            <div className="text-gray-900">{monitor.platform.stt_provider}</div>
            <div className="text-xs text-gray-400">
              {monitor.platform.google_stt
                ? "service account"
                : monitor.platform.stt_key_set
                  ? "key configured"
                  : "no key"}
            </div>
          </div>
          <div>
            <div className="text-xs text-gray-400">LLM</div>
            <div className="text-gray-900">{monitor.platform.llm_provider}</div>
            <div className="text-xs text-gray-400">
              {monitor.platform.llm_model || "default model"}
            </div>
          </div>
          <div>
            <div className="text-xs text-gray-400">LLM key</div>
            <div className="text-gray-900">
              {monitor.platform.llm_key_set ? "configured" : "not configured"}
            </div>
          </div>
          <div>
            <div className="text-xs text-gray-400">User key storage</div>
            <div className="text-gray-900">
              {monitor.platform.own_keys_ok ? "enabled" : "unavailable"}
            </div>
            {!monitor.platform.own_keys_ok && (
              <div className="text-xs text-amber-600">
                AUTH_HMAC_SECRET missing
              </div>
            )}
          </div>
        </div>
      </div>

      <div className="rounded-xl border border-gray-200 bg-white mb-6">
        <div className="px-4 py-3 border-b border-gray-100">
          <h2 className="font-medium text-gray-900">User API keys</h2>
          <p className="text-xs text-gray-400 mt-0.5">
            Each user either uses the platform defaults or their own providers.
            Keys are encrypted before storage and cannot be read back.
          </p>
        </div>
        {monitor.users.length === 0 ? (
          <p className="px-4 py-6 text-sm text-gray-400">No users yet.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-gray-400 border-b border-gray-100">
                <th className="px-4 py-2">User</th>
                <th className="px-4 py-2">Mode</th>
                <th className="px-4 py-2">STT</th>
                <th className="px-4 py-2">LLM</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {monitor.users.map((u) => (
                <UserProviderRow
                  key={u.email}
                  user={u}
                  // The superadmin edits anyone; an admin edits only their own
                  // row here, since other users' rows are superadmin territory.
                  canEdit={
                    (isSuperAdmin ||
                      u.email.toLowerCase() === monitor.viewer.toLowerCase()) &&
                    monitor.platform.own_keys_ok
                  }
                  busy={busy}
                  onSave={handleSaveUser}
                  onReset={handleResetUser}
                />
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="rounded-xl border border-gray-200 bg-white mb-6">
        <div className="px-4 py-3 border-b border-gray-100 flex items-start justify-between gap-4">
          <div>
            <h2 className="font-medium text-gray-900">Who can use this</h2>
            <p className="text-xs text-gray-400 mt-0.5">
              Everyone else is refused at sign-in. Press Enter or comma to add,
              or paste a list. The superadmin is always allowed.
            </p>
          </div>
          <span
            className={`shrink-0 px-2 py-0.5 rounded-full text-xs font-medium ${
              monitor.auth_gate === "enabled"
                ? "bg-green-100 text-green-700"
                : "bg-amber-100 text-amber-700"
            }`}
          >
            {monitor.auth_gate === "enabled"
              ? "list enforced"
              : "gate open to everyone"}
          </span>
        </div>
        <div className="px-4 py-3">
          <EmailTagInput
            id="allowed-emails"
            value={allowedEmails ?? []}
            onChange={handleSaveAllowed}
          />
          <div className="mt-3 flex items-center justify-between">
            <div className="text-xs text-gray-400">
              {allowedDirty && "Unsaved changes."}
              {allowedSetting?.set &&
                " Currently overridden from the deployment value."}
            </div>
            <div className="flex gap-3 items-center">
              {allowedSetting?.set && (
                <button
                  onClick={handleResetAllowed}
                  disabled={busy}
                  className="text-xs text-gray-400 hover:text-red-500 disabled:opacity-50"
                >
                  reset to deployment value
                </button>
              )}
              <button
                onClick={handleSaveAllowedCommit}
                disabled={busy || !allowedDirty}
                className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
              >
                {busy ? "Saving..." : "Save access list"}
              </button>
            </div>
          </div>
        </div>
      </div>

      <div className="rounded-xl border border-gray-200 bg-white mb-6">
        <div className="px-4 py-3 border-b border-gray-100">
          <h2 className="font-medium text-gray-900">Administrators</h2>
          <p className="text-xs text-gray-400 mt-0.5">
            Admins can edit the access list and monitor operational state.
            Granting or revoking administrators is left to the superadmin, who
            is configured only through the SUPERADMIN_EMAIL environment
            variable.
          </p>
        </div>
        <div className="px-4 py-3">
          <div className="mb-3">
            <div className="text-xs text-gray-400">
              Superadmin (env-configured)
            </div>
            <div className="text-sm text-gray-900">
              {monitor.superadmin || "not configured"}
            </div>
          </div>
          {monitor.admins.length > 0 ? (
            <ul className="divide-y divide-gray-50 mb-3">
              {monitor.admins.map((email) => (
                <li
                  key={email}
                  className="py-2 flex items-center justify-between"
                >
                  <span className="text-sm text-gray-700">{email}</span>
                  {isSuperAdmin && (
                    <button
                      onClick={() => handleRemoveAdmin(email)}
                      disabled={busy}
                      className="text-xs text-gray-400 hover:text-red-500 disabled:opacity-50"
                    >
                      remove
                    </button>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-gray-400 mb-3">No administrators yet.</p>
          )}
          {isSuperAdmin && (
            <div className="flex gap-2">
              <input
                type="text"
                value={newAdminEmail}
                onChange={(e) => setNewAdminEmail(e.target.value)}
                placeholder="admin@example.com"
                className="flex-1 px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <button
                onClick={handleAddAdmin}
                disabled={busy || !newAdminEmail.trim()}
                className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
              >
                Add admin
              </button>
            </div>
          )}
        </div>
      </div>

      <div className="rounded-xl border border-gray-200 bg-white">
        <div className="px-4 py-3 border-b border-gray-100">
          <h2 className="font-medium text-gray-900">Audit log</h2>
        </div>
        {monitor.audit.length === 0 ? (
          <p className="px-4 py-6 text-sm text-gray-400">
            No configuration changes yet.
          </p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-gray-400 border-b border-gray-100">
                <th className="px-4 py-2">Time</th>
                <th className="px-4 py-2">Actor</th>
                <th className="px-4 py-2">Action</th>
                <th className="px-4 py-2">Key</th>
              </tr>
            </thead>
            <tbody>
              {monitor.audit.map((a: AuditEntry) => (
                <tr
                  key={`${a.setting_key}-${a.created_at}`}
                  className="border-b border-gray-50 last:border-0"
                >
                  <td className="px-4 py-2 text-gray-500">
                    {new Date(a.created_at).toLocaleString()}
                  </td>
                  <td className="px-4 py-2 text-gray-700">
                    {a.admin_email}
                    <span className="ml-1 text-xs text-gray-400">{a.role}</span>
                  </td>
                  <td className="px-4 py-2">
                    <span
                      className={`px-2 py-0.5 rounded-full text-xs font-medium ${
                        a.action === "set"
                          ? "bg-blue-100 text-blue-700"
                          : "bg-amber-100 text-amber-700"
                      }`}
                    >
                      {a.action}
                    </span>
                  </td>
                  <td className="px-4 py-2 font-mono text-xs text-gray-700">
                    {a.setting_key}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
