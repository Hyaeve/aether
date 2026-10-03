<script setup>
import { computed, ref } from 'vue'
import { state, date } from '../lib'
import Icon from '../components/Icon.vue'
const query = ref(''), level = ref('all')
const logs = computed(() => state.logs.filter(l => (level.value === 'all' || l.level === level.value) && l.message.includes(query.value)).slice().reverse())
</script>
<template>
  <section class="page-head"><div><div class="eyebrow">SYSTEM EVENTS</div><h1>系统日志<span class="title-dot">.</span></h1><p>记录每一次连接与任务执行。</p></div><span class="muted">{{ logs.length }} 条记录</span></section><div class="section-toolbar"><select v-model="level" aria-label="日志级别"><option value="all">全部级别</option><option value="info">信息</option><option value="success">成功</option><option value="error">错误</option><option value="cancelled">已停止</option></select><div class="search-field"><Icon name="Search" :size="16" /><input v-model="query" aria-label="搜索日志" placeholder="搜索日志…" /></div></div><div class="table-wrap"><table><thead><tr><th>时间</th><th>级别</th><th>事件</th></tr></thead><tbody><tr v-for="(l, i) in logs" :key="i"><td class="nowrap">{{ date(l.time) }}</td><td><span class="status" :class="l.level === 'error' ? 'danger' : l.level === 'success' ? 'success' : 'pending'">{{ l.level }}</span></td><td>{{ l.message }}</td></tr></tbody></table><div v-if="!logs.length" class="small-empty">暂无日志</div></div>
</template>
