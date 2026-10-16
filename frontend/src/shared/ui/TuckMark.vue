<script setup lang="ts">
defineProps<{ animated?: boolean }>()
</script>

<template>
  <svg class="tuck-mark" :class="{ animated }" viewBox="0 0 64 64" fill="currentColor" aria-hidden="true"><path d="M10.3 24.7C9 20.7 12.9 18.9 15.4 22C18.2 25.4 20.1 30.4 23.3 34.1C26.2 37.5 30 39.3 34.4 38.7C42.1 37.6 47 31.8 46.7 25.1C46.5 20.1 43.6 16.3 39.4 14.2C37.7 13.4 38.2 11 40.2 11.4C49.8 13.1 55.5 20.8 55.5 30C55.5 43.6 45.8 54 32.5 54C21.9 54 13.6 47.9 10.7 38.5C9.3 34 8.8 29.3 10.3 24.7Z"/><g class="tuck-spark"><path d="M30 13.8C34.8 17 37.6 21.4 35.4 26.4C34.5 28.5 32.5 30.5 30.7 31.1C27.8 28.7 25.8 25.4 26.1 22.1C26.4 18.7 28.1 15.8 30 13.8Z" transform="rotate(17 31 22)"/></g></svg>
</template>

<style scoped>
.tuck-mark { display:block; width:1em; height:1em; flex:none; transform-origin:center; }
.tuck-mark.animated { animation:tuck-breathe 2s ease-in-out infinite; }
.animated .tuck-spark { transform-box:fill-box; transform-origin:center; animation:tuck-spark 2s ease-in-out infinite; }
@keyframes tuck-breathe {
  0%,100% { opacity:.65; transform:scale(.92) rotate(-5deg); }
  50% { opacity:1; transform:scale(1) rotate(5deg); }
}
@keyframes tuck-spark {
  0%,100% { transform:translateY(1px); }
  50% { transform:translateY(-2px); }
}
@media(prefers-reduced-motion:reduce) { .tuck-mark.animated, .animated .tuck-spark { animation:none; } }
</style>
