import { useRef, useState } from 'react';
import { MeetingDocument } from '../../types';

interface DocumentsPanelProps {
  documents: MeetingDocument[];
  onUpload: (files: File[]) => Promise<void>;
  onClear: () => Promise<void>;
}

const ACCEPT = '.pdf,.docx,.pptx,.xlsx,.csv,.txt,.md,.markdown';

export function DocumentsPanel({ documents, onUpload, onClear }: DocumentsPanelProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleFiles = async (files: FileList | null) => {
    if (!files || files.length === 0) return;
    setUploading(true);
    setError(null);
    try {
      await onUpload(Array.from(files));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to upload documents');
      console.error(err);
    } finally {
      setUploading(false);
      if (inputRef.current) inputRef.current.value = '';
    }
  };

  const handleClear = async () => {
    setClearing(true);
    setError(null);
    try {
      await onClear();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to clear documents');
      console.error(err);
    } finally {
      setClearing(false);
    }
  };

  return (
    <div className="border-b border-steel/30 p-4">
      <div className="flex items-center justify-between mb-1">
        <h2 className="text-sm font-medium text-ink">Meeting Documents</h2>
        {documents.length > 0 && (
          <button
            onClick={handleClear}
            disabled={clearing || uploading}
            className="text-xs text-coral-600 hover:text-coral-700 disabled:opacity-50"
          >
            {clearing ? 'Clearing...' : 'Clear all'}
          </button>
        )}
      </div>

      <p className="text-xs text-muted mb-2">
        Forgot to record? Upload the agenda, notes, or report and generate the summary from them.
        PDF, DOCX, PPTX, XLSX, CSV, TXT, MD.
      </p>

      <div className="flex items-center gap-2">
        <input
          ref={inputRef}
          type="file"
          multiple
          accept={ACCEPT}
          onChange={(e) => handleFiles(e.target.files)}
          disabled={uploading}
          className="block w-full text-xs text-muted file:mr-2 file:py-1 file:px-2 file:rounded file:border-0 file:bg-brand-100 file:text-brand-700 file:text-xs file:font-semibold hover:file:bg-brand-300/50 disabled:opacity-50"
        />
      </div>

      {uploading && <p className="mt-2 text-xs text-muted">Uploading and extracting text...</p>}
      {error && (
        <div className="mt-2 p-2 bg-coral-100 text-coral-700 text-xs rounded-lg whitespace-pre-wrap">
          {error}
        </div>
      )}

      {documents.length > 0 ? (
        <ul className="mt-3 space-y-1">
          {documents.map((doc) => (
            <li
              key={doc.name}
              className="flex items-center justify-between text-xs bg-wash border border-steel/30 rounded px-2 py-1"
            >
              <span className="block truncate text-ink text-ellipsis" title={doc.name}>
                {doc.name}
              </span>
              <span className="ml-2 shrink-0 text-muted">{doc.chars.toLocaleString()} chars</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="mt-3 text-xs italic text-muted">No documents uploaded.</p>
      )}
    </div>
  );
}
