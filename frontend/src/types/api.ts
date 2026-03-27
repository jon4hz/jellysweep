// API types matching the Go models in internal/api/models/models.go

export type MediaType = 'tv' | 'movie'

export interface User {
  id: number
  name: string
  username: string
  isAdmin: boolean
  email: string
  gravatarUrl: string
}

export interface MeResponse {
  username: string
  name: string
  isAdmin: boolean
  gravatarUrl: string
  pendingRequestsCount: number
  isDryRun: boolean
}

export interface AuthConfig {
  jellyfin: { enabled: boolean } | null
  oidc: { enabled: boolean; name: string } | null
}

export interface UserRequestInfo {
  ID: number
  Status: string
}

export interface UserMediaItem {
  ID: number
  Title: string
  Year: number
  MediaType: MediaType
  LibraryName: string
  FileSize: number
  DefaultDeleteAt: string
  Unkeepable: boolean
  CleanupMode?: string
  KeepCount?: number
  Request?: UserRequestInfo
}

export interface AdminRequestInfo {
  ID: number
  UserID: number
  Username: string
  Status: string
  CreatedAt: string
  UpdatedAt: string
}

export interface AdminMediaItem {
  ID: number
  JellyfinID: string
  LibraryName: string
  ArrID: number
  Title: string
  TmdbId?: number
  TvdbId?: number
  Year: number
  FileSize: number
  MediaType: MediaType
  RequestedBy: string
  DefaultDeleteAt: string
  ProtectedUntil?: string
  Unkeepable: boolean
  CleanupMode?: string
  KeepCount?: number
  Request?: AdminRequestInfo
}

export interface HistoryEventItem {
  ID: number
  MediaID: number
  JellyfinID: string
  ArrID: number
  MediaType: MediaType
  Title: string
  Year: number
  LibraryName: string
  EventType: string
  Username?: string
  EventTime: string
  CreatedAt: string
}

export interface HistoryResponse {
  items: HistoryEventItem[]
  total: number
  page: number
  pageSize: number
  totalPages: number
}

export interface SchedulerJob {
  id: string
  name: string
  description: string
  status: string
  schedule: string
  lastRun?: string
  nextRun?: string
  runCount: number
  errorCount: number
  lastError?: string
  enabled: boolean
  singleton: boolean
}

export interface CacheStats {
  name: string
  hits: number
  misses: number
  hitRate: number
}

export interface UserPermission {
  id: number
  username: string
  hasAutoApproval: boolean
  createdAt: string
}

// Standard API response wrappers
export interface ApiSuccess<T = undefined> {
  success: true
  message: string
  data?: T
}

export interface ApiError {
  success: false
  error: string
}

export type ApiResponse<T = undefined> = ApiSuccess<T> | ApiError

export interface KeepRequestResponse {
  success: boolean
  message: string
  autoApproved?: boolean
}

export interface MediaListResponse {
  success: boolean
  mediaItems: UserMediaItem[]
}

export interface AdminMediaListResponse {
  success: boolean
  mediaItems: AdminMediaItem[]
}

export interface AdminKeepRequestsResponse {
  success: boolean
  keepRequests: AdminMediaItem[]
}

export interface SchedulerJobsResponse {
  success: boolean
  jobs: Record<string, SchedulerJob>
}

export interface CacheStatsResponse {
  success: boolean
  stats: CacheStats[]
}

export interface UsersResponse {
  success: boolean
  users: UserPermission[]
}

export interface VapidKeyResponse {
  publicKey: string
}

export interface WebPushStatusResponse {
  subscribed: boolean
}
