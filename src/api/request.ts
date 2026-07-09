import axios from 'axios'

const apiBaseURL = (import.meta.env.VITE_API_BASE_URL || '/api').trim()
const assetBaseURL = (import.meta.env.VITE_ASSET_BASE_URL || '').trim().replace(/\/$/, '')

const apiClient = axios.create({
  baseURL: apiBaseURL,
  headers: {
    'Content-Type': 'application/json',
  },
})

export function resolveAssetUrl(path: string): string {
  if (!path) {
    return ''
  }

  const localFootballLogo = resolveVipcFootballLogo(path)
  if (localFootballLogo) {
    return localFootballLogo
  }

  if (/^https?:\/\//i.test(path)) {
    return path
  }

  return `${assetBaseURL}${path}`
}

function resolveVipcFootballLogo(path: string): string {
  try {
    const url = new URL(path)
    if (!url.hostname.endsWith('vipc.cn') || !url.pathname.includes('/vipc-sport/image/')) {
      return ''
    }
    const filename = url.pathname.split('/').filter(Boolean).pop()
    return filename ? `${assetBaseURL}/footballimg/${filename}` : ''
  } catch {
    return ''
  }
}

export default apiClient