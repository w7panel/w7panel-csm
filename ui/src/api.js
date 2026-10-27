import { loginInMicroapp } from './auth.js'

const prefix = '/csm-api/v1'
let refreshPromise

export async function api(path, options = {}) {
  const request = window.$wujie?.props?.microappProxy || fetch
  async function send(token) {
    const headers = { ...(token ? { 'Authorization-csm': `Bearer ${token}` } : {}), ...options.headers }
    if (options.body) headers['Content-Type'] = 'application/json'
    return request(prefix + path, { ...options, headers })
  }
  const token = localStorage.getItem('csm-token')
  let response = await send(token)
  const refreshToken = localStorage.getItem('csm-refresh-token')
  if (response.status === 401 && refreshToken && path !== '/auth/refresh' && path !== '/auth/login') {
    let nextToken = localStorage.getItem('csm-token')
    if (nextToken === token) {
      if (!refreshPromise) refreshPromise = request(prefix + '/auth/refresh', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
      }).then(async refreshed => {
        if (!refreshed.ok) {
          localStorage.removeItem('csm-token')
          localStorage.removeItem('csm-refresh-token')
          return null
        }
        const tokens = await refreshed.json()
        if (tokens.access_token) {
          localStorage.setItem('csm-token', tokens.access_token)
          if (tokens.refresh_token) localStorage.setItem('csm-refresh-token', tokens.refresh_token)
        }
        return tokens.access_token
      }).finally(() => { refreshPromise = null })
      nextToken = await refreshPromise
    }
    if (nextToken) response = await send(nextToken)
  }
  if (response.status === 401 && window.__POWERED_BY_WUJIE__ && !path.startsWith('/auth/')) {
    let nextToken = localStorage.getItem('csm-token')
    if (!nextToken || nextToken === token) {
      await loginInMicroapp()
      nextToken = localStorage.getItem('csm-token')
    }
    if (nextToken) response = await send(nextToken)
  }
  if (!response.ok) {
    const message = await response.json().catch(() => ({}))
    const error = new Error(message.error || `请求失败 (${response.status})`)
    error.status = response.status
    throw error
  }
  return response.status === 204 ? null : response.json()
}

export const items = data => Array.isArray(data) ? data : data?.items || []
