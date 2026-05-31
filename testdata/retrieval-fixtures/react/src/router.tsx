import { createBrowserRouter } from 'react-router-dom'
import Dashboard from './pages/Dashboard'

export const router = createBrowserRouter([
  { path: '/', element: <Dashboard /> },
  { path: '/about', element: <div>about</div> },
])
