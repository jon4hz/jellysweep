// Web Push notification management
import { getVapidKey, getWebPushStatus, subscribeWebPush, unsubscribeWebPush } from '@/api/auth'
import { getServiceWorkerRegistration } from './pwa'

export function isPushSupported(): boolean {
  return 'serviceWorker' in navigator && 'PushManager' in window
}

export async function isPushSubscribed(): Promise<boolean> {
  const reg = getServiceWorkerRegistration()
  if (!reg) return false
  const sub = await reg.pushManager.getSubscription()
  if (!sub) return false
  try {
    const status = await getWebPushStatus(sub.endpoint)
    return status.subscribed
  } catch {
    return false
  }
}

export async function subscribePush(username: string): Promise<boolean> {
  const reg = getServiceWorkerRegistration()
  if (!reg) return false

  try {
    const { publicKey } = await getVapidKey()
    const sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(publicKey),
    })
    await subscribeWebPush(sub, username)
    return true
  } catch (err) {
    console.error('Failed to subscribe to push:', err)
    return false
  }
}

export async function unsubscribePush(): Promise<boolean> {
  const reg = getServiceWorkerRegistration()
  if (!reg) return false

  try {
    const sub = await reg.pushManager.getSubscription()
    if (sub) {
      await unsubscribeWebPush(sub.endpoint)
      await sub.unsubscribe()
    }
    return true
  } catch (err) {
    console.error('Failed to unsubscribe from push:', err)
    return false
  }
}

function urlBase64ToUint8Array(base64String: string): BufferSource {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const rawData = window.atob(base64)
  const outputArray = new Uint8Array(rawData.length)
  for (let i = 0; i < rawData.length; ++i) {
    outputArray[i] = rawData.charCodeAt(i)
  }
  return outputArray
}
