import { get, post } from './client'
import type { AuthConfig, MeResponse } from '@/types/api'

export function getMe(): Promise<MeResponse> {
  return get<MeResponse>('/api/me')
}

export function getAuthConfig(): Promise<AuthConfig> {
  return get<AuthConfig>('/api/auth/config')
}

export async function loginJellyfin(username: string, password: string): Promise<{ success: boolean; redirect?: string; error?: string }> {
  const formData = new FormData()
  formData.append('username', username)
  formData.append('password', password)

  const response = await fetch('/auth/jellyfin/login', {
    method: 'POST',
    body: formData,
  })

  return response.json()
}

export function logout(): Promise<void> {
  return fetch('/logout', { method: 'GET', redirect: 'manual' }).then(() => {
    window.location.href = '/login'
  })
}

export function getImageCacheUrl(id: string): string {
  return `/api/images/cache?id=${encodeURIComponent(id)}`
}

// Webpush
export function getVapidKey() {
  return get<{ publicKey: string }>('/api/webpush/vapid-key')
}

export function getWebPushStatus(endpoint?: string) {
  const qs = endpoint ? `?endpoint=${encodeURIComponent(endpoint)}` : ''
  return get<{ subscribed: boolean }>(`/api/webpush/status${qs}`)
}

export function subscribeWebPush(subscription: PushSubscription, username: string) {
  return post('/api/webpush/subscribe', { subscription: subscription.toJSON(), username })
}

export function unsubscribeWebPush(endpoint: string) {
  return post('/api/webpush/unsubscribe', { endpoint })
}
