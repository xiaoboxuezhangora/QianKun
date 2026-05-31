import { ref } from 'vue'

export function useAuth() {
  const loggedIn = ref(false)
  function login() {
    loggedIn.value = true
  }
  return { loggedIn, login }
}
