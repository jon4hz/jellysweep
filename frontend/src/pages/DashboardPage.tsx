import { useState, useCallback, useMemo } from 'react'
import type { UserMediaItem } from '@/types/api'
import { getMediaItems } from '@/api/media'
import { formatFileSize } from '@/lib/utils'
import { useAuth } from '@/context/AuthContext'
import { MediaGrid } from '@/components/MediaGrid'
import { MediaCard } from '@/components/MediaCard'
import { StatsCharts } from '@/components/StatsCharts'
import { TabContainer, TabContent } from '@/components/Tabs'
import { LoadingSpinner, EmptyState } from '@/components/ui'
import { useEffect } from 'react'

type RequestFilter = 'unrequested' | 'requested' | 'unavailable' | 'all'

const SORT_OPTIONS = [
  { value: 'delete-asc', label: 'Deletion Date (Soonest)' },
  { value: 'delete-desc', label: 'Deletion Date (Latest)' },
  { value: 'title-asc', label: 'Title (A-Z)' },
  { value: 'title-desc', label: 'Title (Z-A)' },
  { value: 'size-desc', label: 'File Size (Largest)' },
  { value: 'size-asc', label: 'File Size (Smallest)' },
]

function sortItems(items: UserMediaItem[], sortBy: string): UserMediaItem[] {
  const sorted = [...items]
  switch (sortBy) {
    case 'delete-asc':
      return sorted.sort((a, b) => new Date(a.DefaultDeleteAt).getTime() - new Date(b.DefaultDeleteAt).getTime())
    case 'delete-desc':
      return sorted.sort((a, b) => new Date(b.DefaultDeleteAt).getTime() - new Date(a.DefaultDeleteAt).getTime())
    case 'title-asc':
      return sorted.sort((a, b) => a.Title.localeCompare(b.Title))
    case 'title-desc':
      return sorted.sort((a, b) => b.Title.localeCompare(a.Title))
    case 'size-desc':
      return sorted.sort((a, b) => b.FileSize - a.FileSize)
    case 'size-asc':
      return sorted.sort((a, b) => a.FileSize - b.FileSize)
    default:
      return sorted
  }
}

export default function DashboardPage() {
  const { user } = useAuth()
  const [items, setItems] = useState<UserMediaItem[]>([])
  const [loading, setLoading] = useState(true)
  const [requestFilter, setRequestFilter] = useState<RequestFilter>('unrequested')
  const [activeTab, setActiveTab] = useState('overview')

  const fetchItems = useCallback(async (refresh = false) => {
    setLoading(true)
    try {
      const data = await getMediaItems(refresh)
      setItems(data.mediaItems ?? [])
    } catch {
      // error handled by api client
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchItems() }, [fetchItems])

  const handleRemove = useCallback((id: number) => {
    setItems((prev) => prev.filter((item) => item.ID !== id))
  }, [])

  // Pre-filter by request status before passing to MediaGrid
  const filteredByRequest = useMemo(() => {
    switch (requestFilter) {
      case 'unrequested':
        return items.filter((i) => !i.Request?.ID && !i.Unkeepable)
      case 'requested':
        return items.filter((i) => i.Request?.ID)
      case 'unavailable':
        return items.filter((i) => i.Unkeepable)
      case 'all':
        return items
    }
  }, [items, requestFilter])

  const libraries = useMemo(
    () => [...new Set(items.map((i) => i.LibraryName))].sort(),
    [items],
  )

  const totalSize = useMemo(
    () => items.reduce((acc, i) => acc + i.FileSize, 0),
    [items],
  )

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <LoadingSpinner size="lg" />
      </div>
    )
  }

  const itemCount = items.length

  return (
    <div>
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-center gap-3 flex-wrap">
          <h1 className="text-2xl sm:text-3xl font-bold text-gray-100">Media Dashboard</h1>
          <span className={`inline-flex items-center px-3 py-1 rounded-full text-sm font-medium ${
            itemCount === 0
              ? 'bg-green-500/20 text-green-400'
              : 'bg-red-500/20 text-red-400'
          }`}>
            {itemCount} {itemCount === 1 ? 'item' : 'items'} scheduled
          </span>
        </div>
        <p className="mt-1 text-gray-400">
          {itemCount > 0
            ? `${formatFileSize(totalSize)} of media across ${libraries.length} ${libraries.length === 1 ? 'library' : 'libraries'}`
            : 'No media currently scheduled for deletion'}
        </p>
      </div>

      {itemCount === 0 ? (
        <EmptyState
          icon={
            <svg className="w-16 h-16" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          }
          title="No media scheduled for deletion"
          description={user?.isAdmin
            ? 'Your media libraries are clean. Run a scan to check for new items.'
            : 'There are no items scheduled for deletion right now.'}
          action={user?.isAdmin ? (
            <button onClick={() => fetchItems(true)} className="btn-primary">
              Refresh
            </button>
          ) : undefined}
        />
      ) : (
        <TabContainer
          tabs={[
            { id: 'overview', label: 'Overview' },
            { id: 'stats', label: 'Stats' },
          ]}
          activeTab={activeTab}
          onTabChange={setActiveTab}
        >
          <TabContent id="overview" activeTab={activeTab}>
            <MediaGrid
              items={filteredByRequest}
              renderCard={(item) => <MediaCard item={item} onRemove={handleRemove} />}
              getKey={(item) => item.ID}
              searchField={(item) => item.Title}
              libraries={libraries}
              getLibrary={(item) => item.LibraryName}
              sortOptions={SORT_OPTIONS}
              sortFn={sortItems}
              pageSize={12}
              onRefresh={() => fetchItems(true)}
              extraFilters={
                <select
                  value={requestFilter}
                  onChange={(e) => setRequestFilter(e.target.value as RequestFilter)}
                  className="px-3 py-2 bg-gray-800 border border-gray-700 text-gray-100 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent text-sm"
                >
                  <option value="unrequested">Unrequested</option>
                  <option value="requested">Requested</option>
                  <option value="unavailable">Unavailable</option>
                  <option value="all">All Items</option>
                </select>
              }
            />
          </TabContent>
          <TabContent id="stats" activeTab={activeTab}>
            <StatsCharts items={items} />
          </TabContent>
        </TabContainer>
      )}
    </div>
  )
}
