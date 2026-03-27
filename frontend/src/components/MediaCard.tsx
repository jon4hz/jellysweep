import { useState, useCallback } from 'react'
import type { UserMediaItem } from '@/types/api'
import { formatFileSize, formatDate, formatExactDate, timeUntil, getEpisodeTooltip, getSeasonTooltip } from '@/lib/utils'
import { requestKeepMedia } from '@/api/media'
import { showToast } from '@/components/ui'

interface Props {
  item: UserMediaItem
  onRemove: (id: number) => void
}

export function MediaCard({ item, onRemove }: Props) {
  const [loading, setLoading] = useState(false)
  const [status, setStatus] = useState<'idle' | 'submitted' | 'protected'>(
    item.Request?.Status === 'approved' ? 'protected' :
    item.Request?.ID ? 'submitted' : 'idle'
  )

  const handleRequestKeep = useCallback(async () => {
    if (loading || status !== 'idle' || item.Unkeepable) return
    setLoading(true)
    try {
      const data = await requestKeepMedia(item.ID)
      if (data.success) {
        showToast(data.message, 'success')
        if (data.autoApproved) {
          setStatus('protected')
          setTimeout(() => onRemove(item.ID), 300)
        } else {
          setStatus('submitted')
        }
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to submit request', 'error')
    } finally {
      setLoading(false)
    }
  }, [item.ID, item.Unkeepable, loading, status, onRemove])

  const deletionDate = item.DefaultDeleteAt
  const isOverdue = new Date(deletionDate) < new Date()

  return (
    <div
      className={`media-card overflow-hidden transition-all duration-300 ${status === 'protected' ? 'opacity-0 translate-x-5' : ''}`}
    >
      {/* Poster */}
      <div className="aspect-[2/3] bg-gray-800 overflow-hidden rounded-t-lg relative">
        <img
          src={`/api/images/cache?id=${item.ID}`}
          alt={item.Title}
          className="w-full h-full object-cover"
          loading="lazy"
        />
        {/* Media type badge */}
        <div className="absolute top-2 left-2">
          <span className="px-2 py-0.5 rounded text-xs font-medium bg-gray-900/80 text-gray-200 backdrop-blur-sm">
            {item.MediaType === 'tv' ? 'TV' : 'Movie'}
          </span>
        </div>
        {/* Cleanup mode badge */}
        {item.CleanupMode && item.CleanupMode !== 'all' && item.MediaType === 'tv' && (
          <div className="absolute top-2 right-2 group">
            <span className="px-2 py-0.5 rounded text-xs font-medium bg-indigo-900/80 text-indigo-200 backdrop-blur-sm cursor-help">
              {item.CleanupMode === 'keep_episodes'
                ? `Keep ${item.KeepCount ?? 0} Episode${(item.KeepCount ?? 0) !== 1 ? 's' : ''}`
                : `Keep ${item.KeepCount ?? 0} Season${(item.KeepCount ?? 0) !== 1 ? 's' : ''}`}
            </span>
            <div className="absolute right-0 top-full mt-1 w-64 p-2 bg-gray-800 border border-gray-700 rounded-lg shadow-xl opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all duration-200 z-10 text-xs text-gray-300">
              {item.CleanupMode === 'keep_episodes'
                ? getEpisodeTooltip(item.KeepCount ?? 0)
                : getSeasonTooltip(item.KeepCount ?? 0)}
            </div>
          </div>
        )}
      </div>

      {/* Content */}
      <div className="p-4 space-y-3">
        <div>
          <h3 className="font-semibold text-gray-100 truncate">{item.Title}</h3>
          <p className="text-sm text-gray-400">
            {item.Year > 0 && `${item.Year} · `}{item.LibraryName}
          </p>
        </div>

        <div className="flex items-center justify-between text-xs text-gray-400">
          <span>{formatFileSize(item.FileSize)}</span>
          <span className="group relative cursor-help">
            <span className={isOverdue ? 'text-red-400 font-medium' : ''}>
              {isOverdue ? 'Overdue' : timeUntil(deletionDate)}
            </span>
            <span className="absolute bottom-full right-0 mb-1 w-48 p-1.5 bg-gray-800 border border-gray-700 rounded text-xs text-gray-300 opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all z-10">
              {formatExactDate(deletionDate)}
            </span>
          </span>
        </div>

        <div className="text-xs text-gray-500">
          Deletion: {formatDate(deletionDate)}
        </div>

        {/* Action button */}
        {item.Unkeepable ? (
          <button disabled className="btn-secondary w-full text-sm opacity-50 cursor-not-allowed">
            Request Unavailable
          </button>
        ) : status === 'protected' ? (
          <button disabled className="btn-secondary w-full text-sm opacity-50 cursor-not-allowed">
            <svg className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
            Protected
          </button>
        ) : status === 'submitted' ? (
          <button disabled className="btn-secondary w-full text-sm opacity-50 cursor-not-allowed">
            <svg className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
            Request Submitted
          </button>
        ) : (
          <button
            onClick={handleRequestKeep}
            disabled={loading}
            className="btn-primary w-full text-sm"
          >
            {loading ? (
              <>
                <svg className="w-4 h-4 mr-2 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
                Submitting...
              </>
            ) : (
              'Request to Keep'
            )}
          </button>
        )}
      </div>
    </div>
  )
}
