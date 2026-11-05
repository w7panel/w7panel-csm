import test from 'node:test'
import assert from 'node:assert/strict'
import { terminalConnection } from './terminalConnection.js'

test('terminal connection uses the panel microapp proxy and both authentication protocols', () => {
  const connection = terminalConnection('ckm-85455', 'csm-admin', 'csm-token', {
    url: '/panel-api/v1/microapp/w7panel-csm/proxy/', paneltoken: 'panel-token',
  }, 'https://panel.example')
  assert.equal(connection.url, 'wss://panel.example/panel-api/v1/microapp/w7panel-csm/proxy/csm-api/v1/csms/ckm-85455/terminal?namespace=csm-admin')
  assert.deepEqual(connection.protocols, ['v4.channel.k8s.io', 'w7panel-bearer.panel-token', 'bearer.csm-token'])
})

test('standalone terminal uses the CSM origin', () => {
  assert.deepEqual(terminalConnection('host', '', 'token', {}, 'https://csm.example'), {
    url: 'wss://csm.example/csm-api/v1/csms/host/terminal',
    protocols: ['v4.channel.k8s.io', 'bearer.token'],
  })
})
