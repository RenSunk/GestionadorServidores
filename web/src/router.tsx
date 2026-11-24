import { createBrowserRouter } from 'react-router'
import { Layout } from './components/Layout.tsx'
import { DashboardPage } from './pages/DashboardPage.tsx'
import { DeploymentPage } from './pages/DeploymentPage.tsx'
import { NewProjectPage } from './pages/NewProjectPage.tsx'
import { NewServerPage } from './pages/NewServerPage.tsx'
import { NotFoundPage } from './pages/NotFoundPage.tsx'
import { ProjectDetailPage } from './pages/ProjectDetailPage.tsx'
import { ProjectsPage } from './pages/ProjectsPage.tsx'
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
      { path: 'projects', element: <ProjectsPage /> },
      { path: 'projects/new', element: <NewProjectPage /> },
      { path: 'projects/:id', element: <ProjectDetailPage /> },
      { path: 'projects/:id/deployments/:deploymentId', element: <DeploymentPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])
