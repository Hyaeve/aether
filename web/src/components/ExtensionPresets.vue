<script setup>
import {computed} from 'vue'
import Icon from './Icon.vue'
const props=defineProps({modelValue:{type:String,default:''},groups:{type:Array,default:()=>['video','audio']},addOnly:Boolean,labelPrefix:{type:String,default:''}})
const emit=defineEmits(['update:modelValue','commit'])
const presets={video:'mp4;mkv;avi;mov;wmv;flv;webm;m4v;ts;m2ts;mkvb;rm;3gp;iso',audio:'mp3;flac;wav;aac;m4a;ogg;wma;ape;alac;opus',comic:'cbz;cbr;zip;rar;epub;pdf',image:'jpg;jpeg;png;webp',data:'nfo;ass;srt;ssa;sub',document:'pdf;doc;docx;xls;xlsx;ppt;pptx;txt;md;csv;epub;mobi;cbz;cbr;zip;rar'}
const values=computed(()=>new Set(props.modelValue.toLowerCase().split(/[;,\s]+/).map(v=>v.replace(/^\./,'')).filter(Boolean)))
const active=group=>presets[group].split(';').every(v=>values.value.has(v))
function toggle(group){const next=new Set(values.value),remove=!props.addOnly&&active(group);presets[group].split(';').forEach(v=>remove?next.delete(v):next.add(v));emit('update:modelValue',[...next].join(';'));emit('commit')}
</script>
<template><span class="extension-presets"><button v-for="group in groups" :key="group" type="button" class="icon-btn extension-preset" :class="group" :aria-label="`${labelPrefix}${{video:'视频',audio:'音频',comic:'漫画',image:'图片',data:'数据',document:'文档'}[group]}扩展名`" :aria-pressed="active(group)" @click="toggle(group)"><Icon :name="{video:'Film',audio:'Music',comic:'BookOpen',image:'Image',data:'FileJson',document:'Files'}[group]" /></button></span></template>
<style scoped>.extension-presets{display:flex;gap:4px}</style>
