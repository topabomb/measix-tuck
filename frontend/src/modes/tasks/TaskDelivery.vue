<script setup lang="ts">
import type { DockerTask } from '../../shared/workspace/runtime'
import DeliveryAnswer from '../../shared/delivery/DeliveryAnswer.vue'
import TaskResult from './TaskResult.vue'
import { taskName } from './task-view'
defineProps<{task:DockerTask}>()
</script>
<template><section class="task-page task-delivery" :aria-label="`任务交付：${taskName(task.title)}`"><h1>{{ taskName(task.title) }}</h1><DeliveryAnswer :task="task" /><TaskResult :task="task" /></section></template>
