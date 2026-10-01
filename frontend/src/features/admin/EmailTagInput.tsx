import { useState } from 'react';

// Deliberately permissive: the goal is to catch obvious mistakes (missing @,
// spaces, a pasted list) without rejecting valid addresses that a stricter
// RFC-compliant regex would refuse. The backend normalizes and de-duplicates.
function looksLikeEmail(value: string): boolean {
  const trimmed = value.trim();
  if (trimmed.length > 254 || /\s/.test(trimmed)) return false;
  const at = trimmed.indexOf('@');
  if (at <= 0 || at !== trimmed.lastIndexOf('@')) return false;
  const domain = trimmed.slice(at + 1);
  return domain.includes('.') && !domain.startsWith('.') && !domain.endsWith('.');
}

// Splits on commas, semicolons, and whitespace so a list pasted from a document
// or an email header turns into separate chips instead of one broken chip.
function splitEntries(raw: string): string[] {
  return raw.split(/[,;\s]+/).map((p) => p.trim()).filter(Boolean);
}

export function EmailTagInput({
  value,
  onChange,
  disabled,
  placeholder = 'name@company.com',
  id,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
  placeholder?: string;
  id?: string;
}) {
  const [draft, setDraft] = useState('');
  const [invalid, setInvalid] = useState<string[]>([]);

  function commit(raw: string) {
    const candidates = splitEntries(raw);
    if (candidates.length === 0) return;

    const accepted: string[] = [];
    const rejected: string[] = [];
    for (const candidate of candidates) {
      if (looksLikeEmail(candidate)) {
        accepted.push(candidate.toLowerCase());
      } else {
        rejected.push(candidate);
      }
    }

    setInvalid(rejected);
    if (accepted.length > 0) {
      // De-duplicate case-insensitively so the same person cannot be added twice.
      const merged = [...value];
      for (const email of accepted) {
        if (!merged.some((existing) => existing.toLowerCase() === email)) {
          merged.push(email);
        }
      }
      onChange(merged);
    }
  }

  function remove(email: string) {
    onChange(value.filter((existing) => existing !== email));
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter' || e.key === ',' || e.key === ';') {
      // Commit the chip instead of submitting the surrounding form.
      e.preventDefault();
      commit(draft);
      setDraft('');
      return;
    }
    if (e.key === 'Backspace' && draft === '' && value.length > 0) {
      remove(value[value.length - 1]);
    }
  }

  return (
    <div>
      <div
        onClick={() => {
          const input = document.getElementById(id ?? '');
          input?.focus();
        }}
        className={`flex flex-wrap items-center gap-1.5 p-2 rounded-lg border border-gray-300 bg-white min-h-[38px] ${
          disabled ? 'bg-gray-50' : ''
        } ${invalid.length > 0 ? 'border-red-300' : 'focus-within:ring-2 focus-within:ring-blue-500'}`}
      >
        {value.map((email) => (
          <span
            key={email}
            className="inline-flex items-center gap-1 pl-2 pr-1 py-0.5 bg-blue-50 text-blue-700 rounded-full text-xs"
          >
            {email}
            {!disabled && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  remove(email);
                }}
                aria-label={`Remove ${email}`}
                className="w-4 h-4 rounded-full hover:bg-blue-200 flex items-center justify-center"
              >
                ×
              </button>
            )}
          </span>
        ))}
        <input
          id={id}
          type="text"
          value={draft}
          disabled={disabled}
          onChange={(e) => {
            setDraft(e.target.value);
            if (invalid.length > 0) setInvalid([]);
          }}
          onBlur={() => {
            if (draft.trim() !== '') {
              commit(draft);
              setDraft('');
            }
          }}
          onKeyDown={handleKeyDown}
          placeholder={value.length === 0 ? placeholder : ''}
          className="flex-1 min-w-[180px] px-1 py-0.5 text-sm outline-none bg-transparent"
        />
      </div>
      {invalid.length > 0 && (
        <p className="mt-1 text-xs text-red-600">
          Not a valid email address: {invalid.join(', ')}
        </p>
      )}
    </div>
  );
}
