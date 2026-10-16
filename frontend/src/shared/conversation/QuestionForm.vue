<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import type { PendingQuestion } from '../../engine/conversation/contracts'
const props = defineProps<{ question: PendingQuestion; disabled: boolean; fullPage?: boolean }>()
const emit = defineEmits<{ submit: [answer: string]; skip: [] }>()
const formId = useId()
type Field = { type?: string; title?: string; description?: string; enum?: unknown[]; enumNames?: string[]; oneOf?: {const: unknown; title?: string}[]; default?: unknown; minimum?: number; maximum?: number; minLength?: number; maxLength?: number; format?: string }
const fields = computed(() => Object.entries((props.question.schema?.properties || { response: {type:'string',title:'回答问题'} }) as Record<string, Field>).map(([key, spec]) => ({
  key, ...spec, label: spec.title || key, required: (props.question.schema?.required as string[] | undefined)?.includes(key) ?? false,
  options: spec.enum?.map((value, i) => ({value,label:spec.enumNames?.[i] || String(value)})) || spec.oneOf?.map(item => ({value:item.const,label:item.title || String(item.const)})),
})).sort((a, b) => Number(b.required) - Number(a.required)))
const values = ref<Record<string, unknown>>(Object.create(null))
for (const field of fields.value) if (field.default !== undefined) values.value[field.key] = ['object','array'].includes(field.type || '') ? JSON.stringify(field.default) : field.default
const error = ref('')
function submit() {
  error.value = ''
  const result: Record<string, unknown> = Object.create(null)
  for (const field of fields.value) {
    const value = values.value[field.key]
    if (value === undefined || value === '') { if (!field.required) continue }
    if (['object','array'].includes(field.type || '') && !field.options) {
      try { result[field.key] = JSON.parse(String(value)) } catch { error.value = `「${field.label}」需要有效 JSON`; return }
    } else if (field.type === 'number' || field.type === 'integer') result[field.key] = Number(value)
    else result[field.key] = value
  }
  emit('submit', JSON.stringify(result))
}
</script>
<template>
  <form class="question-form" :class="{'question-full-page':fullPage}" :aria-label="question.title || '回答问题'" :aria-describedby="question.detail ? `${formId}-message` : undefined" @submit.prevent="submit">
    <div class="question-fields">
      <p v-if="question.detail" :id="`${formId}-message`" class="question-message">{{ question.detail }}</p>
      <label v-for="(field, index) in fields" :key="field.key" class="question-field">
        <span>{{ field.label }}<span v-if="field.required" class="required" aria-hidden="true"> *</span></span>
        <select v-if="field.options || field.type === 'boolean'" v-model="values[field.key]" :aria-label="field.label" :aria-describedby="field.description ? `${formId}-${index}-help` : undefined" :required="field.required" :disabled="disabled">
          <option :value="undefined" :disabled="field.required">请选择</option>
          <template v-if="field.options"><option v-for="(option, i) in field.options" :key="i" :value="option.value">{{ option.label }}</option></template>
          <template v-else><option :value="true">是</option><option :value="false">否</option></template>
        </select>
        <textarea v-else-if="['object','array'].includes(field.type || '')" v-model="values[field.key] as string" :aria-label="field.label" :aria-describedby="field.description ? `${formId}-${index}-help` : undefined" :required="field.required" :disabled="disabled" rows="3" placeholder="JSON" />
        <input v-else v-model="values[field.key]" :data-testid="index === 0 ? 'question-answer' : undefined" :aria-label="field.label" :aria-describedby="field.description ? `${formId}-${index}-help` : undefined" :type="field.type === 'number' || field.type === 'integer' ? 'number' : field.format === 'email' ? 'email' : field.format === 'date' ? 'date' : 'text'" :step="field.type === 'integer' ? 1 : 'any'" :min="field.minimum" :max="field.maximum" :minlength="field.minLength" :maxlength="field.maxLength" :required="field.required" :disabled="disabled" />
        <small v-if="field.description" :id="`${formId}-${index}-help`">{{ field.description }}</small>
      </label>
      <p v-if="error" role="alert" class="question-error">{{ error }}</p>
    </div>
    <div class="question-actions"><button type="button" data-testid="deny-interaction" :disabled="disabled" @click="emit('skip')">跳过</button><button type="submit" class="primary" data-testid="approve-interaction" :disabled="disabled">提交回答</button></div>
  </form>
</template>
<style scoped>
.question-form { min-width:0; }.question-fields { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); column-gap:16px; max-height:min(180px,max(60px,calc(34dvh - 112px))); overflow:auto; padding:2px; scrollbar-width:thin; }
.question-message,.question-error { grid-column:1/-1; }
.question-message { margin:8px 0 12px; white-space:pre-wrap; overflow-wrap:anywhere; line-height:1.5; }
.question-field { display:grid; gap:6px; margin:5px 0; font-size:var(--tuck-font-ui); min-width:0; }.required { color:var(--tuck-muted); }
.question-field input,.question-field select,.question-field textarea { box-sizing:border-box; width:100%; min-width:0; max-width:100%; min-height:34px; padding:7px 9px; border:1px solid var(--tuck-border); border-radius:7px; background:var(--tuck-background); color:var(--tuck-text); font:inherit; }
.question-field textarea { resize:vertical; }.question-field small { color:var(--tuck-muted); overflow-wrap:anywhere; }.question-error { color:var(--tuck-danger); }
.question-actions { display:flex; justify-content:flex-end; gap:8px; padding-top:10px; }
.question-full-page .question-fields { grid-template-columns:minmax(0,1fr); max-height:none; overflow:visible; padding:0; }
.question-full-page .question-field { margin:8px 0; }
button { padding:7px 12px; border:1px solid var(--tuck-border); border-radius:8px; background:var(--tuck-surface); color:var(--tuck-text); font:inherit; cursor:pointer; }button.primary { background:var(--tuck-accent); color:var(--tuck-accentText); border-color:transparent; }button:disabled { opacity:.45; cursor:default; }
@media(max-width:640px) { .question-fields { grid-template-columns:minmax(0,1fr); } }
</style>
