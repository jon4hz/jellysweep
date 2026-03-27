import { useState, useCallback } from 'react'
import type { AdminMediaItem } from '@/types/api'
import { formatFileSize, timeUntil, getEpisodeTooltip, getSeasonTooltip } from '@/lib/utils'
import { markMediaAsKeep, markMediaAsDelete, markMediaAsKeepForever } from '@/api/admin'
import { showToast } from '@/components/ui'

type RemoveAnimation = 'swipe-right' | 'swipe-left' | 'fly-up' | null

interface Props {
  item: AdminMediaItem
  onRemove: (id: number) => void
}

export function AdminMediaCard({ item, onRemove }: Props) {
  const [loading, setLoading] = useState<string | null>(null)
  const [animation, setAnimation] = useState<RemoveAnimation>(null)

  const handleAction = useCallback(async (
    action: 'keep' | 'delete' | 'keep-forever',
    apiFn: (id: number) => Promise<{ success: boolean; message: string }>,
    anim: RemoveAnimation,
  ) => {
    if (loading) return
    setLoading(action)
    try {
      const data = await apiFn(item.ID)
      if (data.success) {
        showToast(data.message, 'success')
        setAnimation(anim)
        setTimeout(() => onRemove(item.ID), 300)
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Action failed', 'error')
    } finally {
      setLoading(null)
    }
  }, [item.ID, loading, onRemove])

  const statusBadge = item.Request?.Status === 'pending' ? (
    <span className="px-2 py-0.5 text-xs font-medium bg-yellow-500/20 text-yellow-400 rounded-full">Keep Requested</span>
  ) : item.Unkeepable ? (
    <span className="px-2 py-0.5 text-xs font-medium bg-red-500/20 text-red-400 rounded-full">Must Delete</span>
  ) : null

  const animClass = animation === 'swipe-right' ? 'translate-x-full opacity-0' :
    animation === 'swipe-left' ? '-translate-x-full opacity-0' :
    animation === 'fly-up' ? '-translate-y-full opacity-0' : ''

  return (
    <div className={`media-card transition-all duration-300 ${animClass}`}>
      {/* Poster */}
      <div className="relative aspect-[2/3] bg-gray-800 rounded-t-xl overflow-hidden">
        <img
          src={`/api/images/cache?id=${item.JellyfinID}`}
          alt={item.Title}
          className="w-full h-full object-cover"
          loading="lazy"
          onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
        />
        {/* Badges */}
        <div className="absolute top-2 left-2 flex flex-col gap-1">
          <span className={`px-2 py-0.5 text-xs font-medium rounded-full ${
            item.MediaType === 'movie'
              ? 'bg-blue-500/80 text-white'
              : 'bg-purple-500/80 text-white'
          }`}>
            {item.MediaType === 'movie' ? 'Movie' : 'TV'}
          </span>
          {item.CleanupMode && (
            <span
              className="px-2 py-0.5 text-xs font-medium bg-gray-900/80 text-gray-300 rounded-full cursor-help"
              title={item.CleanupMode === 'keep_episodes'
                ? getEpisodeTooltip(item.KeepCount ?? 0)
                : getSeasonTooltip(item.KeepCount ?? 0)}
            >
              {item.CleanupMode === 'keep_episodes'
                ? `Keep ${item.KeepCount} Eps`
                : `Keep ${item.KeepCount} Seasons`}
            </span>
          )}
        </div>
      </div>

      {/* Info */}
      <div className="p-4 flex-1 flex flex-col">
        <h3 className="font-semibold text-gray-100 text-sm line-clamp-2">{item.Title}</h3>
        <div className="flex items-center gap-2 mt-1 text-xs text-gray-400 flex-wrap">
          <span>{item.Year}</span>
          <span>&middot;</span>
          <span>{item.LibraryName}</span>
          <span>&middot;</span>
          <span>{formatFileSize(item.FileSize)}</span>
        </div>

        {statusBadge && <div className="mt-2">{statusBadge}</div>}

        {item.RequestedBy && (
          <div className="mt-1 text-xs text-gray-500">
            Requested by <span className="text-gray-300">{item.RequestedBy}</span>
          </div>
        )}

        <div className="mt-auto pt-3 text-xs text-gray-500">
          Deletes {timeUntil(item.DefaultDeleteAt)}
        </div>

        {/* Action buttons */}
        <div className="flex gap-2 mt-3">
          <button
            onClick={() => handleAction('keep', markMediaAsKeep, 'swipe-right')}
            disabled={loading !== null}
            className="flex-1 px-2 py-1.5 text-xs font-medium bg-green-600/20 text-green-400 rounded-lg hover:bg-green-600/30 disabled:opacity-50 transition-colors flex items-center justify-center gap-1"
          >
            {loading === 'keep' ? (
              <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-green-400" />
            ) : (
              <svg className="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z"/></svg>
            )}
            Keep
          </button>
          <button
            onClick={() => handleAction('keep-forever', markMediaAsKeepForever, 'fly-up')}
            disabled={loading !== null}
            className="px-2 py-1.5 text-xs font-medium bg-indigo-600/20 text-indigo-400 rounded-lg hover:bg-indigo-600/30 disabled:opacity-50 transition-colors flex items-center justify-center gap-1"
            title="Keep Forever — adds jellysweep-ignore tag"
          >
            {loading === 'keep-forever' ? (
              <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-indigo-400" />
            ) : (
              <svg className="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><path d="M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z"/></svg>
            )}
          </button>
          <button
            onClick={() => handleAction('delete', markMediaAsDelete, 'swipe-left')}
            disabled={loading !== null}
            className="flex-1 px-2 py-1.5 text-xs font-medium bg-red-600/20 text-red-400 rounded-lg hover:bg-red-600/30 disabled:opacity-50 transition-colors flex items-center justify-center gap-1"
          >
            {loading === 'delete' ? (
              <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-red-400" />
            ) : (
              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
            )}
            Sweep
          </button>
        </div>
      </div>
    </div>
  )
}
