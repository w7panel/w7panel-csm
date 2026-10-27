import assert from 'node:assert/strict'
import test from 'node:test'
import { loginInMicroapp } from './auth.js'
import { api } from './api.js'

test('microapp obtains a host OIDC code and exchanges it for a CSM token', async () => {
  const values = new Map([['csm-token', 'previous-account'], ['csm-refresh-token', 'previous-refresh']])
  globalThis.localStorage = {
    getItem: key => values.get(key),
    setItem: (key, value) => values.set(key, value),
    removeItem: key => values.delete(key),
  }
  const requests = []
  let authorization
  globalThis.fetch = async (path, options) => {
    requests.push({ path, options })
    const body = path.endsWith('/auth/config')
      ? { clientId: 'default', redirectUri: 'https://csm.example/callback', scope: 'openid profile', sshHost: 'ssh.example' }
      : { access_token: 'current-account', refresh_token: 'current-refresh' }
    return { ok: true, status: 200, json: async () => body }
  }
  globalThis.window = {
    __POWERED_BY_WUJIE__: true,
    $wujie: {
      bus: { $emit: (event, data, callback) => { assert.equal(event, 'getOidcCode'); authorization = data; callback('host-code') } },
      props: { url: '/panel-api/v1/microapp/w7panel-csm/proxy', paneltoken: 'host-token' },
    },
  }

  const config = await loginInMicroapp()
  assert.equal(config.sshHost, 'ssh.example')
  assert.equal(authorization.client_id, 'default')
  assert.equal(authorization.redirect_uri, config.redirectUri)
  assert.equal(authorization.code_challenge_method, 'S256')
  assert.ok(authorization.code_challenge)
  assert.equal(requests[0].options.headers['Authorization-csm'], undefined)
  assert.equal(requests[0].path, '/panel-api/v1/microapp/w7panel-csm/proxy/csm-api/v1/auth/config')
  assert.ok(requests.every(request => request.options.headers['X-W7Panel-Token'] === 'host-token'))
  const exchange = JSON.parse(requests[1].options.body)
  assert.equal(exchange.code, 'host-code')
  assert.equal(exchange.redirect_uri, config.redirectUri)
  assert.ok(exchange.code_verifier.length >= 43)
  assert.equal(values.get('csm-token'), 'current-account')
  assert.equal(values.get('csm-refresh-token'), 'current-refresh')
})

test('concurrent expired microapp requests authorize through the host once', async () => {
  const values = new Map([['csm-token', 'expired']])
  globalThis.localStorage = {
    getItem: key => values.get(key),
    setItem: (key, value) => values.set(key, value),
    removeItem: key => values.delete(key),
  }
  let authorizations = 0
  globalThis.fetch = async (path, options) => {
    let status = 200
    let body = { path }
    if (path.endsWith('/auth/config')) body = { clientId: 'default', redirectUri: 'https://csm.example/callback', scope: 'openid profile' }
    else if (path.endsWith('/auth/login')) {
      await new Promise(resolve => setTimeout(resolve, 5))
      body = { access_token: 'renewed' }
    } else if (options.headers['Authorization-csm'] !== 'Bearer renewed') status = 401
    assert.equal(options.headers['X-W7Panel-Token'], 'host-token')
    return { ok: status === 200, status, json: async () => body }
  }
  globalThis.window = {
    __POWERED_BY_WUJIE__: true,
    $wujie: {
      bus: { $emit: (_event, _data, callback) => { authorizations++; callback('host-code') } },
      props: { url: '/panel-api/v1/microapp/w7panel-csm/proxy', paneltoken: 'host-token' },
    },
  }

  const [hosts, orders] = await Promise.all([api('/csms'), api('/orders')])
  assert.equal(hosts.path, '/panel-api/v1/microapp/w7panel-csm/proxy/csm-api/v1/csms')
  assert.equal(orders.path, '/panel-api/v1/microapp/w7panel-csm/proxy/csm-api/v1/orders')
  assert.equal(authorizations, 1)
})
