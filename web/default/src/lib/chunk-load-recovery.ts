/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

const CHUNK_RELOAD_KEY = 'newapi:chunk-load-reload-at'
const RELOAD_COOLDOWN_MS = 60_000

declare global {
  interface WindowEventMap {
    'vite:preloadError': Event & { payload?: unknown }
  }
}

function errorText(error: unknown): string {
  if (typeof error === 'string') return error
  if (error instanceof Error) {
    return `${error.name} ${error.message} ${error.stack ?? ''}`
  }
  if (typeof error === 'object' && error !== null) {
    try {
      return JSON.stringify(error)
    } catch {
      return String(error)
    }
  }
  return String(error)
}

export function isChunkLoadError(error: unknown): boolean {
  const message = errorText(error)
  return [
    'ChunkLoadError',
    'Loading chunk',
    'Loading CSS chunk',
    'Failed to fetch dynamically imported module',
    'Importing a module script failed',
    'dynamically imported module',
    '/static/js/async/',
    '/static/css/async/',
    "Unexpected token '<'",
  ].some((needle) => message.includes(needle))
}

export function recoverFromChunkLoadError(error: unknown): boolean {
  if (!isChunkLoadError(error)) return false
  if (typeof window === 'undefined') return false

  try {
    const lastReloadAt = Number(
      window.sessionStorage.getItem(CHUNK_RELOAD_KEY) || 0
    )
    if (Date.now() - lastReloadAt < RELOAD_COOLDOWN_MS) return false
    window.sessionStorage.setItem(CHUNK_RELOAD_KEY, String(Date.now()))
  } catch {
    // Storage may be unavailable; a single reload is still the best recovery.
  }

  window.location.reload()
  return true
}

export function installChunkLoadRecovery(): void {
  if (typeof window === 'undefined') return

  window.addEventListener('error', (event) => {
    recoverFromChunkLoadError(event.error ?? event.message)
  })

  window.addEventListener('unhandledrejection', (event) => {
    if (recoverFromChunkLoadError(event.reason)) {
      event.preventDefault()
    }
  })

  window.addEventListener('vite:preloadError', (event) => {
    if (recoverFromChunkLoadError(event.payload)) {
      event.preventDefault()
    }
  })
}
