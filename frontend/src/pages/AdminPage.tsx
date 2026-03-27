import { useState, useCallback, useEffect, useMemo } from 'react'
import type { AdminMediaItem } from '@/types/api'
import { getKeepRequests, getAdminMediaItems } from '@/api/admin'
import { MediaGrid } from '@/components/MediaGrid'
import { AdminMediaCard } from '@/components/AdminMediaCard'
import { KeepRequestCard } from '@/components/KeepRequestCard'
import { UserManagement } from '@/components/UserManagement'
import { TabContainer, TabContent } from '@/components/Tabs'
import { LoadingSpinner } from '@/components/ui'

type AdminStatusFilter = 'requested' | 'must-delete' | 'normal' | 'all'

const SORT_OPTIONS = [
  { value: 'delete-asc', label: 'Deletion Date (Soonest)' },
  { value: 'delete-desc', label: 'Deletion Date (Latest)' },
  { value: 'title-asc', label: 'Title (A-Z)' },
  { value: 'title-desc', label: 'Title (Z-A)' },
  { value: 'size-desc', label: 'File Size (Largest)' },
  { value: 'size-asc', label: 'File Size (Smallest)' },
]

const REQUEST_SORT_OPTIONS = [
  { value: 'delete-asc', label: 'Deletion Date (Soonest)' },
  { value: 'delete-desc', label: 'Deletion Date (Latest)' },
  { value: 'title-asc', label: 'Title (A-Z)' },
  { value: 'title-desc', label: 'Title (Z-A)' },
]

function sortAdminItems(items: AdminMediaItem[], sortBy: string): AdminMediaItem[] {
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

export default function AdminPage() {
  const [activeTab, setActiveTab] = useState('requests')
  const [requests, setRequests] = useState<AdminMediaItem[]>([])
  const [mediaItems, setMediaItems] = useState<AdminMediaItem[]>([])
  const [loadingRequests, setLoadingRequests] = useState(true)
  const [loadingMedia, setLoadingMedia] = useState(true)
  const [statusFilter, setStatusFilter] = useState<AdminStatusFilter>('all')

  const fetchRequests = useCallback(async () => {
    setLoadingRequests(true)
    try {
      const data = await getKeepRequests()
      setRequests(data.keepRequests ?? [])
    } catch {
      // handled by client
    } finally {
      setLoadingRequests(false)
    }
  }, [])

  const fetchMedia = useCallback(async () => {
    setLoadingMedia(true)
    try {
      const data = await getAdminMediaItems()
      setMediaItems(data.mediaItems ?? [])
    } catch {
      // handled by client
    } finally {
      setLoadingMedia(false)
    }
  }, [])

  useEffect(() => {
    fetchRequests()
    fetchMedia()
  }, [fetchRequests, fetchMedia])

  const handleRemoveRequest = useCallback((id: number) => {
    setRequests((prev) => prev.filter((r) => r.ID !== id))
  }, [])

  const handleRemoveMedia = useCallback((id: number) => {
    setMediaItems((prev) => prev.filter((m) => m.ID !== id))
  }, [])

  // Pre-filter media by status
  const filteredMedia = useMemo(() => {
    switch (statusFilter) {
      case 'requested':
        return mediaItems.filter((i) => i.Request?.Status === 'pending')
      case 'must-delete':
        return mediaItems.filter((i) => i.Unkeepable)
      case 'normal':
        return mediaItems.filter((i) => !i.Request?.Status && !i.Unkeepable)
      case 'all':
        return mediaItems
    }
  }, [mediaItems, statusFilter])

  const requestLibraries = useMemo(
    () => [...new Set(requests.map((r) => r.LibraryName))].sort(),
    [requests],
  )

  const mediaLibraries = useMemo(
    () => [...new Set(mediaItems.map((m) => m.LibraryName))].sort(),
    [mediaItems],
  )

  const loading = loadingRequests && loadingMedia

  return (
    <div>
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-center gap-3 flex-wrap">
          <h1 className="text-2xl sm:text-3xl font-bold text-gray-100">Admin Panel</h1>
          {requests.length > 0 && (
            <span className="inline-flex items-center px-3 py-1 rounded-full text-sm font-medium bg-yellow-500/20 text-yellow-400">
              {requests.length} pending {requests.length === 1 ? 'request' : 'requests'}
            </span>
          )}
        </div>
        <p className="mt-1 text-gray-400">
          Manage keep requests, media items, and user permissions.
        </p>
      </div>

      {loading ? (
        <div className="flex justify-center py-20">
          <LoadingSpinner size="lg" />
        </div>
      ) : (
        <TabContainer
          tabs={[
            { id: 'requests', label: `Approval Queue${requests.length > 0 ? ` (${requests.length})` : ''}` },
            { id: 'media', label: 'Keep or Sweep' },
            { id: 'users', label: 'Users' },
          ]}
          activeTab={activeTab}
          onTabChange={setActiveTab}
        >
          <TabContent id="requests" activeTab={activeTab}>
            {requests.length === 0 ? (
              <div className="text-center py-12">
                <svg className="w-16 h-16 mx-auto text-gray-600 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <p className="text-gray-400 text-lg">No pending requests</p>
                <p className="text-gray-500 text-sm mt-1">All keep requests have been handled.</p>
              </div>
            ) : (
              <MediaGrid
                items={requests}
                renderCard={(item) => <KeepRequestCard item={item} onRemove={handleRemoveRequest} />}
                getKey={(item) => item.ID}
                searchField={(item) => item.Title}
                libraries={requestLibraries}
                getLibrary={(item) => item.LibraryName}
                sortOptions={REQUEST_SORT_OPTIONS}
                sortFn={sortAdminItems}
                loading={loadingRequests}
                onRefresh={fetchRequests}
              />
            )}
          </TabContent>

          <TabContent id="media" activeTab={activeTab}>
            <MediaGrid
              items={filteredMedia}
              renderCard={(item) => <AdminMediaCard item={item} onRemove={handleRemoveMedia} />}
              getKey={(item) => item.ID}
              searchField={(item) => item.Title}
              libraries={mediaLibraries}
              getLibrary={(item) => item.LibraryName}
              sortOptions={SORT_OPTIONS}
              sortFn={sortAdminItems}
              loading={loadingMedia}
              onRefresh={fetchMedia}
              extraFilters={
                <select
                  value={statusFilter}
                  onChange={(e) => setStatusFilter(e.target.value as AdminStatusFilter)}
                  className="px-3 py-2 bg-gray-800 border border-gray-700 text-gray-100 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent text-sm"
                >
                  <option value="all">All Items</option>
                  <option value="requested">Keep Requested</option>
                  <option value="must-delete">Must Delete</option>
                  <option value="normal">Normal</option>
                </select>
              }
            />
          </TabContent>

          <TabContent id="users" activeTab={activeTab}>
            <UserManagement />
          </TabContent>
        </TabContainer>
      )}
    </div>
  )
}
