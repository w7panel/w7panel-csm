import test from 'node:test'
import assert from 'node:assert/strict'
import { api } from './api.js'

const response = (status, body = {}) => ({ status, ok: status >= 200 && status < 300, json: async () => body })

test('concurrent expired requests refresh once and retry with the new token', async () => {
  const values = new Map([['csm-token', 'old'], ['csm-refresh-token', 'refresh']])
  globalThis.localStorage = { getItem: key => values.get(key), setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) }
  globalThis.window = {}
  let refreshes = 0
  globalThis.fetch = async (url, options) => {
    if (url.endsWith('/auth/refresh')) {
      refreshes++
      await new Promise(resolve => setTimeout(resolve, 10))
      return response(200, { access_token: 'new', refresh_token: 'next' })
    }
    return options.headers['Authorization-csm'] === 'Bearer new' ? response(200, { path: url }) : response(401)
  }

  const [hosts, versions] = await Promise.all([api('/csms'), api('/upgrade-versions')])
  assert.equal(refreshes, 1)
  assert.equal(hosts.path, '/csm-api/v1/csms')
  assert.equal(versions.path, '/csm-api/v1/upgrade-versions')
  assert.equal(values.get('csm-refresh-token'), 'next')
})

test('a late 401 retries with a token refreshed by another request', async () => {
  const values = new Map([['csm-token', 'old'], ['csm-refresh-token', 'refresh']])
  globalThis.localStorage = { getItem: key => values.get(key), setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) }
  globalThis.window = {}
  let releaseSlow
  let refreshes = 0
  globalThis.fetch = async (url, options) => {
    if (url.endsWith('/auth/refresh')) { refreshes++; return response(200, { access_token: 'new' }) }
    if (url.endsWith('/slow') && options.headers['Authorization-csm'] === 'Bearer old') await new Promise(resolve => { releaseSlow = resolve })
    return options.headers['Authorization-csm'] === 'Bearer new' ? response(200, { path: url }) : response(401)
  }

  const slow = api('/slow')
  await api('/fast')
  releaseSlow()
  assert.deepEqual(await slow, { path: '/csm-api/v1/slow' })
  assert.equal(refreshes, 1)
})
