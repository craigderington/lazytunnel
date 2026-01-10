# lazytunnel Web UI

Modern React-based web interface for managing SSH tunnels.

## Stack

- **React 18** + **TypeScript**
- **Vite** - Lightning fast build tool
- **Tailwind CSS** + **shadcn/ui** - Beautiful, accessible UI components
- **Zustand** - Lightweight state management
- **React Query** - Server state management with automatic caching
- **React Hook Form** + **Zod** - Type-safe form validation

## Getting Started

### Installation

```bash
npm install
cp .env.example .env
```

### Development

```bash
npm run dev
# UI available at http://localhost:5173
```

### Build

```bash
npm run build
npm run preview
```

## Features Implemented

- Responsive dashboard layout with sidebar
- Real-time tunnel list with auto-refresh
- Create tunnel dialog with validation
- Start/stop/delete tunnel actions
- Color-coded status indicators
- Dark mode support

## License

MIT
