import { useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Layout, type PageType } from './components/Layout'
import { TunnelList } from './components/TunnelList'
import { Monitoring } from './components/Monitoring'
import { Metrics } from './components/Metrics'
import { Settings } from './components/Settings'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 5000,
    },
  },
})

function App() {
  const [activePage, setActivePage] = useState<PageType>('tunnels')

  const renderPage = () => {
    switch (activePage) {
      case 'tunnels':
        return <TunnelList />
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
    <QueryClientProvider client={queryClient}>
      <Layout activePage={activePage} onPageChange={setActivePage}>
        {renderPage()}
      </Layout>
    </QueryClientProvider>
  )
}

export default App
