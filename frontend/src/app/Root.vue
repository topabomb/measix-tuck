<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { pageTitle } from './page-title'
const route = useRoute()
const detail = ref<{path:string;title:string}>()
watch(() => route.fullPath, () => { detail.value = undefined }, {flush:'sync'})
const title = computed(() => detail.value?.path === route.fullPath ? detail.value.title : pageTitle(route))
watch(title, value => { document.title = value }, {immediate:true,flush:'post'})
</script>
<template><RouterView v-slot="{ Component }"><component :is="Component" @page-title="detail = $event" /></RouterView></template>
