<script setup>
import Icon from '../components/Icon.vue'
defineProps({ title: String, items: { default: () => [] }, icon: { default: 'Wrench' } })
</script>
<template>
  <section class="page-head"><div><div class="eyebrow">WORKSPACE SERVICES</div><h1>{{ title }}<span class="title-dot">.</span></h1></div><span class="status pending">尚未实现</span></section>
  <div class="empty-state"><span class="empty-icon"><Icon :name="icon" :size="36" /></span><h3>{{ title }}正在规划中</h3><p>当前版本尚不提供此服务，以下为计划模块。</p></div>
  <div v-if="items.length" class="planned-list"><div v-for="item in items" :key="item"><Icon name="CircleDashed" :size="17" /><span>{{ item }}</span><small>待开发</small></div></div>
</template>
