import { get, post, put } from './client'
import type {
  AdminKeepRequestsResponse,
  AdminMediaListResponse,
  ApiSuccess,
  SchedulerJobsResponse,
  CacheStatsResponse,
  HistoryResponse,
  UsersResponse,
} from '@/types/api'

// Keep requests
export function getKeepRequests(): Promise<AdminKeepRequestsResponse> {
  return get<AdminKeepRequestsResponse>('/admin/api/keep-requests')
}

export function acceptKeepRequest(mediaId: number): Promise<ApiSuccess> {
  return post<ApiSuccess>(`/admin/api/keep-requests/${mediaId}/accept`)
}

export function declineKeepRequest(mediaId: number): Promise<ApiSuccess> {
  return post<ApiSuccess>(`/admin/api/keep-requests/${mediaId}/decline`)
}

// Media management
export function getAdminMediaItems(): Promise<AdminMediaListResponse> {
  return get<AdminMediaListResponse>('/admin/api/media')
}

export function markMediaAsKeep(mediaId: number): Promise<ApiSuccess> {
  return post<ApiSuccess>(`/admin/api/media/${mediaId}/keep`)
}

export function markMediaAsDelete(mediaId: number): Promise<ApiSuccess> {
  return post<ApiSuccess>(`/admin/api/media/${mediaId}/delete`)
}

export function markMediaAsKeepForever(mediaId: number): Promise<ApiSuccess> {
  return post<ApiSuccess>(`/admin/api/media/${mediaId}/keep-forever`)
}

// Scheduler
export function getSchedulerJobs(): Promise<SchedulerJobsResponse> {
  return get<SchedulerJobsResponse>('/admin/api/scheduler/jobs')
}

export function runSchedulerJob(jobId: string): Promise<ApiSuccess> {
  return post<ApiSuccess>(`/admin/api/scheduler/jobs/${encodeURIComponent(jobId)}/run`)
}

export function enableSchedulerJob(jobId: string): Promise<ApiSuccess> {
  return post<ApiSuccess>(`/admin/api/scheduler/jobs/${encodeURIComponent(jobId)}/enable`)
}

export function disableSchedulerJob(jobId: string): Promise<ApiSuccess> {
  return post<ApiSuccess>(`/admin/api/scheduler/jobs/${encodeURIComponent(jobId)}/disable`)
}

export function getCacheStats(): Promise<CacheStatsResponse> {
  return get<CacheStatsResponse>('/admin/api/scheduler/cache/stats')
}

export function clearCache(): Promise<ApiSuccess> {
  return post<ApiSuccess>('/admin/api/scheduler/cache/clear')
}

// History
export function getHistory(params: {
  page?: number
  pageSize?: number
  sortBy?: string
  sortOrder?: string
  includeEventTypes?: string[]
  jellyfinId?: string
}): Promise<HistoryResponse> {
  const qs = new URLSearchParams()
  if (params.page) qs.set('page', String(params.page))
  if (params.pageSize) qs.set('pageSize', String(params.pageSize))
  if (params.sortBy) qs.set('sortBy', params.sortBy)
  if (params.sortOrder) qs.set('sortOrder', params.sortOrder)
  if (params.includeEventTypes?.length) qs.set('includeEventTypes', params.includeEventTypes.join(','))
  if (params.jellyfinId) qs.set('jellyfinId', params.jellyfinId)
  return get<HistoryResponse>(`/admin/api/history?${qs.toString()}`)
}

// Users
export function getUsers(): Promise<UsersResponse> {
  return get<UsersResponse>('/admin/api/users')
}

export function updateUserPermissions(userId: number, hasAutoApproval: boolean): Promise<ApiSuccess> {
  return put<ApiSuccess>(`/admin/api/users/${userId}/permissions`, { hasAutoApproval })
}
