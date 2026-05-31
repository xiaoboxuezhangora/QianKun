import { NgModule } from '@angular/core'
import { RouterModule, Routes } from '@angular/router'
import { UserComponent } from './user/user.component'
import { OrderComponent } from './order/order.component'

const routes: Routes = [
  { path: 'user', component: UserComponent },
  { path: 'order', component: OrderComponent },
]

@NgModule({
  imports: [RouterModule.forRoot(routes)],
  exports: [RouterModule],
})
export class AppRoutingModule {}
