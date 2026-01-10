# 🐛 Debugging lazytunnel UI

## 🎯 CRITICAL FIXES APPLIED (Just Now!)

### Demo Mode Fix
**Problem:** React Query was overwriting demo tunnels with API data every 5 seconds
**Solution:** Added `isDemoMode` flag to store that prevents API from fetching/overwriting when demo mode is active

### Form Submission Debugging
**Added:** Extensive console logging to track every step of form submission

## If buttons aren't working:

### 1. **MUST DO FIRST**: Hard Refresh Your Browser
- **Chrome/Firefox (Linux/Windows)**: `Ctrl + Shift + R`
- **Chrome/Firefox (Mac)**: `Cmd + Shift + R`
- **Safari**: `Cmd + Option + R`

⚠️ **This is CRITICAL!** The old JavaScript is cached. You must force refresh!

### 2. Open Browser Console
Press `F12` or right-click → "Inspect" → "Console" tab

You should see helpful emoji logs:

**Demo Mode:**
- 🎭 Toggle demo mode clicked
- ✅ Enabling demo mode with 8 tunnels (plus a table)
- ✨ Demo mode enabled!
- 🔍 TunnelList Debug: {tunnels: 8, isDemoMode: true, ...}

**Create Tunnel:**
- 🖱️ New Tunnel button clicked
- 🚇 Create Tunnel Dialog: OPENING
- 🔘 Submit button clicked!
- 📋 Form submit event triggered
- ✅ Form validation passed! (or ❌ Form validation FAILED)
- 📝 Form submitted with data: {...}
- 🚀 Sending create tunnel request: {...}
- ✅ Tunnel created successfully

### 3. Check What's Loaded

In the console, type:
```javascript
// Check if React Query is working
window.__REACT_QUERY_DEVTOOLS_GLOBAL_HOOK__

// Check tunnel store
localStorage.getItem('tunnel-store')

// Check theme
document.documentElement.classList.contains('dark')
```

### 4. Common Issues

**Demo Mode button does nothing:**
- ✅ **FIXED!** React Query was overwriting demo data - now protected with `isDemoMode` flag
- After hard refresh, should see 8 demo tunnels immediately
- Check console for: "🔍 TunnelList Debug: {tunnels: 8, isDemoMode: true}"

**New Tunnel form won't submit:**
- Check console for "🔘 Submit button clicked!"
  - If you DON'T see this, the button isn't responding to clicks
  - If you DO see it, check next log: "📋 Form submit event triggered"
- Check for validation errors: "❌ Form validation FAILED"
  - An alert should show what fields are invalid
- Check for API errors: Look for "❌ Failed to create tunnel" with error message

**New Tunnel dialog won't open:**
- Check console for "🚇 Create Tunnel Dialog: OPENING" log
- Try clicking directly on the button text, not the icon

**Navigation items don't work:**
- They should show an alert: "Monitoring - Coming soon!"
- If no alert, check console for errors

### 5. Nuclear Option - Clear Everything

```javascript
// In browser console:
localStorage.clear()
sessionStorage.clear()
location.reload(true)
```

Then hard refresh again.

### 6. Verify Dev Server

Make sure Vite dev server is running:
```bash
cd /home/cd/Work/lazytunnel/web
npm run dev
```

Should show: `➜ Local: http://localhost:5173/`

## Still Not Working?

1. Check Network tab in DevTools - any failed requests?
2. Any JavaScript errors in Console (red text)?
3. Try a different browser (Chrome, Firefox, Safari)
4. Check if the backend is running on port 8080

## Quick Test in Console

```javascript
// This should show 8 demo tunnels
console.log('DEMO_TUNNELS count:', 8)

// Force demo mode programmatically
window.location.reload()
```
