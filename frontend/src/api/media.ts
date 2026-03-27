import { get, post } from './client'
import type {
  MediaListResponse,
  KeepRequestResponse,
} from '@/types/api'

export function getMediaItems(refresh = false): Promise<MediaListResponse> {
  const qs = refresh ? '?refresh=true' : ''
  return get<MediaListResponse>(`/api/media${qs}`)
}

export function requestKeepMedia(mediaId: number): Promise<KeepRequestResponse> {
  return post<KeepRequestResponse>(`/api/media/${mediaId}/request-keep`)
}
