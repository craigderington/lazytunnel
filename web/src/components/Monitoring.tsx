import { useMemo, useState } from 'react';
import { useTunnelStore } from '@/store/tunnelStore';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import {
  Activity,
  Server,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  Clock,
  RefreshCw,
  Wifi,
  WifiOff,
  Zap,
  Circle,
  TrendingUp,
  TrendingDown,
  Minus
} from 'lucide-react';

interface Event {
  id: string;
  type: 'created' | 'connected' | 'disconnected' | 'failed' | 'deleted' | 'error';
  timestamp: Date;
  tunnelName: string;
  tunnelId: string;
  message: string;
}

// Helper to generate mock events from tunnel data
const generateEventsFromTunnels = (tunnels: any[]): Event[] => {
  const events: Event[] = [];

  tunnels.forEach((tunnel) => {
    // Created event
    events.push({
      id: `${tunnel.id}-created`,
      type: 'created',
      timestamp: new Date(tunnel.createdAt),
      tunnelName: tunnel.name,
      tunnelId: tunnel.id,
      message: `Tunnel "${tunnel.name}" was created`,
    });

    // Connection events
    if (tunnel.lastConnected) {
      events.push({
        id: `${tunnel.id}-connected`,
        type: 'connected',
        timestamp: new Date(tunnel.lastConnected),
        tunnelName: tunnel.name,
        tunnelId: tunnel.id,
        message: `Tunnel "${tunnel.name}" established connection`,
      });
    }

    // Status-based events
    if (tunnel.status === 'failed') {
      events.push({
        id: `${tunnel.id}-failed`,
        type: 'failed',
        timestamp: new Date(tunnel.updatedAt),
        tunnelName: tunnel.name,
        tunnelId: tunnel.id,
        message: tunnel.errorMessage || `Tunnel "${tunnel.name}" failed`,
      });
    } else if (tunnel.status === 'disconnected') {
      events.push({
        id: `${tunnel.id}-disconnected`,
        type: 'disconnected',
        timestamp: new Date(tunnel.updatedAt),
        tunnelName: tunnel.name,
        tunnelId: tunnel.id,
        message: `Tunnel "${tunnel.name}" disconnected`,
      });
    }
  });

  return events.sort((a, b) => b.timestamp.getTime() - a.timestamp.getTime());
};

const formatTimestamp = (date: Date): string => {
  const now = Date.now();
  const diff = now - date.getTime();
  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (seconds < 60) return `${seconds}s ago`;
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  return `${days}d ago`;
};

export function Monitoring() {
  const { tunnels } = useTunnelStore();
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [filter, setFilter] = useState<'all' | 'errors' | 'warnings' | 'info'>('all');

  // Generate events from tunnel data
  const allEvents = useMemo(() => generateEventsFromTunnels(tunnels), [tunnels]);

  // Filter events
  const filteredEvents = useMemo(() => {
    if (filter === 'all') return allEvents;
    if (filter === 'errors') return allEvents.filter(e => e.type === 'failed' || e.type === 'error');
    if (filter === 'warnings') return allEvents.filter(e => e.type === 'disconnected');
    return allEvents.filter(e => e.type === 'created' || e.type === 'connected');
  }, [allEvents, filter]);

  // System health metrics
  const systemHealth = useMemo(() => {
    const total = tunnels.length;
    const active = tunnels.filter(t => t.status === 'active').length;
    const failed = tunnels.filter(t => t.status === 'failed').length;
    const disconnected = tunnels.filter(t => t.status === 'disconnected').length;
    const stopped = tunnels.filter(t => t.status === 'stopped').length;
    const connecting = tunnels.filter(t => t.status === 'connecting').length;

    let status: 'healthy' | 'degraded' | 'critical';
    let statusText: string;

    if (failed > 0 || total === 0) {
      status = 'critical';
      statusText = 'System has critical issues';
    } else if (disconnected > total * 0.3) {
      status = 'degraded';
      statusText = 'System performance degraded';
    } else {
      status = 'healthy';
      statusText = 'All systems operational';
    }

    return {
      status,
      statusText,
      total,
      active,
      failed,
      disconnected,
      stopped,
      connecting,
      successRate: total > 0 ? Math.round((active / total) * 100) : 0,
    };
  }, [tunnels]);

  // Get event icon and color
  const getEventIcon = (type: Event['type']) => {
    switch (type) {
      case 'created':
        return <Circle className="h-4 w-4 text-blue-500" />;
      case 'connected':
        return <CheckCircle2 className="h-4 w-4 text-green-500" />;
      case 'disconnected':
        return <WifiOff className="h-4 w-4 text-yellow-500" />;
      case 'failed':
      case 'error':
        return <XCircle className="h-4 w-4 text-red-500" />;
      case 'deleted':
        return <Minus className="h-4 w-4 text-gray-500" />;
      default:
        return <Circle className="h-4 w-4 text-gray-500" />;
    }
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Activity className="h-8 w-8" />
          <div>
            <h1 className="text-3xl font-bold">Monitoring</h1>
            <p className="text-muted-foreground">Real-time system health and events</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant={autoRefresh ? 'default' : 'outline'}
            size="sm"
            onClick={() => setAutoRefresh(!autoRefresh)}
            className="gap-2"
          >
            <RefreshCw className={`h-4 w-4 ${autoRefresh ? 'animate-spin' : ''}`} />
            {autoRefresh ? 'Auto-refresh On' : 'Auto-refresh Off'}
          </Button>
        </div>
      </div>

      {/* System Health Overview */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Server className="h-5 w-5" />
              <CardTitle>System Health</CardTitle>
            </div>
            <Badge
              variant={
                systemHealth.status === 'healthy'
                  ? 'default'
                  : systemHealth.status === 'degraded'
                  ? 'secondary'
                  : 'destructive'
              }
              className="gap-2"
            >
              {systemHealth.status === 'healthy' ? (
                <CheckCircle2 className="h-3 w-3" />
              ) : systemHealth.status === 'degraded' ? (
                <AlertTriangle className="h-3 w-3" />
              ) : (
                <XCircle className="h-3 w-3" />
              )}
              {systemHealth.status.toUpperCase()}
            </Badge>
          </div>
          <CardDescription>{systemHealth.statusText}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 md:grid-cols-6 gap-4">
            <div className="text-center p-4 border rounded-lg">
              <Server className="h-6 w-6 mx-auto mb-2 text-muted-foreground" />
              <p className="text-2xl font-bold">{systemHealth.total}</p>
              <p className="text-xs text-muted-foreground">Total</p>
            </div>

            <div className="text-center p-4 border rounded-lg bg-green-500/10">
              <Wifi className="h-6 w-6 mx-auto mb-2 text-green-500" />
              <p className="text-2xl font-bold text-green-500">{systemHealth.active}</p>
              <p className="text-xs text-muted-foreground">Active</p>
            </div>

            <div className="text-center p-4 border rounded-lg bg-blue-500/10">
              <Clock className="h-6 w-6 mx-auto mb-2 text-blue-500" />
              <p className="text-2xl font-bold text-blue-500">{systemHealth.connecting}</p>
              <p className="text-xs text-muted-foreground">Connecting</p>
            </div>

            <div className="text-center p-4 border rounded-lg bg-gray-500/10">
              <Circle className="h-6 w-6 mx-auto mb-2 text-gray-500" />
              <p className="text-2xl font-bold text-gray-500">{systemHealth.stopped}</p>
              <p className="text-xs text-muted-foreground">Stopped</p>
            </div>

            <div className="text-center p-4 border rounded-lg bg-yellow-500/10">
              <WifiOff className="h-6 w-6 mx-auto mb-2 text-yellow-500" />
              <p className="text-2xl font-bold text-yellow-500">{systemHealth.disconnected}</p>
              <p className="text-xs text-muted-foreground">Disconnected</p>
            </div>

            <div className="text-center p-4 border rounded-lg bg-red-500/10">
              <XCircle className="h-6 w-6 mx-auto mb-2 text-red-500" />
              <p className="text-2xl font-bold text-red-500">{systemHealth.failed}</p>
              <p className="text-xs text-muted-foreground">Failed</p>
            </div>
          </div>

          <div className="mt-4 pt-4 border-t">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Zap className="h-5 w-5 text-muted-foreground" />
                <span className="text-sm font-medium">Success Rate</span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-2xl font-bold">{systemHealth.successRate}%</span>
                {systemHealth.successRate >= 90 ? (
                  <TrendingUp className="h-5 w-5 text-green-500" />
                ) : systemHealth.successRate >= 50 ? (
                  <Minus className="h-5 w-5 text-yellow-500" />
                ) : (
                  <TrendingDown className="h-5 w-5 text-red-500" />
                )}
              </div>
            </div>
            <div className="mt-2 h-2 bg-muted rounded-full overflow-hidden">
              <div
                className={`h-full transition-all ${
                  systemHealth.successRate >= 90
                    ? 'bg-green-500'
                    : systemHealth.successRate >= 50
                    ? 'bg-yellow-500'
                    : 'bg-red-500'
                }`}
                style={{ width: `${systemHealth.successRate}%` }}
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Event Stream */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>Event Stream</CardTitle>
              <CardDescription>Recent tunnel activity and system events</CardDescription>
            </div>
            <div className="flex gap-2">
              <Button
                variant={filter === 'all' ? 'default' : 'outline'}
                size="sm"
                onClick={() => setFilter('all')}
              >
                All ({allEvents.length})
              </Button>
              <Button
                variant={filter === 'errors' ? 'destructive' : 'outline'}
                size="sm"
                onClick={() => setFilter('errors')}
              >
                Errors
              </Button>
              <Button
                variant={filter === 'warnings' ? 'secondary' : 'outline'}
                size="sm"
                onClick={() => setFilter('warnings')}
              >
                Warnings
              </Button>
              <Button
                variant={filter === 'info' ? 'default' : 'outline'}
                size="sm"
                onClick={() => setFilter('info')}
              >
                Info
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-2 max-h-[600px] overflow-y-auto">
            {filteredEvents.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Activity className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>No events to display</p>
                <p className="text-sm mt-1">Events will appear here as tunnels are managed</p>
              </div>
            ) : (
              filteredEvents.map((event) => (
                <div
                  key={event.id}
                  className="flex items-start gap-3 p-3 border rounded-lg hover:bg-accent transition-colors"
                >
                  <div className="mt-1">{getEventIcon(event.type)}</div>
                  <div className="flex-1 min-w-0">
                    <p className="font-medium truncate">{event.message}</p>
                    <div className="flex items-center gap-2 mt-1">
                      <Badge variant="outline" className="text-xs">
                        {event.tunnelName}
                      </Badge>
                      <span className="text-xs text-muted-foreground">
                        {formatTimestamp(event.timestamp)}
                      </span>
                    </div>
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {event.timestamp.toLocaleTimeString()}
                  </div>
                </div>
              ))
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
