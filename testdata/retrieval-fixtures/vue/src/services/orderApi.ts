import axios from 'axios'

export async function fetchOrders() {
  const res = await axios.get('/api/orders')
  return res.data
}

export async function createOrder(payload: any) {
  const res = await axios.post('/api/orders', payload)
  return res.data
}
