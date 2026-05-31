<template>
  <div class="order-list">
    <div v-for="o in orders" :key="o.id">{{ o.title }}</div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { fetchOrders } from '../services/orderApi'

const orders = ref<any[]>([])
onMounted(async () => {
  orders.value = await fetchOrders()
})
</script>
