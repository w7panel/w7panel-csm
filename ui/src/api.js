const prefix = '/csm-api/v1'

export async function api(path, options = {}) {
  const request = window.$wujie?.props?.microappProxy || fetch
  async function send(token) {
    const headers = { ...(token ? { 'Authorization-csm': `Bearer ${token}` } : {}), ...options.headers }
    if (options.body) headers['Content-Type'] = 'application/json'
    return request(prefix + path, { ...options, headers })
  }
  let response = await send(localStorage.getItem('csm-token'))
  const refreshToken = localStorage.getItem('csm-refresh-token')
  if (response.status === 401 && refreshToken && path !== '/auth/refresh' && path !== '/auth/login') {
    const refreshed = await request(prefix + '/auth/refresh', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    })
    if (refreshed.ok) {
      const tokens = await refreshed.json()
      if (tokens.access_token) {
        localStorage.setItem('csm-token', tokens.access_token)
        if (tokens.refresh_token) localStorage.setItem('csm-refresh-token', tokens.refresh_token)
        response = await send(tokens.access_token)
      }
    } else {
      localStorage.removeItem('csm-token')
      localStorage.removeItem('csm-refresh-token')
    }
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
