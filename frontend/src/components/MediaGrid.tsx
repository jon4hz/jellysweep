import { useState, useCallback, useMemo, useRef, useEffect } from 'react'
import { debounce } from '@/lib/utils'
import { SkeletonGrid } from '@/components/ui'

interface MediaGridProps<T> {
  items: T[]
  renderCard: (item: T) => React.ReactNode
  getKey: (item: T) => string | number
  searchField: (item: T) => string
  libraries: string[]
  getLibrary: (item: T) => string
  sortOptions: { value: string; label: string }[]
  sortFn: (items: T[], sortBy: string) => T[]
  loading?: boolean
  pageSize?: number
  extraFilters?: React.ReactNode
  onRefresh?: () => void
}

export function MediaGrid<T>({
  items,
  renderCard,
  getKey,
  searchField,
  libraries,
  getLibrary,
  sortOptions,
  sortFn,
  loading = false,
  pageSize = 12,
  extraFilters,
  onRefresh,
}: MediaGridProps<T>) {
  const [search, setSearch] = useState('')
  const [library, setLibrary] = useState('')
  const [sortBy, setSortBy] = useState(sortOptions[0]?.value ?? '')
  const [visibleCount, setVisibleCount] = useState(pageSize)
  const sentinelRef = useRef<HTMLDivElement>(null)

  // Filter + sort
  const processed = useMemo(() => {
    let filtered = items
    if (search) {
      const q = search.toLowerCase()
      filtered = filtered.filter((item) => searchField(item).toLowerCase().includes(q))
    }
    if (library) {
      filtered = filtered.filter((item) => getLibrary(item) === library)
    }
    return sortFn(filtered, sortBy)
  }, [items, search, library, sortBy, searchField, getLibrary, sortFn, sortOptions])

  const visible = useMemo(() => processed.slice(0, visibleCount), [processed, visibleCount])
  const hasMore = visibleCount < processed.length

  // Reset visible count when filters change
  useEffect(() => {
    setVisibleCount(pageSize)
  }, [search, library, sortBy, pageSize])

  // Intersection observer for infinite scroll
  useEffect(() => {
    if (!sentinelRef.current) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && hasMore) {
          setVisibleCount((prev) => prev + pageSize)
        }
      },
      { rootMargin: '200px' },
    )
    observer.observe(sentinelRef.current)
    return () => observer.disconnect()
  }, [hasMore, pageSize])

  const debouncedSearch = useMemo(() => debounce((val: string) => setSearch(val), 300), [])
  const handleSearch = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    debouncedSearch(e.target.value)
  }, [debouncedSearch])

  if (loading) return <SkeletonGrid count={pageSize} />

  return (
    <div>
      {/* Filter bar */}
      <div className="card-no-hover p-4 mb-6">
        <div className="flex flex-col sm:flex-row gap-3">
          {/* Search */}
          <div className="flex-1">
            <div className="relative">
              <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
              <input
                type="text"
                placeholder="Search..."
                onChange={handleSearch}
                className="w-full pl-10 pr-3 py-2 bg-gray-800 border border-gray-700 text-gray-100 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent text-sm"
              />
            </div>
          </div>

          {/* Library filter */}
          {libraries.length > 1 && (
            <select
              value={library}
              onChange={(e) => setLibrary(e.target.value)}
              className="px-3 py-2 bg-gray-800 border border-gray-700 text-gray-100 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent text-sm"
            >
              <option value="">All Libraries</option>
              {libraries.map((lib) => (
                <option key={lib} value={lib}>{lib}</option>
              ))}
            </select>
          )}

          {/* Sort */}
          {sortOptions.length > 1 && (
            <select
              value={sortBy}
              onChange={(e) => setSortBy(e.target.value)}
              className="px-3 py-2 bg-gray-800 border border-gray-700 text-gray-100 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent text-sm"
            >
              {sortOptions.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
          )}

          {extraFilters}

          {onRefresh && (
            <button
              onClick={onRefresh}
              className="px-3 py-2 bg-gray-800 border border-gray-700 text-gray-100 rounded-lg hover:bg-gray-700 transition-colors text-sm flex items-center gap-1.5"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              Refresh
            </button>
          )}
        </div>

        {/* Results count */}
        <div className="mt-2 text-xs text-gray-500">
          Showing {visible.length} of {processed.length} items
          {processed.length !== items.length && ` (${items.length} total)`}
        </div>
      </div>

      {/* Grid */}
      {visible.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-gray-500">No items match your filters.</p>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-4 sm:gap-6" style={{ padding: 4, margin: -4 }}>
            {visible.map((item) => (
              <div key={getKey(item)}>{renderCard(item)}</div>
            ))}
          </div>
          {hasMore && (
            <div ref={sentinelRef} className="flex justify-center py-8">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-500" />
            </div>
          )}
        </>
      )}
    </div>
  )
}
