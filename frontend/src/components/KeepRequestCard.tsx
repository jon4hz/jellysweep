import { useState, useCallback } from 'react'
import type { AdminMediaItem } from '@/types/api'
import { timeUntil, getEpisodeTooltip, getSeasonTooltip } from '@/lib/utils'
import { acceptKeepRequest, declineKeepRequest } from '@/api/admin'
import { showToast } from '@/components/ui'

interface Props {
  item: AdminMediaItem
  onRemove: (id: number) => void
}

export function KeepRequestCard({ item, onRemove }: Props) {
  const [loading, setLoading] = useState<string | null>(null)
  const [animation, setAnimation] = useState<'swipe-right' | 'swipe-left' | null>(null)

  const handleAction = useCallback(async (
    action: 'accept' | 'decline',
    apiFn: (id: number) => Promise<{ success: boolean; message: string }>,
  ) => {
    if (loading || !item.Request?.ID) return
    setLoading(action)
    try {
      const data = await apiFn(item.Request.ID)
      if (data.success) {
        showToast(data.message, 'success')
        setAnimation(action === 'accept' ? 'swipe-right' : 'swipe-left')
        setTimeout(() => onRemove(item.ID), 300)
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Action failed', 'error')
    } finally {
      setLoading(null)
    }
  }, [item.ID, item.Request?.ID, loading, onRemove])

  const animClass = animation === 'swipe-right' ? 'translate-x-full opacity-0' :
    animation === 'swipe-left' ? '-translate-x-full opacity-0' : ''

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
        <div className="flex items-center gap-2 mt-1 text-xs text-gray-400">
          <span>{item.Year}</span>
          <span>&middot;</span>
          <span>{item.LibraryName}</span>
        </div>

        {item.Request?.Username && (
          <div className="mt-2 text-xs text-gray-500">
            Requested by <span className="text-gray-300">{item.Request.Username}</span>
          </div>
        )}

        <div className="mt-auto pt-3 text-xs text-gray-500">
          Deletes {timeUntil(item.DefaultDeleteAt)}
        </div>

        {/* Accept / Decline */}
        <div className="flex gap-2 mt-3">
          <button
            onClick={() => handleAction('accept', acceptKeepRequest)}
            disabled={loading !== null}
            className="flex-1 px-3 py-1.5 text-xs font-medium bg-green-600/20 text-green-400 rounded-lg hover:bg-green-600/30 disabled:opacity-50 transition-colors flex items-center justify-center gap-1"
          >
            {loading === 'accept' ? (
              <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-green-400" />
            ) : (
              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7"/></svg>
            )}
            Accept
          </button>
          <button
            onClick={() => handleAction('decline', declineKeepRequest)}
            disabled={loading !== null}
            className="flex-1 px-3 py-1.5 text-xs font-medium bg-red-600/20 text-red-400 rounded-lg hover:bg-red-600/30 disabled:opacity-50 transition-colors flex items-center justify-center gap-1"
          >
            {loading === 'decline' ? (
              <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-red-400" />
            ) : (
              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12"/></svg>
            )}
            Decline
          </button>
        </div>
      </div>
    </div>
  )
}
