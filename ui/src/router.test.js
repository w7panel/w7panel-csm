import assert from 'node:assert/strict'
import test from 'node:test'
import { createMemoryHistory } from 'vue-router'
import { createAppRouter } from './router.js'

test('CSM routes keep navigation and host names', async () => {
  const router = createAppRouter({}, createMemoryHistory())
  await router.push('/')
  assert.equal(router.currentRoute.value.name, 'hosts')
  await router.push({ name: 'host', params: { name: 'test host' } })
  assert.equal(router.currentRoute.value.path, '/hosts/test%20host')
  assert.equal(router.currentRoute.value.params.name, 'test host')
  await router.push({ name: 'terminal', params: { name: 'test host' } })
  assert.equal(router.currentRoute.value.meta.section, 'hosts')
  await router.push('/apps')
  assert.equal(router.currentRoute.value.meta.section, 'templates')
  await router.push('/versions')
  assert.equal(router.currentRoute.value.name, 'versions')
})
