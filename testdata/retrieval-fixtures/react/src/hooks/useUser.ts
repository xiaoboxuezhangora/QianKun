import { useState } from 'react'

export function useUser() {
  const [user, setUser] = useState<{ id: string } | null>(null)
  return { user, setUser }
}
