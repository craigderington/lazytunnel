import { useEffect, useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Layout, type PageType } from './components/Layout'
import { TunnelList } from './components/TunnelList'
import { Monitoring } from './components/Monitoring'
import { Metrics } from './components/Metrics'
import { Settings } from './components/Settings'
import { Topology } from './components/Topology'
import { LoginPage } from './components/LoginPage'
import { useWebSocket } from './hooks/useWebSocket'
import { useAuthStore } from './store/authStore'
import { useTunnelStore } from './store/tunnelStore'
import { Loader2 } from 'lucide-react'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 5000,
    },
  },
})

function AuthenticatedApp() {
  const [activePage, setActivePage] = useState<PageType>('tunnels')
  useWebSocket()

  const renderPage = () => {
    switch (activePage) {
      case 'tunnels':
        return <TunnelList />
      case 'topology':
        return <Topology />
      case 'monitoring':
        return <Monitoring />
      case 'metrics':
        return <Metrics />
      case 'settings':
        return <Settings />
      default:
        return <TunnelList />
    }
  }

  return (
    <Layout activePage={activePage} onPageChange={setActivePage}>
      {renderPage()}
    </Layout>
  )
}

function AppGate() {
  const initialize = useAuthStore((s) => s.initialize)
  const isReady = useAuthStore((s) => s.isReady)
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const authRequired = useAuthStore((s) => s.authRequired)
  const isDemoMode = useTunnelStore((s) => s.isDemoMode)

  useEffect(() => {
    initialize()
  }, [initialize])

  if (!isReady) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <Loader2 className="h-10 w-10 animate-spin text-primary" />
      </div>
    )
  }

  if (authRequired && !isAuthenticated && !isDemoMode) {
    return <LoginPage />
  }

  return <AuthenticatedApp />
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AppGate />
    </QueryClientProvider>
  )
}