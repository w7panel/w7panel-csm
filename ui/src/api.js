import { loginInMicroapp } from './auth.js'

const prefix = '/csm-api/v1'
let refreshPromise

export async function api(path, options = {}) {
  const props = window.$wujie?.props
  const url = (props?.url || '').replace(/\/$/, '') + prefix
  const panelToken = props?.paneltoken || (props ? localStorage.getItem('panelToken') : null)
  async function send(token) {
    const headers = { ...(token ? { 'Authorization-csm': `Bearer ${token}` } : {}), ...(panelToken ? { 'X-W7Panel-Token': panelToken } : {}), ...options.headers }
    if (options.body) headers['Content-Type'] = 'application/json'
    return fetch(url + path, { ...options, headers })
  }
  const token = localStorage.getItem('csm-token')
  let response = await send(token)
  const refreshToken = localStorage.getItem('csm-refresh-token')
  if (response.status === 401 && refreshToken && path !== '/auth/refresh' && path !== '/auth/login') {
    let nextToken = localStorage.getItem('csm-token')
    if (nextToken === token) {
      if (!refreshPromise) refreshPromise = fetch(url + '/auth/refresh', {
        method: 'POST', headers: { 'Content-Type': 'application/json', ...(panelToken ? { 'X-W7Panel-Token': panelToken } : {}) },
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
  if (response.status === 204) return null
  try {
    if (response.headers?.get && !response.headers.get('content-type')?.includes('json')) throw new Error('non-JSON response')
    return await response.json()
  } catch {
    const error = new Error('CSM 服务端返回了网页而非 API 数据，请更新服务端版本。')
    error.status = response.status
    error.code = 'unexpectedResponse'
    throw error
  }
}

export const items = data => Array.isArray(data) ? data : data?.items || []
