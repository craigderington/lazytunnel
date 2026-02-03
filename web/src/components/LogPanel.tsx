import { useEffect, useState, useRef } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import {
  X,
  Download,
  RefreshCw,
  Terminal,
  ChevronDown,
  Search,
  Filter,
  AlertCircle,
  Info,
  AlertTriangle,
  CheckCircle2,
} from 'lucide-react';
import { Input } from './ui/input';

interface LogEntry {
  __REALTIME_TIMESTAMP: string;
  MESSAGE: string;
  PRIORITY: string;
  _SYSTEMD_UNIT?: string;
  SYSLOG_IDENTIFIER?: string;
}

interface LogPanelProps {
  isOpen: boolean;
  onClose: () => void;
}

export function LogPanel({ isOpen, onClose }: LogPanelProps) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [searchTerm, setSearchTerm] = useState('');
  const [filter, setFilter] = useState<'all' | 'error' | 'warning' | 'info'>('all');
  const logsEndRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<number | undefined>(undefined);

  const fetchLogs = async () => {
    try {
      setLoading(true);
      const response = await fetch('http://localhost:8080/api/v1/logs?lines=200');
      const data = await response.json();
      setLogs(data.logs || []);
    } catch (error) {
      console.error('Failed to fetch logs:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (isOpen) {
      fetchLogs();
    }
  }, [isOpen]);

  useEffect(() => {
    if (autoRefresh && isOpen) {
      intervalRef.current = window.setInterval(() => {
        fetchLogs();
      }, 3000);
    } else {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    }

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [autoRefresh, isOpen]);

  const scrollToBottom = () => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  };

  const downloadLogs = () => {
    const logText = logs
      .map((log) => {
        const timestamp = new Date(parseInt(log.__REALTIME_TIMESTAMP) / 1000);
        return `[${timestamp.toISOString()}] ${log.MESSAGE}`;
      })
      .join('\n');

    const blob = new Blob([logText], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `lazytunnel-logs-${new Date().toISOString()}.txt`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const getLogLevel = (priority: string): 'error' | 'warning' | 'info' | 'debug' => {
    const p = parseInt(priority);
    if (p <= 3) return 'error'; // Emergency, Alert, Critical, Error
    if (p <= 4) return 'warning'; // Warning
    if (p <= 6) return 'info'; // Notice, Info
    return 'debug'; // Debug
  };

  const getLogIcon = (priority: string) => {
    const level = getLogLevel(priority);
    switch (level) {
      case 'error':
        return <AlertCircle className="h-4 w-4 text-red-500" />;
      case 'warning':
        return <AlertTriangle className="h-4 w-4 text-yellow-500" />;
      case 'info':
        return <Info className="h-4 w-4 text-blue-500" />;
      default:
        return <CheckCircle2 className="h-4 w-4 text-gray-500" />;
    }
  };

  const filteredLogs = logs.filter((log) => {
    const level = getLogLevel(log.PRIORITY);
    const matchesFilter = filter === 'all' || level === filter;
    const matchesSearch =
      !searchTerm || log.MESSAGE.toLowerCase().includes(searchTerm.toLowerCase());
    return matchesFilter && matchesSearch;
  });

  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-x-0 bottom-0 z-50 animate-in slide-in-from-bottom duration-300 w-full"
      style={{ height: '70vh', maxHeight: '70vh' }}
    >
      <Card className="h-full rounded-t-lg rounded-b-none border-x-0 border-b-0 shadow-2xl">
        <CardHeader className="border-b bg-muted/30">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Terminal className="h-5 w-5" />
              <div>
                <CardTitle>System Logs</CardTitle>
                <CardDescription>lazytunnel-server.service logs from journalctl</CardDescription>
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
                {autoRefresh ? 'Auto' : 'Manual'}
              </Button>
              <Button variant="outline" size="sm" onClick={fetchLogs} disabled={loading}>
                <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
              </Button>
              <Button variant="outline" size="sm" onClick={downloadLogs}>
                <Download className="h-4 w-4" />
              </Button>
              <Button variant="outline" size="sm" onClick={scrollToBottom}>
                <ChevronDown className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="sm" onClick={onClose}>
                <X className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </CardHeader>

        <CardContent className="p-4 space-y-4 h-[calc(100%-5rem)] overflow-hidden flex flex-col relative">
          {/* Filters */}
          <div className="flex items-center gap-2">
            <div className="relative flex-1">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search logs..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="pl-9"
              />
            </div>
            <div className="flex items-center gap-2">
              <Filter className="h-4 w-4 text-muted-foreground" />
              <Button
                variant={filter === 'all' ? 'default' : 'outline'}
                size="sm"
                onClick={() => setFilter('all')}
              >
                All
              </Button>
              <Button
                variant={filter === 'error' ? 'destructive' : 'outline'}
                size="sm"
                onClick={() => setFilter('error')}
              >
                Errors
              </Button>
              <Button
                variant={filter === 'warning' ? 'secondary' : 'outline'}
                size="sm"
                onClick={() => setFilter('warning')}
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

          {/* Log entries */}
          <div className="flex-1 overflow-y-auto font-mono text-sm space-y-1 bg-black/5 dark:bg-black/20 rounded-lg p-3">
            {filteredLogs.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Terminal className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>No logs available</p>
                <p className="text-xs mt-1">Logs will appear here as they are generated</p>
              </div>
            ) : (
              filteredLogs.map((log, index) => {
                const timestamp = new Date(parseInt(log.__REALTIME_TIMESTAMP) / 1000);
                const level = getLogLevel(log.PRIORITY);

                return (
                  <div
                    key={index}
                    className={`flex items-start gap-2 p-2 rounded hover:bg-accent/50 transition-colors ${
                      level === 'error'
                        ? 'bg-red-500/5'
                        : level === 'warning'
                        ? 'bg-yellow-500/5'
                        : ''
                    }`}
                  >
                    <div className="mt-0.5">{getLogIcon(log.PRIORITY)}</div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1">
                        <span className="text-xs text-muted-foreground">
                          {timestamp.toLocaleTimeString()}
                        </span>
                        <Badge variant="outline" className="text-xs">
                          {level.toUpperCase()}
                        </Badge>
                        {log.SYSLOG_IDENTIFIER && (
                          <Badge variant="secondary" className="text-xs">
                            {log.SYSLOG_IDENTIFIER}
                          </Badge>
                        )}
                      </div>
                      <p className="text-sm break-words whitespace-pre-wrap">{log.MESSAGE}</p>
                    </div>
                  </div>
                );
              })
            )}
            <div ref={logsEndRef} />
          </div>

          {/* Stats */}
          <div className="flex items-center justify-between text-xs text-muted-foreground border-t pt-2">
            <span>
              Showing {filteredLogs.length} of {logs.length} logs
            </span>
            {logs.length > 0 && (
              <span>
                Last updated: {new Date(parseInt(logs[0]?.__REALTIME_TIMESTAMP) / 1000).toLocaleString()}
              </span>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
