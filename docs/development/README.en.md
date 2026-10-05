# Development and packaging

[Project README](../README.en.md) · [Documentation](../README.md) · [简体中文](README.md)

## Start developing

Follow [Getting started](../README.en.md#getting-started) to install dependencies and prepare your platform tools. The root project contains the MyGo desktop shell and Go services; `frontend/` contains the Vue interface. The development launcher starts the native window and Vite together.

```sh
npm run dev
```

The root npm project, frontend npm project, and Go module each have their own lockfiles. MyGo CLI is pinned to `0.2.5`.

## Commands

Run these from the project root:

| Command | Purpose |
| --- | --- |
| `npm ci` | Install MyGo CLI |
| `npm run setup` | Install frontend dependencies, download Go modules, and generate bindings |
| `npm run dev` | Launch the development application |
| `npm run generate` | Regenerate the frontend client from Go services |
| `npm test` | Run Go and frontend tests |
| `npm --prefix frontend run build` | Type-check and build the frontend |
| `npm run check` | Generate bindings, test, build the frontend, and check project structure and files |
| `npm run check:repository` | Check repository files separately |

## Service communication

MyGo generates the client used by the frontend to call Go services. After changing a service method or binding, run `npm run generate` to update `frontend/src/mygo.ts`.

Generated bindings handle service calls, Channels carry live capture, and the `binggan-stream` Protocol serves media and attachments. Vite does not proxy business APIs. See [Frontend development](frontend.md#english) for component and layout conventions.

## Data directories

### Development and packaged applications

| Setting | Development default | Packaged default |
| --- | --- | --- |
| Data directory | `.local/user-data/` | `饼干大小姐 V2` under the OS application-data directory |
| Receiver port | `18788` | `18778` |
| Proxy port | `18789` | `18779` |
| Vite address | `127.0.0.1:5175` | Bundled frontend |

Override paths and ports with `BINGGAN_USER_DATA_DIR`, `BINGGAN_RECEIVER_PORT`, and `CAPTURE_PROXY_PORT`.

The launcher writes a `.workspace-id` in the data directory and derives a separate Bundle ID, isolating WebView storage. Keeping the directory keeps its identity; a new directory gets a new identity. The packaged application ID is `time.binggan.haomen.v2`.

To start empty, quit the application and move its development data directory to a backup location. The next launch creates a new directory. Set `BINGGAN_USER_DATA_DIR` to the backup to reopen the previous workspace.

## Platform tools

```sh
npm run prepare:resources -- --platform darwin-arm64
npm run prepare:resources -- --verify --platform darwin-arm64
```

Available targets are `darwin-arm64`, `darwin-amd64`, `windows-amd64`, `windows-arm64`, and `all`. Windows has not been tested. The script downloads FFmpeg, ffprobe, and license files from `resources/FFMPEG-SOURCES-*.json`. It checks source and extracted SHA-256 values and executable architecture. Darwin targets also compile the capture helper.

Downloads are cached in `.cache/resources/`; tools go in `resources/<platform>/tools/`. The test launcher adds the current platform tools to PATH. Direct Go test commands also need FFmpeg and ffprobe on PATH.

## Tests

```sh
npm run check
```

The full checks use this Go test scope:

```sh
go test . ./cmd/... ./internal/...
```

This avoids testing Go packages shipped inside frontend dependencies. After binding changes, check that the generated client is up to date.

### Optional native tests

Prepare macOS ARM64 tools and build the frontend before running the real WKWebView suite:

```sh
BINGGAN_MYGO_E2E=1 go test . -run '^TestMyGoWebView$' -count=1 -v
```

| Environment variable | Purpose |
| --- | --- |
| `BINGGAN_MYGO_E2E=1` | Run real WKWebView tests in temporary workspaces |
| `BINGGAN_MAC_BUNDLE` | Validate a specific application bundle |
| `BINGGAN_TEST_INSTALLED_SZPLAYER=1` | Inspect the installed player |
| `BINGGAN_SURGE_INTEGRATION=1` | Run native routing integration tests and restore configuration |

Regular tests use isolated environments. Permissions, full queues in the real player, certificate trust, and keeping the display awake for extended periods still need native validation. WebView and simulated-control tests do not replace those checks. Windows has not been tested.

## Packaging

### macOS

Packaging needs macOS, Go, Xcode Command Line Tools, Swift `6.2+`, and a compatible SDK. Development and packaging enable cgo for the Accessibility driver.

```sh
# Apple Silicon
npm run prepare:resources -- --platform darwin-arm64
bash scripts/build-macos.sh arm64

# Intel
npm run prepare:resources -- --platform darwin-amd64
bash scripts/build-macos.sh amd64

# Universal
npm run prepare:resources -- --platform darwin-arm64
npm run prepare:resources -- --platform darwin-amd64
bash scripts/build-macos.sh universal
```

The script builds the PermissionFlow helper and localization resources, then checks architecture, Bundle ID, and signatures. Output is `build/darwin-<target>/饼干大小姐.app`.

It prefers an installed `26.5` SDK in the developer tools directory, otherwise using the active SDK from `xcrun`. Override this with `BINGGAN_PERMISSION_SDK`. SDK `26.5` has been used to verify this build. If compilation reports a missing `SwiftUIMacros.StateMacro` plugin, check that the SDK and tools are compatible.

The script uses local ad-hoc signing. Public distribution needs Developer ID signing and notarization configured for its release channel.

### Windows (not tested)

Windows startup, WebView2, certificates, capture, and exports have not been tested. GitHub Actions provides x64 and ARM64 builds; the commands below build locally. Native playback orchestration is currently implemented only on macOS.

```sh
npm run prepare:resources -- --platform windows-amd64
npm run prepare:resources -- --platform windows-arm64
npm run build -- -platform windows/amd64,windows/arm64
```

## Automated builds and releases

The [build workflow](../../.github/workflows/ci.yml) first runs Go and frontend tests, type checks, generated-binding checks, and repository checks on macOS. After those pass, MyGo builds the applications.

| Trigger | Result |
| --- | --- |
| Push to `main` or open a pull request | Run checks and build macOS ARM64 / x64 and Windows ARM64 / x64 (runtime not tested). |
| Select Run workflow in Actions | Run the same process manually. |
| Push a version tag starting with `v` | Build all four archives and publish them with SHA-256 checksums to a GitHub release. |

Open a successful run in [Build](https://github.com/hackerlengyue/binggan/actions/workflows/ci.yml) and download its Artifacts. They are retained for 14 days. Tagged builds also publish their archives to [Releases](https://github.com/hackerlengyue/binggan/releases).

The tag must match the version in `package.json`: version `2.0.0` uses `v2.0.0`. When updating the version, change `package.json`, lockfiles, `frontend/package.json`, and `mygo.config.ts` together. Commit the changes before pushing the tag:

```sh
git tag v2.0.0
git push origin v2.0.0
```

macOS archives use ad-hoc signing and include the Accessibility driver, PermissionFlow, and their licenses. Windows runtime behavior has not been tested. CI does not enable optional tests requiring the real player, certificate trust, or system networking.
