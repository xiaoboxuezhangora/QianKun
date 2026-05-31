import { NgModule } from '@angular/core'
import { BrowserModule } from '@angular/platform-browser'
import { AppRoutingModule } from './app-routing.module'
import { UserComponent } from './user/user.component'
import { OrderComponent } from './order/order.component'

@NgModule({
  declarations: [UserComponent, OrderComponent],
  imports: [BrowserModule, AppRoutingModule],
  bootstrap: [],
})
export class AppModule {}
