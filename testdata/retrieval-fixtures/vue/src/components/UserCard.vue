<template>
  <div class="user-card">{{ name }}</div>
</template>

<script setup lang="ts">
defineProps<{ name: string }>()
</script>
