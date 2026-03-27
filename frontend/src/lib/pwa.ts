// PWA install prompt + service worker registration

let deferredPrompt: BeforeInstallPromptEvent | null = null
let swRegistration: ServiceWorkerRegistration | null = null

interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

export function isPwaInstalled(): boolean {
  return window.matchMedia('(display-mode: standalone)').matches ||
    (navigator as any).standalone === true
}

export function canInstallPwa(): boolean {
  return deferredPrompt !== null
}

export async function installPwa(): Promise<boolean> {
  if (!deferredPrompt) return false
  deferredPrompt.prompt()
  const result = await deferredPrompt.userChoice
  deferredPrompt = null
  return result.outcome === 'accepted'
}

export function getServiceWorkerRegistration(): ServiceWorkerRegistration | null {
  return swRegistration
}

export async function initPwa(): Promise<void> {
  // Listen for install prompt
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    deferredPrompt = e as BeforeInstallPromptEvent
  })

  // Register service worker
  if ('serviceWorker' in navigator) {
    try {
      swRegistration = await navigator.serviceWorker.register('/static/sw.js')

      // Handle updates
      swRegistration.addEventListener('updatefound', () => {
        const newWorker = swRegistration!.installing
        if (newWorker) {
          newWorker.addEventListener('statechange', () => {
            if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
              // New version available, could show update toast
              console.log('New service worker installed, refresh to update')
            }
          })
        }
      })
    } catch (err) {
      console.error('Service Worker registration failed:', err)
      swRegistration = await navigator.serviceWorker.getRegistration() ?? null
    }
  }
}
