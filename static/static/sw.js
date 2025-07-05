const CACHE_NAME = "jellysweep-v1";
const STATIC_ASSETS = [
  "/",
  "/static/dist/style.css",
  "/static/dist/chart.js",
  "/static/jellysweep.png",
  "/static/manifest.json",
];

// TODO: also cache images

// Install event - cache static assets
self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => {
        console.log("Caching static assets");
        return cache.addAll(STATIC_ASSETS);
      })
      .then(() => {
        // Force the waiting service worker to become the active service worker
        return self.skipWaiting();
      })
  );
});

// Activate event - clean up old caches
self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((cacheNames) => {
        return Promise.all(
          cacheNames.map((cacheName) => {
            if (cacheName !== CACHE_NAME) {
              console.log("Deleting old cache:", cacheName);
              return caches.delete(cacheName);
            }
          })
        );
      })
      .then(() => {
        // Take control of all clients immediately
        return self.clients.claim();
      })
  );
});

// Fetch event - serve from cache with network fallback
self.addEventListener("fetch", (event) => {
  // Skip non-GET requests
  if (event.request.method !== "GET") {
    return;
  }

  // Skip requests to external domains
  if (!event.request.url.startsWith(self.location.origin)) {
    return;
  }

  event.respondWith(
    caches.match(event.request).then((cachedResponse) => {
      // Return cached version if available
      if (cachedResponse) {
        return cachedResponse;
      }

      // Otherwise fetch from network
      return fetch(event.request)
        .then((response) => {
          // Don't cache non-successful responses
          if (
            !response ||
            response.status !== 200 ||
            response.type !== "basic"
          ) {
            return response;
          }

          // Clone the response as it can only be consumed once
          const responseToCache = response.clone();

          // Cache static assets and pages
          if (
            event.request.url.includes("/static/") ||
            event.request.url === self.location.origin + "/" ||
            event.request.url.includes("/admin") ||
            event.request.url.includes("/login")
          ) {
            caches.open(CACHE_NAME).then((cache) => {
              cache.put(event.request, responseToCache);
            });
          }

          return response;
        })
        .catch(() => {
          // If network fails, try to serve a cached fallback page
          if (event.request.headers.get("accept").includes("text/html")) {
            return caches.match("/");
          }
        });
    })
  );
});

// Handle background sync for offline actions
self.addEventListener("sync", (event) => {
  if (event.tag === "background-sync") {
    event.waitUntil(doBackgroundSync());
  }
});

// Handle push notifications
self.addEventListener("push", (event) => {
  console.log("Push notification received");

  if (!event.data) {
    console.log("Push event but no data");
    return;
  }

  let notificationData;
  try {
    notificationData = event.data.json();
  } catch (e) {
    console.error("Failed to parse push data:", e);
    return;
  }

  const options = {
    body: notificationData.body || "New notification from JellySweep",
    icon: notificationData.icon || "/static/jellysweep.png",
    badge: notificationData.badge || "/static/jellysweep.png",
    tag: "jellysweep-notification",
    data: notificationData.data || {},
    actions: notificationData.actions || [
      {
        action: "open",
        title: "Open JellySweep",
      },
    ],
    requireInteraction: true,
    vibrate: [100, 50, 100],
  };

  event.waitUntil(
    self.registration.showNotification(
      notificationData.title || "JellySweep",
      options
    )
  );
});

// Handle notification clicks
self.addEventListener("notificationclick", (event) => {
  console.log("Notification clicked:", event);

  event.notification.close();

  const action = event.action;
  const data = event.notification.data;

  let url = "/";

  // Handle different notification types
  if (data && data.type === "keep_request_decision") {
    url = "/"; // Could be more specific based on the type
  }

  if (action === "open" || !action) {
    event.waitUntil(
      clients
        .matchAll({ type: "window", includeUncontrolled: true })
        .then((clients) => {
          // Check if there's already a window/tab open with the target URL
          for (const client of clients) {
            if (
              client.url === self.location.origin + url &&
              "focus" in client
            ) {
              return client.focus();
            }
          }

          // If no existing window, open a new one
          if (clients.openWindow) {
            return clients.openWindow(url);
          }
        })
    );
  }
});

function doBackgroundSync() {
  // Implement any background sync logic here
  console.log("Background sync triggered");
  return Promise.resolve();
}
