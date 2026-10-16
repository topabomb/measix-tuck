import { onBeforeUnmount, ref } from 'vue'

export function useMobileLayout() {
  const query = window.matchMedia('(max-width:760px)')
  const mobile = ref(query.matches)
  const update = () => { mobile.value = query.matches }
  query.addEventListener('change', update)
  onBeforeUnmount(() => query.removeEventListener('change', update))
  return mobile
}
