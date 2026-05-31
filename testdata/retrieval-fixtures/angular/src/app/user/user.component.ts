import { Component } from '@angular/core'
import { UserService } from './user.service'

@Component({
  selector: 'app-user',
  template: '<div>{{ name }}</div>',
})
export class UserComponent {
  name = ''
  constructor(private userService: UserService) {}
}
