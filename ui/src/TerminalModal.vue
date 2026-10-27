<script setup>
import { nextTick, onUnmounted, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

const props = defineProps({ visible: Boolean, page: Boolean, name: { type: String, required: true } })
const emit = defineEmits(['close'])
const element = ref(null)
const message = ref('')
let terminal
let fit
let socket
let resizeObserver

function send(channel, value) {
  if (socket?.readyState !== WebSocket.OPEN) return
  const bytes = new TextEncoder().encode(value)
  const frame = new Uint8Array(bytes.length + 1)
  frame[0] = channel
  frame.set(bytes, 1)
  socket.send(frame)
}
function resize() {
  fit?.fit()
  if (terminal) send(4, JSON.stringify({ Width: terminal.cols, Height: terminal.rows }))
}
function disconnect() {
  if (socket) { socket.onopen = socket.onmessage = socket.onerror = socket.onclose = null; socket.close(); socket = null }
}
function dispose() {
  disconnect()
  resizeObserver?.disconnect()
  resizeObserver = null
  terminal?.dispose()
  terminal = null
  fit = null
}
function connect() {
  disconnect()
  terminal?.reset()
  message.value = ''
  const token = localStorage.getItem('csm-token')
  if (!token) { message.value = '请先登录后打开终端'; return }
  const base = window.$wujie?.props?.url || window.location.origin
  const url = new URL(`/csm-api/v1/csms/${encodeURIComponent(props.name)}/terminal`, base)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  socket = new WebSocket(url, ['v4.channel.k8s.io', `bearer.${token}`])
  socket.binaryType = 'arraybuffer'
  socket.onopen = () => { terminal?.focus(); resize() }
  socket.onmessage = event => {
    const frame = new Uint8Array(event.data)
    if (frame[0] === 1 || frame[0] === 2) terminal?.write(frame.subarray(1))
    else if (frame[0] === 3) message.value = new TextDecoder().decode(frame.subarray(1))
  }
  socket.onerror = () => { message.value = '终端连接失败' }
  socket.onclose = () => { message.value ||= '终端连接已断开' }
}
watch(() => [props.visible, props.name, props.page], async ([visible]) => {
  dispose()
  if (!visible) return
  await nextTick()
  if (!props.visible || !element.value) return
  terminal = new Terminal({ cursorBlink: true, theme: { background: '#1d2129' } })
  fit = new FitAddon()
  terminal.loadAddon(fit)
  terminal.open(element.value)
  terminal.onData(data => send(0, data))
  resizeObserver = new ResizeObserver(resize)
  resizeObserver.observe(element.value)
  resize()
  connect()
}, { immediate: true })
onUnmounted(dispose)
</script>

<template>
  <section v-if="page" class="terminal-page" aria-label="网页终端">
    <div class="terminal-page-heading"><h1>{{ name }} · 网页终端</h1></div>
    <div class="terminal-toolbar"><span>交互式终端</span><button class="terminal-reconnect" type="button" @click="connect">重新连接</button></div>
    <div v-if="visible" ref="element" class="terminal-screen" aria-label="主机终端" />
    <p v-if="message" class="terminal-message" role="status">{{ message }}</p>
  </section>
  <a-modal v-else :visible="visible" :title="`${name} · 网页终端`" width="min(900px, calc(100vw - 24px))" :footer="false" :unmount-on-close="true" @cancel="emit('close')">
    <div class="terminal-toolbar"><span>交互式终端</span><button class="terminal-reconnect" type="button" @click="connect">重新连接</button></div>
    <div v-if="visible" ref="element" class="terminal-screen" aria-label="主机终端" />
    <p v-if="message" class="terminal-message" role="status">{{ message }}</p>
  </a-modal>
</template>

<style scoped>
.terminal-toolbar{display:flex;align-items:center;justify-content:space-between;margin-bottom:10px;color:var(--color-text-2,#4e5969)}
.terminal-reconnect{min-height:44px;border:0;background:transparent;color:rgb(var(--primary-6,22,93,255));cursor:pointer}
.terminal-page{min-width:0}
.terminal-page-heading{display:flex;align-items:center;flex-wrap:wrap;gap:8px 16px;margin-bottom:16px}
.terminal-page-heading h1{min-width:0;overflow-wrap:anywhere}
.terminal-screen{height:min(520px,65vh);min-height:240px;min-width:0;padding:8px;overflow:hidden;background:#1d2129}
.terminal-page .terminal-screen{height:calc(100dvh - 200px)}
.terminal-message{margin:10px 0 0;color:rgb(var(--danger-6,245,63,63))}
</style>
