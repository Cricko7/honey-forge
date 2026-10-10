import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createBrowserRouter, Navigate } from 'react-router-dom'
import './index.css'
import { ThemeProvider } from '@/lib/theme'
import { Layout } from '@/components/Layout'
import { LoginPage } from '@/pages/Login'
import { RegisterPage } from '@/pages/Register'
import { DashboardPage } from '@/pages/Dashboard'
import { NetworkBuilderPage } from '@/pages/NetworkBuilder'
import { TrapsPage } from '@/pages/Traps'
import { TrapDetailPage } from '@/pages/TrapDetail'
import { EventsPage } from '@/pages/Events'
import { ProfilesPage } from '@/pages/Profiles'
import { CatalogPage } from '@/pages/Catalog'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 10_000, refetchOnWindowFocus: false, retry: 1 },
  },
})

const router = createBrowserRouter([
  { path: '/', element: <Navigate to="/login" replace /> },
  { path: '/login', element: <LoginPage /> },
  { path: '/register', element: <RegisterPage /> },
  {
    path: '/app',
    element: <Layout />,
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'network', element: <NetworkBuilderPage /> },
      { path: 'traps', element: <TrapsPage /> },
      { path: 'traps/:id', element: <TrapDetailPage /> },
      { path: 'events', element: <EventsPage /> },
      { path: 'profiles', element: <ProfilesPage /> },
      { path: 'catalog', element: <CatalogPage /> },
    ],
  },
])

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ThemeProvider>
  </React.StrictMode>,
)
