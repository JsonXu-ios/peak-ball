/** VIPC logos already collected by the crawler are served by the local API. */
export function teamLogoUrl(path?: string | null): string {
  const value = path?.trim() || ''
  if (!value) return ''
  try {
    const url = new URL(value.startsWith('//') ? `https:${value}` : value)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return ''
    if ((url.hostname === 'vipc.cn' || url.hostname.endsWith('.vipc.cn')) && url.pathname.includes('/vipc-sport/image/')) {
      const filename = url.pathname.split('/').filter(Boolean).pop()
      return filename ? `/footballimg/${filename}` : ''
    }
    return url.href
  } catch {
    return value.startsWith('/footballimg/') || value.startsWith('/images/') ? value : ''
  }
}
