# New API Electron Desktop App

This directory contains the Electron wrapper for New API, providing a native desktop application with system tray support for Windows, macOS, and Linux.

## Prerequisites

### 1. Go Binary (Required)
The Electron app requires the compiled Go binary to function. You have two options:

The binary must be built with the stable Go release required by the root
`go.mod` or newer (currently **Go 1.27.3**). Both the input binary and the copy
inside the packaged app are checked by electron-builder hooks. The check reads
Go build information from PE, ELF, or Mach-O files without running the backend
or requiring a local Go installation. Old, missing, prerelease, and unreadable
build information stops packaging.

**Option A: Use existing binary (without Go installed)**
```bash
# If you have a pre-built binary (e.g., new-api-macos)
cp ../new-api-macos ../new-api
```

**Option B: Build from source (requires Go 1.27.3 or newer)**

From the repository root, build the frontend in `web/classic` first, then run
`go build -o new-api` on macOS/Linux or `go build -o new-api.exe` on Windows.
The binary must match the desktop app's target OS and architecture.

### 2. Electron Dependencies

Use **Node.js 24.22.1 or a newer 24.x release**. The local npm configuration and
packaging hook enforce this security baseline, including direct
`electron-builder` invocations.

```bash
cd electron
npm ci
```

The postinstall command installs the locked Electron runtime and its license
files. Packaging obtains the target Electron ZIP using the SHA-256 checksum
embedded in that exact npm package, verifies it, and passes the local ZIP to
electron-builder. It never trusts a checksum downloaded alongside the ZIP.
Cached archives are verified too. Custom Electron versions must be updated in
`package.json` and `package-lock.json` together.

## Development

Run the app in development mode:
```bash
npm run dev-app
```

This will:
- Wait for a separately started Go backend and the Vite dev server on port 5174
- Open an Electron window with DevTools enabled
- Create a system tray icon (menu bar on macOS)
- Use the database configured for that separately started backend

## Building for Production

### Quick Build
```bash
# Ensure Go binary exists in parent directory
ls ../new-api  # Should exist

# Build for current platform
npm run build

# Platform-specific builds
npm run build:mac    # Creates .dmg and .zip
npm run build:win    # Creates .exe installer
npm run build:linux  # Creates .AppImage and .deb
```

`npm test` checks fail-closed handling of invalid backend metadata. Set
`NEW_API_GO_FIXTURES` to a directory containing actual cross-compiled
`windows-old`, `linux-old`, `darwin-old` (Go 1.25.1) and corresponding `*-safe`
files (the current Go baseline) to also run the platform and packaged-copy
regression checks. These fixtures are only read, never executed.

### Build Output
- Built applications are in `electron/dist/`
- macOS: `.dmg` (installer) and `.zip` (portable)
- Windows: `.exe` (installer) and portable exe
- Linux: `.AppImage` and `.deb`

## Configuration

### Port
Default port is 3000. To change, edit `main.js`:
```javascript
const PORT = 3000; // Change to desired port
```

### Database Location
- **Development**: `../data/new-api.db` (project directory)
- **Production**:
  - macOS: `~/Library/Application Support/New API/data/`
  - Windows: `%APPDATA%/New API/data/`
  - Linux: `~/.config/New API/data/`
