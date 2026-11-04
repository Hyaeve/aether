<script setup>
import Icon from './Icon.vue'
defineProps({membership:{type:Object,required:true},provider:String})
</script>
<template><span class="storage-membership" :class="[membership.tier,`member-${provider}`]" role="img" :aria-label="membership.label" :data-tooltip="`${membership.label} · 最近读取 ${new Date(membership.checkedAt).toLocaleString()}`"><Icon :name="membership.icon" :size="12"/><span>{{membership.label}}</span></span></template>
<style scoped>
.storage-membership{display:inline-flex;align-items:center;justify-content:center;gap:3px;white-space:nowrap;height:19px;padding:0 5px;border-radius:5px;border:1px solid #cba76266;background:linear-gradient(120deg,#34302a,#211e19);color:#f1d399;font-size:10px;line-height:17px;font-weight:600;flex:none;pointer-events:auto}
.storage-membership.silver,.storage-membership.platinum{background:linear-gradient(120deg,#dbe7f2,#eff4fa);border-color:#b0c3d8;color:#425a79}
.storage-membership.diamond{background:linear-gradient(120deg,#ddf4f7,#eefbfd);border-color:#a5d6e0;color:#28667a}
.storage-membership.permanent{background:linear-gradient(120deg,#352b47,#211a2d);border-color:#b69bc27a;color:#e8cef6}
.storage-membership[class~="88vip"]{background:linear-gradient(120deg,#523323,#2b201b);border-color:#c49b725e;color:#f4d2a7}
.storage-membership.member-quark.svip-plus{background:linear-gradient(120deg,#382d49,#211b2b);border-color:#b69bc270;color:#ebd6fb}
[data-theme=dark] .storage-membership.silver,[data-theme=dark] .storage-membership.platinum{background:linear-gradient(120deg,#293747,#202a38);color:#d3e0f0;border-color:#6682a1}
[data-theme=dark] .storage-membership.diamond{background:linear-gradient(120deg,#1f3945,#192d37);color:#b3e7ef;border-color:#497d8e}
</style>
