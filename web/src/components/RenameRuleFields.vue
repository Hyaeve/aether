<script setup>
import { useId } from 'vue'
import RoundedSelect from './RoundedSelect.vue'
import NumberInput from './NumberInput.vue'
import { renameDeleteTypes } from '../rename-rules'
defineProps({ rule: { type: Object, required: true } })
const id = useId()
const separators = [
  { value: '、', label: '顿号（、）' }, { value: '-', label: '连字符（-）' },
  { value: '_', label: '下划线（_）' }, { value: ' ', label: '空格' },
  { value: '.', label: '点号（.）' }, { value: '', label: '无分隔符' }
]
const cases = [
  { value: 'none', label: '不转换' }, { value: 'lower', label: '全部小写' },
  { value: 'upper', label: '全部大写' }, { value: 'title', label: '每个单词首字母大写' },
  { value: 'sentence', label: '仅第一个首字母大写' }
]
const chinese = [
  { value: 'none', label: '不转换' }, { value: 't2s', label: '繁体转简体' },
  { value: 's2hk', label: '简体转繁体(香港)' }, { value: 's2tw', label: '简体转繁体(台湾)' }
]
const widths = [
  { value: 'none', label: '不转换' },
  { value: 'narrow_cn', label: '将全角转换为中文半角' },
  { value: 'narrow_en', label: '将全角转换为英文半角' },
  { value: 'wide_cn', label: '将半角转换为中文全角' },
  { value: 'wide_en', label: '将半角转换为英文全角' }
]
</script>
<template>
  <div class="rename-type-fields">
    <template v-if="rule.kind === 'replace'">
      <div class="rename-field"><span>查找类型</span><RoundedSelect v-model="rule.findType" label="查找类型" :options="[{value:'literal',label:'常规查找'},{value:'regex',label:'正则查找'}]" /></div>
      <label class="rename-field">查找内容<input v-model="rule.find" maxlength="255" /></label>
      <label class="rename-field">替换为<input v-model="rule.replace" maxlength="255" /></label>
      <div class="rename-rule-checks"><label class="rename-check"><input v-model="rule.caseSensitive" type="checkbox" />大小写</label><label class="rename-check"><input v-model="rule.firstOnly" type="checkbox" />仅第一个</label></div>
    </template>
    <template v-else-if="rule.kind === 'number'">
      <label class="rename-field">名称统一修改为<input v-model="rule.name" maxlength="255" placeholder="留空保留原名称" /></label>
      <div class="rename-field"><span>序号位置</span><RoundedSelect v-model="rule.numberPosition" label="序号位置" :options="[{value:'before',label:'名称之前'},{value:'after',label:'名称之后'}]" /></div>
      <label class="rename-field">开始序号<NumberInput v-model="rule.start" aria-label="开始序号" min="1" max="1000000000" step="1" /></label>
      <label class="rename-field">序号最少位数<NumberInput v-model="rule.digits" aria-label="序号最少位数" min="1" max="12" step="1" /></label>
      <div class="rename-field"><span>分隔符</span><RoundedSelect v-model="rule.separator" label="分隔符" :options="separators" /></div>
    </template>
    <template v-else-if="rule.kind === 'insert'">
      <label class="rename-field">添加到第几位<NumberInput v-model="rule.position" aria-label="添加到第几位" min="1" max="4096" step="1" /></label>
      <label class="rename-field">添加的字符<input v-model="rule.text" maxlength="255" /></label>
    </template>
    <template v-else-if="rule.kind === 'delete'">
      <div class="rename-delete-types" role="group" aria-label="删除类型"><label v-for="type in renameDeleteTypes" :key="type.value" class="rename-check"><input v-model="rule.deleteTypes" type="checkbox" :value="type.value" />{{type.label}}</label></div>
      <div class="rename-range">
        <label class="rename-check"><input v-model="rule.deleteRange" type="checkbox" />按位置删除</label>
        <fieldset :disabled="!rule.deleteRange">
          <label class="rename-field">从位置<NumberInput v-model="rule.from" aria-label="从位置" min="1" max="4096" step="1" /></label>
          <div class="rename-directions" role="group" aria-label="开始位置方向"><label class="rename-check"><input v-model="rule.fromEnd" type="radio" :name="`${id}-from`" :value="false" />从左到右</label><label class="rename-check"><input v-model="rule.fromEnd" type="radio" :name="`${id}-from`" :value="true" />从右到左</label></div>
          <label class="rename-field">到位置<NumberInput v-model="rule.to" aria-label="到位置" min="1" max="4096" step="1" /></label>
          <div class="rename-directions" role="group" aria-label="结束位置方向"><label class="rename-check"><input v-model="rule.toEnd" type="radio" :name="`${id}-to`" :value="false" />从左到右</label><label class="rename-check"><input v-model="rule.toEnd" type="radio" :name="`${id}-to`" :value="true" />从右到左</label></div>
        </fieldset>
      </div>
      <label class="rename-field">自定义删除<input v-model="rule.text" maxlength="255" /></label>
      <div class="rename-rule-checks"><label class="rename-check"><input v-model="rule.firstOnly" type="checkbox" />仅匹配第一个</label></div>
    </template>
    <template v-else-if="rule.kind === 'move'">
      <label class="rename-field">将第几位<NumberInput v-model="rule.from" aria-label="将第几位" min="1" max="4096" step="1" /></label>
      <label class="rename-field">移动到第几位<NumberInput v-model="rule.to" aria-label="移动到第几位" min="1" max="4096" step="1" /></label>
    </template>
    <template v-else-if="rule.kind === 'convert'">
      <div class="rename-field"><span>大小写</span><RoundedSelect v-model="rule.caseMode" label="大小写" :options="cases" /></div>
      <div class="rename-field"><span>繁转简</span><RoundedSelect v-model="rule.chineseMode" label="繁转简" :options="chinese" /></div>
      <div class="rename-field"><span>全半角</span><RoundedSelect v-model="rule.widthMode" label="全半角" :options="widths" /></div>
    </template>
  </div>
</template>
<style scoped>
.rename-type-fields { min-width:0; }
.rename-field { display:grid;grid-template-columns:92px minmax(0,1fr);align-items:center;gap:8px;margin-top:10px;font-size:12px; }
.rename-field > :last-child { min-width:0;width:100%; }
.rename-field input, .rename-field :deep(input), .rename-field :deep(.rounded-select-trigger) { height:32px;min-height:32px;padding-top:3px;padding-bottom:3px;font-size:12px;border-radius:6px; }
.rename-field :deep(.select-label) { display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap; }
.rename-field :deep(.rounded-select-popup) { width:100%;min-width:0;border-radius:8px;max-height:220px; }
.rename-field :deep(.rounded-select-option) { white-space:normal;min-height:32px;font-size:12px; }
.rename-field :deep(.rounded-select-option .select-label) { white-space:normal;overflow-wrap:anywhere; }
.rename-check { display:flex;align-items:center;gap:6px;font-size:12px;margin:0;min-height:28px; }
.rename-check input { flex:none;width:14px;height:14px;margin:0; }
.rename-rule-checks, .rename-directions { display:flex;flex-wrap:wrap;gap:6px 16px;margin-top:8px; }
.rename-delete-types { display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:4px 8px;margin-top:10px; }
.rename-range { border-block:1px solid var(--border);padding-block:10px;margin-top:10px; }
.rename-range fieldset { border:0;padding:0;margin:0;min-width:0; }
.rename-range fieldset:disabled { opacity:.5; }
.rename-directions { justify-content:flex-end; }
@media (max-width:760px) {
  .rename-field { grid-template-columns:88px minmax(0,1fr); }
  .rename-field input, .rename-field :deep(input), .rename-field :deep(.rounded-select-trigger) { height:36px;min-height:36px; }
  .rename-check { min-height:32px; }
}
</style>
