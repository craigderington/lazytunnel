import { useState } from 'react';
import { useSettingsStore } from '@/store/settingsStore';
import { useThemeStore } from '@/store/themeStore';
import { useTunnelStore } from '@/store/tunnelStore';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card';
import { Label } from './ui/label';
import { Input } from './ui/input';
import { Switch } from './ui/switch';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import {
  Settings as SettingsIcon,
  Server,
  RefreshCw,
  Bell,
  Volume2,
  Database,
  RotateCcw,
  Save,
  Moon,
  Sun
} from 'lucide-react';

export function Settings() {
  const { settings, updateSettings, resetSettings } = useSettingsStore();
  const { theme, toggleTheme } = useThemeStore();
  const { isDemoMode, setDemoMode } = useTunnelStore();
  const [localSettings, setLocalSettings] = useState(settings);
  const [hasChanges, setHasChanges] = useState(false);

  const handleChange = (key: keyof typeof settings, value: string | number | boolean) => {
    setLocalSettings((prev) => ({ ...prev, [key]: value }));
    setHasChanges(true);
  };

  const handleSave = () => {
    updateSettings(localSettings);
    setHasChanges(false);
  };

  const handleReset = () => {
    if (confirm('Are you sure you want to reset all settings to defaults?')) {
      resetSettings();
      setLocalSettings(settings);
      setHasChanges(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <SettingsIcon className="h-8 w-8" />
          <div>
            <h1 className="text-3xl font-bold">Settings</h1>
            <p className="text-muted-foreground">Configure your lazytunnel preferences</p>
          </div>
        </div>
        <div className="flex gap-2">
          {hasChanges && (
            <Badge variant="outline" className="bg-yellow-500/10 text-yellow-500 border-yellow-500/20">
              Unsaved Changes
            </Badge>
          )}
        </div>
      </div>

      {/* API Configuration */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Server className="h-5 w-5" />
            <CardTitle>API Configuration</CardTitle>
          </div>
          <CardDescription>
            Configure the backend API endpoint
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="apiBaseUrl">API Base URL</Label>
            <Input
              id="apiBaseUrl"
              type="url"
              value={localSettings.apiBaseUrl}
              onChange={(e) => handleChange('apiBaseUrl', e.target.value)}
              placeholder="http://localhost:8080/api/v1"
            />
            <p className="text-sm text-muted-foreground">
              The base URL for the lazytunnel API server
            </p>
          </div>
        </CardContent>
      </Card>

      {/* Refresh & Polling */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <RefreshCw className="h-5 w-5" />
            <CardTitle>Refresh & Polling</CardTitle>
          </div>
          <CardDescription>
            Control how often the UI updates tunnel status
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="autoRefreshInterval">Auto Refresh Interval (seconds)</Label>
            <Input
              id="autoRefreshInterval"
              type="number"
              min="1"
              max="60"
              value={localSettings.autoRefreshInterval}
              onChange={(e) => handleChange('autoRefreshInterval', parseInt(e.target.value) || 5)}
            />
            <p className="text-sm text-muted-foreground">
              How often to poll the API for tunnel status updates (1-60 seconds)
            </p>
          </div>
        </CardContent>
      </Card>

      {/* Notifications */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Bell className="h-5 w-5" />
            <CardTitle>Notifications</CardTitle>
          </div>
          <CardDescription>
            Configure notification preferences
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label htmlFor="enableNotifications">Enable Notifications</Label>
              <p className="text-sm text-muted-foreground">
                Show browser notifications for tunnel events
              </p>
            </div>
            <Switch
              id="enableNotifications"
              checked={localSettings.enableNotifications}
              onCheckedChange={(checked) => handleChange('enableNotifications', checked)}
            />
          </div>

          <div className="flex items-center justify-between">
            <div className="space-y-0.5 flex items-center gap-2">
              <Volume2 className="h-4 w-4" />
              <div>
                <Label htmlFor="enableSounds">Enable Sounds</Label>
                <p className="text-sm text-muted-foreground">
                  Play sound alerts for important events
                </p>
              </div>
            </div>
            <Switch
              id="enableSounds"
              checked={localSettings.enableSounds}
              onCheckedChange={(checked) => handleChange('enableSounds', checked)}
            />
          </div>
        </CardContent>
      </Card>

      {/* Tunnel Defaults */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Database className="h-5 w-5" />
            <CardTitle>Tunnel Defaults</CardTitle>
          </div>
          <CardDescription>
            Default settings for newly created tunnels
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="maxRetries">Max Retries</Label>
            <Input
              id="maxRetries"
              type="number"
              min="0"
              max="10"
              value={localSettings.maxRetries}
              onChange={(e) => handleChange('maxRetries', parseInt(e.target.value) || 3)}
            />
            <p className="text-sm text-muted-foreground">
              Default maximum reconnection attempts (0-10)
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="defaultKeepAlive">Keep Alive Interval (seconds)</Label>
            <Input
              id="defaultKeepAlive"
              type="number"
              min="10"
              max="300"
              value={localSettings.defaultKeepAlive}
              onChange={(e) => handleChange('defaultKeepAlive', parseInt(e.target.value) || 30)}
            />
            <p className="text-sm text-muted-foreground">
              Default SSH keep-alive interval (10-300 seconds)
            </p>
          </div>

          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label htmlFor="defaultAutoReconnect">Auto Reconnect</Label>
              <p className="text-sm text-muted-foreground">
                Enable auto-reconnect for new tunnels by default
              </p>
            </div>
            <Switch
              id="defaultAutoReconnect"
              checked={localSettings.defaultAutoReconnect}
              onCheckedChange={(checked) => handleChange('defaultAutoReconnect', checked)}
            />
          </div>
        </CardContent>
      </Card>

      {/* Appearance */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            {theme === 'dark' ? <Moon className="h-5 w-5" /> : <Sun className="h-5 w-5" />}
            <CardTitle>Appearance</CardTitle>
          </div>
          <CardDescription>
            Customize the look and feel
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label>Theme</Label>
              <p className="text-sm text-muted-foreground">
                Current theme: {theme === 'dark' ? 'Dark' : 'Light'}
              </p>
            </div>
            <Button onClick={toggleTheme} variant="outline" size="sm">
              {theme === 'dark' ? <Sun className="h-4 w-4 mr-2" /> : <Moon className="h-4 w-4 mr-2" />}
              Switch to {theme === 'dark' ? 'Light' : 'Dark'}
            </Button>
          </div>

          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label>Demo Mode</Label>
              <p className="text-sm text-muted-foreground">
                Use sample data instead of live API
              </p>
            </div>
            <Switch
              checked={isDemoMode}
              onCheckedChange={setDemoMode}
            />
          </div>
        </CardContent>
      </Card>

      {/* Action Buttons */}
      <div className="flex justify-between items-center pt-4 border-t">
        <Button
          onClick={handleReset}
          variant="outline"
          className="gap-2"
        >
          <RotateCcw className="h-4 w-4" />
          Reset to Defaults
        </Button>

        <Button
          onClick={handleSave}
          disabled={!hasChanges}
          className="gap-2"
        >
          <Save className="h-4 w-4" />
          Save Changes
        </Button>
      </div>
    </div>
  );
}
