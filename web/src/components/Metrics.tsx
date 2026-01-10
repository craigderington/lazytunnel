import { useMemo } from 'react';
import { useTunnelStore } from '@/store/tunnelStore';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card';
import { Badge } from './ui/badge';
import {
  Activity,
  TrendingUp,
  ArrowUpCircle,
  ArrowDownCircle,
  Clock,
  Zap,
  CheckCircle,
  XCircle,
  AlertCircle,
  Database,
  Network,
  Timer,
  Circle
} from 'lucide-react';

// Helper function to format bytes
const formatBytes = (bytes: number): string => {
  if (bytes === 0) return '0 Bytes';
  const k = 1024;
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i];
};

// Helper function to format duration
const formatDuration = (seconds: number): string => {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
};

export function Metrics() {
  const { tunnels } = useTunnelStore();

  // Calculate aggregate metrics
  const metrics = useMemo(() => {
    const total = tunnels.length;
    const active = tunnels.filter(t => t.status === 'active').length;
    const failed = tunnels.filter(t => t.status === 'failed').length;
    const connecting = tunnels.filter(t => t.status === 'connecting').length;
    const stopped = tunnels.filter(t => t.status === 'stopped').length;
    const disconnected = tunnels.filter(t => t.status === 'disconnected').length;

    // Calculate average uptime (mock data for now - would come from metrics API)
    const now = Date.now();
    let totalUptime = 0;
    let totalPossibleUptime = 0;

    tunnels.forEach(tunnel => {
      const created = new Date(tunnel.createdAt).getTime();
      const age = (now - created) / 1000; // in seconds
      totalPossibleUptime += age;

      if (tunnel.status === 'active' && tunnel.lastConnected) {
        const lastConn = new Date(tunnel.lastConnected).getTime();
        const connectedTime = (now - lastConn) / 1000;
        totalUptime += Math.min(connectedTime, age);
      }
    });

    const avgUptimePercent = totalPossibleUptime > 0
      ? Math.round((totalUptime / totalPossibleUptime) * 100)
      : 0;

    // Calculate bandwidth (mock data)
    const totalBandwidth = active * 1024 * 1024 * Math.random() * 100; // Random for demo

    return {
      total,
      active,
      failed,
      connecting,
      stopped,
      disconnected,
      avgUptimePercent,
      totalBandwidth,
      healthScore: total > 0 ? Math.round((active / total) * 100) : 0,
    };
  }, [tunnels]);

  // Get tunnel breakdown by type
  const typeBreakdown = useMemo(() => {
    const breakdown = {
      local: 0,
      remote: 0,
      dynamic: 0,
    };

    tunnels.forEach(tunnel => {
      breakdown[tunnel.type]++;
    });

    return breakdown;
  }, [tunnels]);

  // Top tunnels by age
  const oldestTunnels = useMemo(() => {
    return [...tunnels]
      .sort((a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime())
      .slice(0, 5);
  }, [tunnels]);

  // Most recently connected
  const recentlyConnected = useMemo(() => {
    return [...tunnels]
      .filter(t => t.lastConnected)
      .sort((a, b) => {
        const aTime = a.lastConnected ? new Date(a.lastConnected).getTime() : 0;
        const bTime = b.lastConnected ? new Date(b.lastConnected).getTime() : 0;
        return bTime - aTime;
      })
      .slice(0, 5);
  }, [tunnels]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Activity className="h-8 w-8" />
        <div>
          <h1 className="text-3xl font-bold">Metrics</h1>
          <p className="text-muted-foreground">Performance and usage statistics</p>
        </div>
      </div>

      {/* Overview Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {/* Total Tunnels */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Tunnels</CardTitle>
            <Database className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{metrics.total}</div>
            <div className="flex gap-2 mt-2">
              <Badge variant="outline" className="text-xs">
                {metrics.active} active
              </Badge>
              {metrics.failed > 0 && (
                <Badge variant="destructive" className="text-xs">
                  {metrics.failed} failed
                </Badge>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Health Score */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Health Score</CardTitle>
            <Zap className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{metrics.healthScore}%</div>
            <p className="text-xs text-muted-foreground mt-2">
              {metrics.active} of {metrics.total} tunnels operational
            </p>
          </CardContent>
        </Card>

        {/* Average Uptime */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Avg Uptime</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{metrics.avgUptimePercent}%</div>
            <p className="text-xs text-muted-foreground mt-2">
              Since tunnel creation
            </p>
          </CardContent>
        </Card>

        {/* Total Bandwidth */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Est. Bandwidth</CardTitle>
            <TrendingUp className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatBytes(metrics.totalBandwidth)}</div>
            <p className="text-xs text-muted-foreground mt-2">
              Approximate total usage
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Status Breakdown */}
      <Card>
        <CardHeader>
          <CardTitle>Tunnel Status Distribution</CardTitle>
          <CardDescription>Current state of all managed tunnels</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
            <div className="flex items-center gap-3 p-4 border rounded-lg">
              <CheckCircle className="h-8 w-8 text-green-500" />
              <div>
                <p className="text-2xl font-bold">{metrics.active}</p>
                <p className="text-sm text-muted-foreground">Active</p>
              </div>
            </div>

            <div className="flex items-center gap-3 p-4 border rounded-lg">
              <Clock className="h-8 w-8 text-blue-500" />
              <div>
                <p className="text-2xl font-bold">{metrics.connecting}</p>
                <p className="text-sm text-muted-foreground">Connecting</p>
              </div>
            </div>

            <div className="flex items-center gap-3 p-4 border rounded-lg">
              <Circle className="h-8 w-8 text-gray-500" />
              <div>
                <p className="text-2xl font-bold">{metrics.stopped}</p>
                <p className="text-sm text-muted-foreground">Stopped</p>
              </div>
            </div>

            <div className="flex items-center gap-3 p-4 border rounded-lg">
              <AlertCircle className="h-8 w-8 text-yellow-500" />
              <div>
                <p className="text-2xl font-bold">{metrics.disconnected}</p>
                <p className="text-sm text-muted-foreground">Disconnected</p>
              </div>
            </div>

            <div className="flex items-center gap-3 p-4 border rounded-lg">
              <XCircle className="h-8 w-8 text-red-500" />
              <div>
                <p className="text-2xl font-bold">{metrics.failed}</p>
                <p className="text-sm text-muted-foreground">Failed</p>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Tunnel Type Distribution */}
      <Card>
        <CardHeader>
          <CardTitle>Tunnel Type Distribution</CardTitle>
          <CardDescription>Breakdown by tunnel forwarding type</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div className="flex items-center justify-between p-4 border rounded-lg">
              <div className="flex items-center gap-3">
                <ArrowDownCircle className="h-6 w-6 text-blue-500" />
                <div>
                  <p className="font-medium">Local Forward</p>
                  <p className="text-sm text-muted-foreground">Local → Remote</p>
                </div>
              </div>
              <p className="text-2xl font-bold">{typeBreakdown.local}</p>
            </div>

            <div className="flex items-center justify-between p-4 border rounded-lg">
              <div className="flex items-center gap-3">
                <ArrowUpCircle className="h-6 w-6 text-purple-500" />
                <div>
                  <p className="font-medium">Remote Forward</p>
                  <p className="text-sm text-muted-foreground">Remote → Local</p>
                </div>
              </div>
              <p className="text-2xl font-bold">{typeBreakdown.remote}</p>
            </div>

            <div className="flex items-center justify-between p-4 border rounded-lg">
              <div className="flex items-center gap-3">
                <Network className="h-6 w-6 text-green-500" />
                <div>
                  <p className="font-medium">Dynamic (SOCKS5)</p>
                  <p className="text-sm text-muted-foreground">Proxy server</p>
                </div>
              </div>
              <p className="text-2xl font-bold">{typeBreakdown.dynamic}</p>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Two Column Layout for Lists */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Oldest Tunnels */}
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <Timer className="h-5 w-5" />
              <CardTitle>Longest Running</CardTitle>
            </div>
            <CardDescription>Tunnels by age (oldest first)</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              {oldestTunnels.length === 0 ? (
                <p className="text-sm text-muted-foreground text-center py-4">
                  No tunnels available
                </p>
              ) : (
                oldestTunnels.map((tunnel) => {
                  const age = Date.now() - new Date(tunnel.createdAt).getTime();
                  const ageInSeconds = Math.floor(age / 1000);

                  return (
                    <div
                      key={tunnel.id}
                      className="flex items-center justify-between p-3 border rounded-lg hover:bg-accent transition-colors"
                    >
                      <div className="flex-1 min-w-0">
                        <p className="font-medium truncate">{tunnel.name}</p>
                        <p className="text-xs text-muted-foreground">
                          {tunnel.type} tunnel
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        <Badge
                          variant={
                            tunnel.status === 'active'
                              ? 'default'
                              : tunnel.status === 'failed'
                              ? 'destructive'
                              : 'secondary'
                          }
                        >
                          {formatDuration(ageInSeconds)}
                        </Badge>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </CardContent>
        </Card>

        {/* Recently Connected */}
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <Zap className="h-5 w-5" />
              <CardTitle>Recently Active</CardTitle>
            </div>
            <CardDescription>Most recently connected tunnels</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              {recentlyConnected.length === 0 ? (
                <p className="text-sm text-muted-foreground text-center py-4">
                  No recently connected tunnels
                </p>
              ) : (
                recentlyConnected.map((tunnel) => {
                  const lastConn = tunnel.lastConnected
                    ? Date.now() - new Date(tunnel.lastConnected).getTime()
                    : 0;
                  const lastConnSeconds = Math.floor(lastConn / 1000);

                  return (
                    <div
                      key={tunnel.id}
                      className="flex items-center justify-between p-3 border rounded-lg hover:bg-accent transition-colors"
                    >
                      <div className="flex-1 min-w-0">
                        <p className="font-medium truncate">{tunnel.name}</p>
                        <p className="text-xs text-muted-foreground">
                          {tunnel.remoteHost}:{tunnel.remotePort}
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        <Badge
                          variant={
                            tunnel.status === 'active'
                              ? 'default'
                              : tunnel.status === 'failed'
                              ? 'destructive'
                              : 'secondary'
                          }
                        >
                          {formatDuration(lastConnSeconds)} ago
                        </Badge>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
