export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

// Each client reads the CSRF token owned by its current session.
export function createHTTPClient(base: string, csrf: () => string) {
  return async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(init?.headers as Record<string, string>),
    }
    const csrfToken = csrf()
    const method = init?.method ?? 'GET'
    if (method !== 'GET' && csrfToken) {
      headers['X-CSRF-Token'] = csrfToken
      headers['Origin'] = window.location.origin
    }
    const res = await fetch(`${base}${path}`, {
      ...init,
      headers,
      credentials: 'include',
    })
    if (!res.ok) {
      const body = await res.json().catch(() => null)
      throw new ApiError(res.status, body?.error?.code ?? 'unknown', body?.error?.message ?? res.statusText)
    }
    if (res.status === 204) return undefined as T
    return res.json() as Promise<T>
  }
}
