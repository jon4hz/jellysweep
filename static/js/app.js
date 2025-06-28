// Jellysweep Frontend JavaScript
document.addEventListener("DOMContentLoaded", function () {
  // Initialize search and filtering
  initializeFilters();

  // Initialize media request functionality
  initializeMediaRequests();

  // Initialize auto-refresh
  initializeAutoRefresh();
});

function initializeFilters() {
  const searchInput = document.getElementById("search");
  const libraryFilter = document.getElementById("library-filter");
  const sortBy = document.getElementById("sort-by");
  const refreshBtn = document.getElementById("refresh-btn");

  if (searchInput) {
    searchInput.addEventListener("input", debounce(filterMedia, 300));
  }

  if (libraryFilter) {
    libraryFilter.addEventListener("change", filterMedia);
  }

  if (sortBy) {
    sortBy.addEventListener("change", sortMedia);
  }

  if (refreshBtn) {
    refreshBtn.addEventListener("click", refreshMedia);
  }
}

function initializeMediaRequests() {
  // Add click handlers for all request buttons
  document.addEventListener("click", function (e) {
    if (e.target.matches("button[data-media-id]")) {
      const mediaId = e.target.getAttribute("data-media-id");
      requestKeepMedia(mediaId, e.target);
    }
  });
}

function initializeAutoRefresh() {
  // Refresh every 5 minutes
  setInterval(refreshMedia, 5 * 60 * 1000);
}

function filterMedia() {
  const searchTerm =
    document.getElementById("search")?.value.toLowerCase() || "";
  const libraryFilter = document.getElementById("library-filter")?.value || "";

  const mediaCards = document.querySelectorAll("#media-grid > div");

  mediaCards.forEach((card) => {
    const title = (card.getAttribute("data-title") || "").toLowerCase();
    const library = card.getAttribute("data-library") || "";

    const matchesSearch = title.includes(searchTerm);
    const matchesLibrary = !libraryFilter || library === libraryFilter;

    if (matchesSearch && matchesLibrary) {
      card.style.display = "block";
    } else {
      card.style.display = "none";
    }
  });
}

function sortMedia() {
  const sortBy =
    document.getElementById("sort-by")?.value || "deletion-date-asc";
  const mediaGrid = document.getElementById("media-grid");

  if (!mediaGrid) return;

  const cards = Array.from(mediaGrid.children);

  cards.sort((a, b) => {
    let aValue, bValue;

    switch (sortBy) {
      case "title-asc":
        aValue = a.getAttribute("data-title") || "";
        bValue = b.getAttribute("data-title") || "";
        return aValue.localeCompare(bValue);

      case "title-desc":
        aValue = a.getAttribute("data-title") || "";
        bValue = b.getAttribute("data-title") || "";
        return bValue.localeCompare(aValue);

      case "deletion-date-asc":
        aValue = parseInt(a.getAttribute("data-deletion-timestamp")) || 0;
        bValue = parseInt(b.getAttribute("data-deletion-timestamp")) || 0;
        return aValue - bValue; // Earlier dates first

      case "deletion-date-desc":
        aValue = parseInt(a.getAttribute("data-deletion-timestamp")) || 0;
        bValue = parseInt(b.getAttribute("data-deletion-timestamp")) || 0;
        return bValue - aValue; // Later dates first

      default:
        return 0;
    }
  });

  // Re-append sorted cards
  cards.forEach((card) => mediaGrid.appendChild(card));
}

function requestKeepMedia(mediaId, button) {
  if (!mediaId || button.disabled) return;

  // Disable button and show loading state
  button.disabled = true;
  const originalText = button.innerHTML;
  button.innerHTML = `
        <svg class="w-4 h-4 mr-2 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"></path>
        </svg>
        Submitting...
    `;

  fetch(`/api/media/${mediaId}/request-keep`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
  })
    .then((response) => response.json())
    .then((data) => {
      if (data.success) {
        // Update button to show success state
        button.innerHTML = `
                <svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
                </svg>
                Request Submitted
            `;
        button.classList.remove("btn-primary");
        button.classList.add(
          "btn-secondary",
          "opacity-50",
          "cursor-not-allowed"
        );

        showToast("Request submitted successfully", "success");
      } else {
        throw new Error(data.message || "Unknown error");
      }
    })
    .catch((error) => {
      console.error("Error:", error);
      showToast("Failed to submit request: " + error.message, "error");

      // Restore button state
      button.disabled = false;
      button.innerHTML = originalText;
    });
}

function refreshMedia() {
  const refreshBtn = document.getElementById("refresh-btn");
  if (refreshBtn) {
    refreshBtn.disabled = true;
    refreshBtn.innerHTML = `
            <svg class="w-4 h-4 mr-2 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"></path>
            </svg>
            Refreshing...
        `;
  }

  // Reload the page to get fresh data
  setTimeout(() => {
    window.location.reload();
  }, 1000);
}

function showToast(message, type = "info") {
  // Remove existing toasts
  const existingToasts = document.querySelectorAll(".toast");
  existingToasts.forEach((toast) => toast.remove());

  // Create new toast
  const toast = document.createElement("div");
  toast.className = `toast fixed top-4 right-4 p-4 rounded-lg shadow-lg z-50 transition-all duration-300`;

  const typeClasses = {
    success: "bg-green-900 border border-green-700 text-green-100",
    error: "bg-red-900 border border-red-700 text-red-100",
    info: "bg-blue-900 border border-blue-700 text-blue-100",
    warning: "bg-yellow-900 border border-yellow-700 text-yellow-100",
  };

  toast.className += " " + (typeClasses[type] || typeClasses.info);

  const icons = {
    success:
      '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>',
    error:
      '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>',
    info: '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>',
    warning:
      '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L3.732 16.5c-.77.833.192 2.5 1.732 2.5z"></path>',
  };

  toast.innerHTML = `
        <div class="flex items-center">
            <svg class="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                ${icons[type] || icons.info}
            </svg>
            <span>${message}</span>
            <button class="ml-4 text-current hover:opacity-70" onclick="this.parentElement.parentElement.remove()">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
                </svg>
            </button>
        </div>
    `;

  document.body.appendChild(toast);

  // Auto-remove after 5 seconds
  setTimeout(() => {
    if (toast.parentElement) {
      toast.remove();
    }
  }, 5000);
}

function debounce(func, wait) {
  let timeout;
  return function executedFunction(...args) {
    const later = () => {
      clearTimeout(timeout);
      func(...args);
    };
    clearTimeout(timeout);
    timeout = setTimeout(later, wait);
  };
}
