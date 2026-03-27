class ApiClientError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiClientError'
  }
}

async function request<T>(url: string, options: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    ...options.headers as Record<string, string>,
  }

  if (options.body && typeof options.body === 'string') {
    headers['Content-Type'] = 'application/json'
  }

  const response = await fetch(url, { ...options, headers })

  if (response.status === 401) {
    // Session expired — redirect to login (but not if already there)
    if (window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
    throw new ApiClientError(401, 'Unauthorized')
  }

  if (!response.ok) {
    const text = await response.text()
    let message = `HTTP ${response.status}: ${response.statusText}`
    try {
      const json = JSON.parse(text)
      if (json.error) message = json.error
    } catch {
      // use default message
    }
    throw new ApiClientError(response.status, message)
  }

  return response.json() as Promise<T>
}

export function get<T>(url: string): Promise<T> {
  return request<T>(url, { method: 'GET' })
}

export function post<T>(url: string, body?: unknown): Promise<T> {
  return request<T>(url, {
    method: 'POST',
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
}

export function put<T>(url: string, body?: unknown): Promise<T> {
  return request<T>(url, {
    method: 'PUT',
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
}

export { ApiClientError }
