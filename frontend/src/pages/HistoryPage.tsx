import { useState, useEffect, useCallback } from 'react'
import type { HistoryEventItem, HistoryResponse } from '@/types/api'
import { getHistory } from '@/api/admin'
import { formatDate, formatExactDate } from '@/lib/utils'
import { LoadingSpinner, showToast } from '@/components/ui'

const EVENT_TYPES = [
  { value: 'picked_up', label: 'Picked Up', emoji: '📥', color: 'bg-blue-500/20 text-blue-400' },
  { value: 'protected', label: 'Protected', emoji: '🛡️', color: 'bg-green-500/20 text-green-400' },
  { value: 'unprotected', label: 'Unprotected', emoji: '🔓', color: 'bg-yellow-500/20 text-yellow-400' },
  { value: 'protection_expired', label: 'Protection Expired', emoji: '⏰', color: 'bg-orange-500/20 text-orange-400' },
  { value: 'deleted', label: 'Deleted', emoji: '🗑️', color: 'bg-red-500/20 text-red-400' },
  { value: 'request_created', label: 'Request Created', emoji: '📝', color: 'bg-purple-500/20 text-purple-400' },
  { value: 'request_approved', label: 'Request Approved', emoji: '✅', color: 'bg-green-500/20 text-green-400' },
  { value: 'request_denied', label: 'Request Denied', emoji: '❌', color: 'bg-red-500/20 text-red-400' },
  { value: 'keep_forever', label: 'Keep Forever', emoji: '🔒', color: 'bg-indigo-500/20 text-indigo-400' },
  { value: 'admin_keep', label: 'Admin Keep', emoji: '💚', color: 'bg-green-500/20 text-green-400' },
  { value: 'admin_unkeep', label: 'Admin Unkeep', emoji: '💔', color: 'bg-red-500/20 text-red-400' },
  { value: 'not_found', label: 'Not Found', emoji: '🔍', color: 'bg-gray-500/20 text-gray-400' },
]

function getEventBadge(eventType: string) {
  const evt = EVENT_TYPES.find((e) => e.value === eventType)
  if (!evt) return <span className="px-2 py-0.5 text-xs rounded-full bg-gray-500/20 text-gray-400">{eventType}</span>
  return (
    <span className={`px-2 py-0.5 text-xs font-medium rounded-full ${evt.color}`}>
      {evt.emoji} {evt.label}
    </span>
  )
}

export default function HistoryPage() {
  const [data, setData] = useState<HistoryResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(1)
  const [sortBy, setSortBy] = useState('event_time')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc')
  const [selectedTypes, setSelectedTypes] = useState<string[]>([])
  const [showFilterModal, setShowFilterModal] = useState(false)
  const [detailItem, setDetailItem] = useState<HistoryEventItem | null>(null)
  const [detailEvents, setDetailEvents] = useState<HistoryEventItem[]>([])
  const [loadingDetail, setLoadingDetail] = useState(false)
  const pageSize = 20

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const result = await getHistory({
        page,
        pageSize,
        sortBy,
        sortOrder,
        includeEventTypes: selectedTypes.length > 0 ? selectedTypes : undefined,
      })
      setData(result)
    } catch {
      // handled by client
    } finally {
      setLoading(false)
    }
  }, [page, sortBy, sortOrder, selectedTypes])

  useEffect(() => { fetchData() }, [fetchData])

  const handleSort = useCallback((column: string) => {
    if (sortBy === column) {
      setSortOrder((prev) => prev === 'asc' ? 'desc' : 'asc')
    } else {
      setSortBy(column)
      setSortOrder('desc')
    }
    setPage(1)
  }, [sortBy])

  const handleFilterToggle = useCallback((eventType: string) => {
    setSelectedTypes((prev) =>
      prev.includes(eventType)
        ? prev.filter((t) => t !== eventType)
        : [...prev, eventType],
    )
  }, [])

  const handleApplyFilters = useCallback(() => {
    setShowFilterModal(false)
    setPage(1)
  }, [])

  const handleShowDetail = useCallback(async (item: HistoryEventItem) => {
    setDetailItem(item)
    setLoadingDetail(true)
    try {
      const result = await getHistory({
        jellyfinId: item.JellyfinID,
        pageSize: 100,
        sortBy: 'event_time',
        sortOrder: 'desc',
      })
      setDetailEvents(result.items ?? [])
    } catch {
      showToast('Failed to load media history', 'error')
    } finally {
      setLoadingDetail(false)
    }
  }, [])

  const SortIcon = ({ column }: { column: string }) => {
    if (sortBy !== column) return <span className="text-gray-600 ml-1">↕</span>
    return <span className="text-indigo-400 ml-1">{sortOrder === 'asc' ? '↑' : '↓'}</span>
  }

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-8 flex-wrap gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold text-gray-100">Activity History</h1>
          <p className="mt-1 text-gray-400">Browse all media events and actions.</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => setShowFilterModal(true)}
            className={`px-3 py-2 text-sm rounded-lg border transition-colors flex items-center gap-1.5 ${
              selectedTypes.length > 0
                ? 'bg-indigo-600/20 border-indigo-500/50 text-indigo-400'
                : 'bg-gray-800 border-gray-700 text-gray-300 hover:bg-gray-700'
            }`}
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.293A1 1 0 013 6.586V4z" />
            </svg>
            Filter Events
            {selectedTypes.length > 0 && (
              <span className="bg-indigo-500 text-white text-xs rounded-full w-5 h-5 flex items-center justify-center">
                {selectedTypes.length}
              </span>
            )}
          </button>
          <button
            onClick={fetchData}
            className="px-3 py-2 text-sm bg-gray-800 border border-gray-700 text-gray-300 rounded-lg hover:bg-gray-700 transition-colors flex items-center gap-1.5"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </button>
        </div>
      </div>

      {loading ? (
        <div className="flex justify-center py-20">
          <LoadingSpinner size="lg" />
        </div>
      ) : !data || data.items.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-gray-500 text-lg">No history events found.</p>
        </div>
      ) : (
        <>
          <div className="card overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-700/50">
                    <th className="w-10 px-3 py-3"></th>
                    <th
                      className="text-left px-3 py-3 text-gray-400 font-medium cursor-pointer hover:text-gray-200 select-none"
                      onClick={() => handleSort('event_time')}
                    >
                      Date <SortIcon column="event_time" />
                    </th>
                    <th
                      className="text-left px-3 py-3 text-gray-400 font-medium cursor-pointer hover:text-gray-200 select-none"
                      onClick={() => handleSort('title')}
                    >
                      Title <SortIcon column="title" />
                    </th>
                    <th
                      className="text-left px-3 py-3 text-gray-400 font-medium cursor-pointer hover:text-gray-200 select-none"
                      onClick={() => handleSort('library_name')}
                    >
                      Library <SortIcon column="library_name" />
                    </th>
                    <th
                      className="text-left px-3 py-3 text-gray-400 font-medium cursor-pointer hover:text-gray-200 select-none"
                      onClick={() => handleSort('event_type')}
                    >
                      Event <SortIcon column="event_type" />
                    </th>
                    <th className="text-left px-3 py-3 text-gray-400 font-medium">User</th>
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((item) => (
                    <tr key={item.ID} className="border-b border-gray-700/30 hover:bg-gray-800/50 transition-colors">
                      <td className="px-3 py-3">
                        <button
                          onClick={() => handleShowDetail(item)}
                          className="text-gray-500 hover:text-indigo-400 transition-colors"
                          title="View media history"
                        >
                          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                          </svg>
                        </button>
                      </td>
                      <td className="px-3 py-3 text-gray-400 whitespace-nowrap" title={formatExactDate(item.EventTime)}>
                        {formatDate(item.EventTime)}
                      </td>
                      <td className="px-3 py-3">
                        <div className="text-gray-100">{item.Title}</div>
                        <div className="text-xs text-gray-500">{item.Year}</div>
                      </td>
                      <td className="px-3 py-3">
                        <span className={`px-2 py-0.5 text-xs font-medium rounded-full ${
                          item.MediaType === 'movie'
                            ? 'bg-blue-500/20 text-blue-400'
                            : 'bg-purple-500/20 text-purple-400'
                        }`}>
                          {item.LibraryName}
                        </span>
                      </td>
                      <td className="px-3 py-3">{getEventBadge(item.EventType)}</td>
                      <td className="px-3 py-3 text-gray-400">{item.Username || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          {/* Pagination */}
          {data.totalPages > 1 && (
            <div className="flex items-center justify-between mt-4 px-1">
              <div className="text-sm text-gray-500">
                Showing {((page - 1) * pageSize) + 1}–{Math.min(page * pageSize, data.total)} of {data.total}
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  disabled={page === 1}
                  className="px-3 py-1.5 text-sm bg-gray-800 border border-gray-700 text-gray-300 rounded-lg hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                >
                  Previous
                </button>
                <span className="text-sm text-gray-400">
                  Page {page} of {data.totalPages}
                </span>
                <button
                  onClick={() => setPage((p) => Math.min(data!.totalPages, p + 1))}
                  disabled={page === data.totalPages}
                  className="px-3 py-1.5 text-sm bg-gray-800 border border-gray-700 text-gray-300 rounded-lg hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </>
      )}

      {/* Filter Modal */}
      {showFilterModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div className="fixed inset-0 bg-black/60" onClick={() => setShowFilterModal(false)} />
          <div className="relative bg-gray-900 border border-gray-700 rounded-xl p-6 w-full max-w-md shadow-2xl">
            <h3 className="text-lg font-semibold text-gray-100 mb-4">Filter Event Types</h3>
            <div className="space-y-2 max-h-80 overflow-y-auto">
              {EVENT_TYPES.map((evt) => (
                <label key={evt.value} className="flex items-center gap-3 px-3 py-2 rounded-lg hover:bg-gray-800/50 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={selectedTypes.includes(evt.value)}
                    onChange={() => handleFilterToggle(evt.value)}
                    className="rounded border-gray-600 bg-gray-800 text-indigo-500 focus:ring-indigo-500"
                  />
                  <span className="text-sm text-gray-200">{evt.emoji} {evt.label}</span>
                </label>
              ))}
            </div>
            <div className="flex gap-2 mt-4">
              <button onClick={handleApplyFilters} className="btn-primary flex-1">Apply</button>
              <button
                onClick={() => { setSelectedTypes([]); setShowFilterModal(false); setPage(1) }}
                className="btn-secondary flex-1"
              >
                Clear All
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Detail Modal */}
      {detailItem && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div className="fixed inset-0 bg-black/60" onClick={() => setDetailItem(null)} />
          <div className="relative bg-gray-900 border border-gray-700 rounded-xl p-6 w-full max-w-lg shadow-2xl max-h-[80vh] flex flex-col">
            <div className="flex items-start justify-between mb-4">
              <div>
                <h3 className="text-lg font-semibold text-gray-100">{detailItem.Title}</h3>
                <p className="text-sm text-gray-400">{detailItem.Year} &middot; {detailItem.LibraryName}</p>
              </div>
              <button onClick={() => setDetailItem(null)} className="text-gray-500 hover:text-gray-300 transition-colors">
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>

            <div className="overflow-y-auto flex-1">
              {loadingDetail ? (
                <div className="flex justify-center py-8">
                  <LoadingSpinner />
                </div>
              ) : detailEvents.length === 0 ? (
                <p className="text-gray-500 text-center py-4">No events found.</p>
              ) : (
                <div className="space-y-3">
                  {detailEvents.map((evt) => (
                    <div key={evt.ID} className="flex items-start gap-3 px-3 py-2 rounded-lg bg-gray-800/50">
                      <div className="flex-shrink-0 mt-0.5">{getEventBadge(evt.EventType)}</div>
                      <div className="flex-1 min-w-0">
                        <div className="text-xs text-gray-400">{formatExactDate(evt.EventTime)}</div>
                        {evt.Username && <div className="text-xs text-gray-500 mt-0.5">by {evt.Username}</div>}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
