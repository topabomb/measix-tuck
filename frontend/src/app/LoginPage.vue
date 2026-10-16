<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue'
import { signIn } from './auth'
import TuckBrand from '../shared/ui/TuckBrand.vue'
const emit = defineEmits<{ signedIn: [] }>()
const username = ref(''), password = ref(''), remember = ref(false), busy = ref(false), error = ref(''), retry = ref(0)
let timer: ReturnType<typeof setInterval> | undefined
async function submit() {
  if (busy.value || retry.value) return
  busy.value = true; error.value = ''
  try { await signIn(username.value,password.value,remember.value); password.value = ''; emit('signedIn') }
  catch (cause) {
    error.value = (cause as Error).message; password.value = ''
    retry.value = (cause as Error & {retryAfter?:number}).retryAfter || 0
    if (retry.value) { clearInterval(timer); timer = setInterval(() => { if (--retry.value <= 0) clearInterval(timer) },1000) }
  } finally { busy.value = false }
}
onBeforeUnmount(() => clearInterval(timer))
</script>
<template>
  <main class="login-page"><form class="login-card" @submit.prevent="submit">
    <h1><TuckBrand /></h1><p>接住交办，持续推进</p>
    <label>用户名<input v-model="username" autocomplete="username" maxlength="80" autofocus required /></label>
    <label>密码<input v-model="password" type="password" autocomplete="current-password" maxlength="1024" required /></label>
    <label class="remember"><input v-model="remember" type="checkbox" />在此设备保持登录 30 天</label>
    <p v-if="error" role="alert">{{ error }}{{ retry ? `（${retry} 秒）` : '' }}</p>
    <button type="submit" :disabled="busy || !!retry">{{ busy ? '登录中…' : '登录' }}</button>
  </form></main>
</template>
<style scoped>
.login-page { min-height:100dvh; display:grid; place-items:center; padding:24px; box-sizing:border-box; background:var(--tuck-background); color:var(--tuck-text); font:var(--tuck-font-ui)/1.5 var(--tuck-font-family); }
.login-card { width:min(100%,320px); padding:24px; border:1px solid var(--tuck-border); border-radius:14px; background:var(--tuck-surface); }
h1 { margin:0; font-size:24px; } p { margin:8px 0 20px; color:var(--tuck-muted); } label { display:grid; gap:6px; margin:16px 0; }
input:not([type=checkbox]) { width:100%; box-sizing:border-box; padding:10px; border:1px solid var(--tuck-border); border-radius:7px; color:var(--tuck-text); background:var(--tuck-background); font:inherit; }
.remember { display:flex; align-items:center; gap:8px; } input[type=checkbox] { accent-color:var(--tuck-accent); } [role=alert] { color:var(--tuck-danger); }
button { width:100%; padding:10px; border:0; border-radius:7px; background:var(--tuck-accent); color:var(--tuck-accentText); font:inherit; cursor:pointer; } button:disabled { opacity:.6; cursor:wait; }
</style>
