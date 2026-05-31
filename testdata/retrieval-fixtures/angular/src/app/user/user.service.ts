import { Injectable } from '@angular/core'

@Injectable({ providedIn: 'root' })
export class UserService {
  getUser(id: string) {
    return { id }
  }
}
