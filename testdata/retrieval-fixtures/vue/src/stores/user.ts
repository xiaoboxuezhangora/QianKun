import { defineStore } from 'pinia'

export const useUserStore = defineStore('user', {
  state: () => ({ name: '', token: '' }),
  actions: {
    login(name: string) {
      this.name = name
    },
  },
})
