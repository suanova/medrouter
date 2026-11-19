declare global {
  interface Window {
    __BASE_PATH__?: string
    __API_BASE__?: string
  }
}

// The Go server injects window.__BASE_PATH__ into index.html so one build can
// be served from the site root or from a prefix such as /medrouter.
export const basePath =
  typeof window !== 'undefined' ? (window.__BASE_PATH__ ?? '').replace(/\/+$/, '') : ''

// Replaced at build/dev time by rsbuild when VITE_API_BASE is set (the dev
// server needs it because it does not run the Go HTML injector).
declare const __API_BASE_OVERRIDE__: string

const apiBaseOverride =
  typeof __API_BASE_OVERRIDE__ === 'string' ? __API_BASE_OVERRIDE__ : ''

// The API is normally mounted at <site>/api. A deployment can move it (for
// example serve the site at "/" with API_PREFIX=/medrouter/api); the server
// then injects the absolute prefix as window.__API_BASE__.
export const apiBase =
  (typeof window !== 'undefined' ? window.__API_BASE__?.replace(/\/+$/, '') : undefined) ||
  apiBaseOverride.replace(/\/+$/, '') ||
  `${basePath}/api`

export function withBasePath(path: string): string {
  const normalized = path.startsWith('/') ? path : `/${path}`
  return `${basePath}${normalized}`
}

// withApiBase prefixes a path that is relative to the API root (for example
// "/oauth/github/start") with the configured API prefix.
export function withApiBase(path: string): string {
  const normalized = path.startsWith('/') ? path : `/${path}`
  return `${apiBase}${normalized}`
}
