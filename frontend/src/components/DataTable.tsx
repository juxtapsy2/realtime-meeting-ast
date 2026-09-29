import React, { useEffect, useMemo, useRef, useState } from 'react';

export type SortDirection = 'asc' | 'desc';

export interface DataTableColumn<T> {
  /** Key used to read the raw value off each row, or the key a customFields/valueMappings renderer matches on. */
  field: string;
  label: string;
  /** Whether clicking this header toggles sorting (only active when enableSort is set). */
  sortable?: boolean;
  /** Optional override for the value used when sorting (e.g. timestamps as numbers). */
  sortValue?: (row: T) => string | number;
  align?: 'left' | 'right';
  width?: string;
}

export interface DataTableProps<T> {
  columns: DataTableColumn<T>[];
  data: T[];
  rowKey: (row: T) => string | number;
  /** Master switch for header sorting. Defaults to false. */
  enableSort?: boolean;
  /** Starting sort state (e.g. match the server's default ordering). */
  initialSort?: { field: string; direction: SortDirection } | null;
  /** Called on sort toggle. Provide this to own the ordering (e.g. server-side refetch). null resets sorting. */
  onSortChange?: (sort: { field: string; direction: SortDirection } | null) => void;
  /** Raw value -> display renderers, keyed by column field. Runs before the default cell text. */
  valueMappings?: Record<string, (value: unknown, row: T) => React.ReactNode>;
  /** Full-cell renderers for fields that need custom markup (e.g. actions, composite cells). */
  customFields?: Record<string, (row: T) => React.ReactNode>;
  onRowClick?: (row: T) => void;
  /** Extra classes merged onto each row. */
  getRowClassName?: (row: T) => string;
  emptyMessage?: string;
  /** Max rows to render; the rows beyond are revealed as onRevealMore fires. */
  visibleLimit?: number;
  onRevealMore?: () => void;
  className?: string;
}

function compareValues(a: string | number, b: string | number): number {
  if (typeof a === 'number' && typeof b === 'number') return a - b;
  if (typeof a === 'number') return -1;
  if (typeof b === 'number') return 1;
  return a.localeCompare(b, undefined, { numeric: true });
}

function resolveSortValue<T>(col: DataTableColumn<T>, row: T): string | number {
  const v = col.sortValue ? col.sortValue(row) : (row as Record<string, unknown>)[col.field];
  if (v == null) return '';
  return typeof v === 'number' ? v : String(v);
}

export function DataTable<T>({
  columns,
  data,
  rowKey,
  enableSort = false,
  initialSort = null,
  onSortChange,
  valueMappings,
  customFields,
  onRowClick,
  getRowClassName,
  emptyMessage,
  visibleLimit,
  onRevealMore,
  className,
}: DataTableProps<T>) {
  const [sort, setSort] = useState<{ field: string; direction: SortDirection } | null>(initialSort);

  const scrollRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);

  const handleSort = (field: string) => {
    if (!enableSort) return;
    // Click 1: asc. Click 2: desc. Click 3: cancel (back to default order).
    let next: { field: string; direction: SortDirection } | null;
    if (sort?.field !== field) {
      next = { field, direction: 'asc' };
    } else if (sort.direction === 'asc') {
      next = { field, direction: 'desc' };
    } else {
      next = null;
    }
    setSort(next);
    onSortChange?.(next);
  };

  const sortedData = useMemo(() => {
    if (!sort) return data;
    const col = columns.find((c) => c.field === sort.field && c.sortable);
    if (!col) return data;
    const dir = sort.direction === 'asc' ? 1 : -1;
    return [...data].sort((x, y) => dir * compareValues(resolveSortValue(col, x), resolveSortValue(col, y)));
  }, [data, sort, columns]);

  // Sorting runs over the FULL data set; only a visible window (visibleLimit)
  // is rendered. This keeps global ordering (e.g. ID asc puts #1 first) while
  // still bounding the DOM as more rows are revealed by scrolling.
  const rows = visibleLimit != null ? sortedData.slice(0, visibleLimit) : sortedData;
  const hasMoreRows = rows.length < sortedData.length;

  useEffect(() => {
    const sentinel = sentinelRef.current;
    const root = scrollRef.current;
    if (!sentinel || !root || !onRevealMore) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasMoreRows) onRevealMore();
      },
      { root, rootMargin: '120px' }
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [onRevealMore, hasMoreRows, rows.length]);

  const renderCell = (col: DataTableColumn<T>, row: T): React.ReactNode => {
    const custom = customFields?.[col.field];
    if (custom) return custom(row);
    const mapped = valueMappings?.[col.field];
    if (mapped) return mapped((row as Record<string, unknown>)[col.field], row);
    const value = (row as Record<string, unknown>)[col.field];
    if (value == null) return '';
    return String(value);
  };

  return (
    <div ref={scrollRef} className={`flex-1 min-h-0 overflow-y-auto ${className ?? ''}`}>
      {sortedData.length === 0 ? (
        <div className="h-full flex items-center justify-center text-gray-500">
          {emptyMessage ?? 'No data'}
        </div>
      ) : (
        <>
          <table className="w-full text-left">
            <thead className="sticky top-0 bg-white z-10 border-b border-gray-200">
              <tr>
                {columns.map((col) => {
                  const isSortable = enableSort && !!col.sortable;
                  const isActive = sort?.field === col.field;
                  return (
                    <th
                      key={col.field}
                      style={col.width ? { width: col.width } : undefined}
                      className={`py-3 px-4 text-xs uppercase tracking-wide text-gray-500 font-medium ${col.align === 'right' ? 'text-right' : ''}`}
                    >
                      {isSortable ? (
                        <button
                          type="button"
                          onClick={() => handleSort(col.field)}
                          className="inline-flex items-center gap-1 hover:text-gray-700"
                        >
                          <span>{col.label}</span>
                          {isActive ? (
                            <span>{sort?.direction === 'asc' ? '▲' : '▼'}</span>
                          ) : (
                            <span className="text-gray-300">▲</span>
                          )}
                        </button>
                      ) : (
                        col.label
                      )}
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr
                  key={rowKey(row)}
                  onClick={onRowClick ? () => onRowClick(row) : undefined}
                  className={`border-b border-gray-100 ${
                    onRowClick ? 'cursor-pointer hover:bg-blue-50/50 transition-colors' : ''
                  } ${getRowClassName?.(row) ?? ''}`}
                >
                  {columns.map((col) => (
                    <td
                      key={col.field}
                      className={`px-4 py-3 ${col.align === 'right' ? 'text-right' : ''}`}
                    >
                      {renderCell(col, row)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>

          <div ref={sentinelRef} className="flex items-center justify-center h-12 text-sm">
            {hasMoreRows ? (
              <span className="text-gray-400">Scroll for more</span>
            ) : (
              <span className="text-gray-400">End of list</span>
            )}
          </div>
        </>
      )}
    </div>
  );
}