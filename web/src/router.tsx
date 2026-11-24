import { createBrowserRouter } from 'react-router'
import { AuthGate } from './components/AuthGate.tsx'
import { Layout } from './components/Layout.tsx'
import { InvitePage, LoginPage, SetupPage } from './pages/AuthPages.tsx'
import { DashboardPage } from './pages/DashboardPage.tsx'
import { DeploymentPage } from './pages/DeploymentPage.tsx'
import { NewProjectPage } from './pages/NewProjectPage.tsx'
import { NewServerPage } from './pages/NewServerPage.tsx'
import { NotFoundPage } from './pages/NotFoundPage.tsx'
import { ProjectDetailPage } from './pages/ProjectDetailPage.tsx'
import { ProjectsPage } from './pages/ProjectsPage.tsx'
import { ServerDetailPage } from './pages/ServerDetailPage.tsx'
import { ServersPage } from './pages/ServersPage.tsx'
import { SettingsPage } from './pages/SettingsPage.tsx'

export const router = createBrowserRouter([
  { path: 'login', element: <LoginPage /> },
  { path: 'setup', element: <SetupPage /> },
  { path: 'invite/:token', element: <InvitePage /> },
  {
    element: (
      <AuthGate>
        <Layout />
      </AuthGate>
    ),
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'servers', element: <ServersPage /> },
      { path: 'servers/new', element: <NewServerPage /> },
      { path: 'servers/:id', element: <ServerDetailPage /> },
      { path: 'projects', element: <ProjectsPage /> },
      { path: 'projects/new', element: <NewProjectPage /> },
      { path: 'projects/:id', element: <ProjectDetailPage /> },
      { path: 'projects/:id/deployments/:deploymentId', element: <DeploymentPage /> },
      { path: 'settings', element: <SettingsPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])
