export async function getUser(id: string) {
  const res = await fetch(`/api/users/${id}`)
  return res.json()
}

export async function listUsers() {
  const res = await fetch('/api/users')
  return res.json()
}
