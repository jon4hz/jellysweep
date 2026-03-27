import { useState, useEffect, useCallback, useMemo } from 'react'
import type { SchedulerJob, CacheStats } from '@/types/api'
import {
  getSchedulerJobs, runSchedulerJob, enableSchedulerJob, disableSchedulerJob,
  getCacheStats, clearCache,
} from '@/api/admin'
import { formatDate } from '@/lib/utils'
import { LoadingSpinner, showToast } from '@/components/ui'

const STATUS_STYLES: Record<string, string> = {
  running: 'bg-blue-500/20 text-blue-400',
  completed: 'bg-green-500/20 text-green-400',
  failed: 'bg-red-500/20 text-red-400',
  scheduled: 'bg-gray-500/20 text-gray-400',
}

export default function SchedulerPage() {
  const [jobs, setJobs] = useState<Record<string, SchedulerJob>>({})
  const [cacheStats, setCacheStats] = useState<CacheStats[]>([])
  const [loading, setLoading] = useState(true)
  const [actionLoading, setActionLoading] = useState<string | null>(null)
  const [clearingCache, setClearingCache] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const [jobsData, statsData] = await Promise.all([
        getSchedulerJobs(),
        getCacheStats(),
      ])
      setJobs(jobsData.jobs ?? {})
      setCacheStats(statsData.stats ?? [])
    } catch {
      // handled by client
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchAll() }, [fetchAll])

  const handleRunJob = useCallback(async (jobId: string) => {
    setActionLoading(`run-${jobId}`)
    try {
      const data = await runSchedulerJob(jobId)
      if (data.success) {
        showToast(data.message, 'success')
        // Refresh after a short delay to get updated status
        setTimeout(fetchAll, 1000)
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to run job', 'error')
    } finally {
      setActionLoading(null)
    }
  }, [fetchAll])

  const handleToggleJob = useCallback(async (jobId: string, enabled: boolean) => {
    setActionLoading(`toggle-${jobId}`)
    try {
      const data = enabled
        ? await disableSchedulerJob(jobId)
        : await enableSchedulerJob(jobId)
      if (data.success) {
        showToast(data.message, 'success')
        setJobs((prev) => ({
          ...prev,
          [jobId]: { ...prev[jobId]!, enabled: !enabled },
        }))
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to toggle job', 'error')
    } finally {
      setActionLoading(null)
    }
  }, [])

  const handleClearCache = useCallback(async () => {
    setClearingCache(true)
    try {
      const data = await clearCache()
      if (data.success) {
        showToast(data.message, 'success')
        const statsData = await getCacheStats()
        setCacheStats(statsData.stats ?? [])
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to clear cache', 'error')
    } finally {
      setClearingCache(false)
    }
  }, [])

  const jobList = useMemo(() => Object.entries(jobs).sort(([a], [b]) => a.localeCompare(b)), [jobs])

  const cacheOverview = useMemo(() => {
    const totalHits = cacheStats.reduce((acc, s) => acc + s.hits, 0)
    const totalMisses = cacheStats.reduce((acc, s) => acc + s.misses, 0)
    const hitRate = totalHits + totalMisses > 0 ? (totalHits / (totalHits + totalMisses) * 100) : 0
    return { active: cacheStats.length, totalHits, totalMisses, hitRate }
  }, [cacheStats])

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <LoadingSpinner size="lg" />
      </div>
    )
  }

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-8 flex-wrap gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold text-gray-100">Scheduler Management</h1>
          <p className="mt-1 text-gray-400">Monitor and manage background jobs and caches.</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={fetchAll}
            className="px-3 py-2 text-sm bg-gray-800 border border-gray-700 text-gray-300 rounded-lg hover:bg-gray-700 transition-colors flex items-center gap-1.5"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </button>
          <button
            onClick={handleClearCache}
            disabled={clearingCache}
            className="px-3 py-2 text-sm bg-red-600/20 border border-red-500/30 text-red-400 rounded-lg hover:bg-red-600/30 disabled:opacity-50 transition-colors flex items-center gap-1.5"
          >
            {clearingCache ? (
              <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-red-400" />
            ) : (
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
            )}
            Clear Cache
          </button>
        </div>
      </div>

      {/* Cache Stats */}
      <div className="card p-5 mb-8">
        <h2 className="text-lg font-semibold text-gray-100 mb-4">Cache Performance</h2>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
          <div className="text-center">
            <div className="text-2xl font-bold text-indigo-400">{cacheOverview.active}</div>
            <div className="text-xs text-gray-500 mt-1">Active Caches</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-bold text-green-400">{cacheOverview.totalHits.toLocaleString()}</div>
            <div className="text-xs text-gray-500 mt-1">Total Hits</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-bold text-red-400">{cacheOverview.totalMisses.toLocaleString()}</div>
            <div className="text-xs text-gray-500 mt-1">Total Misses</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-bold text-purple-400">{cacheOverview.hitRate.toFixed(1)}%</div>
            <div className="text-xs text-gray-500 mt-1">Hit Rate</div>
          </div>
        </div>
      </div>

      {/* Jobs Table */}
      <div className="card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-700/50">
                <th className="text-left px-4 py-3 text-gray-400 font-medium">Job</th>
                <th className="text-left px-4 py-3 text-gray-400 font-medium">Status</th>
                <th className="text-left px-4 py-3 text-gray-400 font-medium">Schedule</th>
                <th className="text-left px-4 py-3 text-gray-400 font-medium hidden sm:table-cell">Last Run</th>
                <th className="text-left px-4 py-3 text-gray-400 font-medium hidden md:table-cell">Next Run</th>
                <th className="text-left px-4 py-3 text-gray-400 font-medium hidden lg:table-cell">Stats</th>
                <th className="text-right px-4 py-3 text-gray-400 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {jobList.map(([id, job]) => (
                <tr key={id} className="border-b border-gray-700/30 hover:bg-gray-800/50 transition-colors">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <span className={`w-2 h-2 rounded-full flex-shrink-0 ${job.enabled ? 'bg-green-400' : 'bg-gray-600'}`} />
                      <div>
                        <div className="text-gray-100 font-medium">{job.name}</div>
                        {job.description && <div className="text-xs text-gray-500 mt-0.5">{job.description}</div>}
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <span className={`px-2 py-0.5 text-xs font-medium rounded-full ${STATUS_STYLES[job.status] ?? STATUS_STYLES.scheduled}`}>
                      {job.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-gray-400 font-mono text-xs">{job.schedule}</td>
                  <td className="px-4 py-3 text-gray-400 text-xs hidden sm:table-cell">
                    {job.lastRun ? formatDate(job.lastRun) : '—'}
                  </td>
                  <td className="px-4 py-3 text-gray-400 text-xs hidden md:table-cell">
                    {job.enabled && job.nextRun ? formatDate(job.nextRun) : '—'}
                  </td>
                  <td className="px-4 py-3 hidden lg:table-cell">
                    <div className="flex items-center gap-3 text-xs">
                      <span className="text-gray-400">{job.runCount} runs</span>
                      {job.errorCount > 0 && (
                        <span className="text-red-400">{job.errorCount} errors</span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-2">
                      <button
                        onClick={() => handleRunJob(id)}
                        disabled={actionLoading !== null || job.status === 'running'}
                        className="px-2 py-1 text-xs bg-indigo-600/20 text-indigo-400 rounded-lg hover:bg-indigo-600/30 disabled:opacity-50 transition-colors"
                        title="Run Now"
                      >
                        {actionLoading === `run-${id}` ? (
                          <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-indigo-400" />
                        ) : (
                          <svg className="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
                        )}
                      </button>
                      <button
                        onClick={() => handleToggleJob(id, job.enabled)}
                        disabled={actionLoading !== null}
                        className={`px-2 py-1 text-xs rounded-lg disabled:opacity-50 transition-colors ${
                          job.enabled
                            ? 'bg-red-600/20 text-red-400 hover:bg-red-600/30'
                            : 'bg-green-600/20 text-green-400 hover:bg-green-600/30'
                        }`}
                        title={job.enabled ? 'Disable' : 'Enable'}
                      >
                        {actionLoading === `toggle-${id}` ? (
                          <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-current" />
                        ) : job.enabled ? (
                          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
                        ) : (
                          <svg className="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
                        )}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
