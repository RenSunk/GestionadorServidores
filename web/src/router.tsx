import { createBrowserRouter } from 'react-router'
import { Layout } from './components/Layout.tsx'
import { DashboardPage } from './pages/DashboardPage.tsx'
import { NewServerPage } from './pages/NewServerPage.tsx'
import { NotFoundPage } from './pages/NotFoundPage.tsx'
import { ServerDetailPage } from './pages/ServerDetailPage.tsx'
import { ServersPage } from './pages/ServersPage.tsx'

export const router = createBrowserRouter([
  {
    element: <Layout />,
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'servers', element: <ServersPage /> },
      { path: 'servers/new', element: <NewServerPage /> },
      { path: 'servers/:id', element: <ServerDetailPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])
