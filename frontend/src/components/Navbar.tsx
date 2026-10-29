// Global top bar for the signed-in user (identity + sign out). It renders in
// normal flow above the current view — not as a floating overlay — so it can
// never land on a view's own top-right controls. App omits it entirely while
// the meeting room is open: that view owns the whole top edge (connection
// status, Start/Pause/End, mic controls) and needs the space.
export function Navbar({ email, onLogout }: { email: string; onLogout: () => void }) {
  return (
    <nav className="shrink-0 bg-canvas border-b border-steel/30">
      <div className="max-w-7xl w-full mx-auto px-6 py-2 flex items-center justify-end gap-3 text-xs">
        <span className="text-muted">{email}</span>
        <button
          onClick={onLogout}
          className="px-2.5 py-1 rounded-lg border border-steel/50 text-ink hover:bg-wash transition-colors"
        >
          Sign out
        </button>
      </div>
    </nav>
  );
}
