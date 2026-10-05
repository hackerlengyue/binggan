<div align="center">
  <img src="../frontend/public/binggan-logo.png" width="104" alt="Binggan logo" />
  <h1>Binggan</h1>
  <p><strong>饼干大小姐</strong></p>
  <p>Key capture · Playback orchestration · Video decryption · Media library</p>
  <p>
    <a href="https://github.com/hackerlengyue/binggan/actions/workflows/ci.yml"><img src="https://github.com/hackerlengyue/binggan/actions/workflows/ci.yml/badge.svg" alt="Build" /></a>
    <img src="https://img.shields.io/badge/version-2.0.0-f2b9c7?style=flat-square" alt="Version 2.0.0" />
    <img src="https://img.shields.io/badge/Go-1.27.1-00ADD8?style=flat-square" alt="Go 1.27.1" />
    <img src="https://img.shields.io/badge/MyGo-0.2.5-252525?style=flat-square" alt="MyGo 0.2.5" />
    <img src="https://img.shields.io/badge/Vue-3-42b883?style=flat-square" alt="Vue 3" />
  </p>
  <p><a href="../README.md">简体中文</a> · <strong>English</strong></p>
  <p><a href="#preview">Preview</a> · <a href="#features">Features</a> · <a href="#getting-started">Getting started</a> · <a href="#development">Development</a> · <a href="#acknowledgements">Acknowledgements</a></p>
</div>

---

Binggan is a desktop tool for local video processing. Capture and manage keys, queue lessons, decrypt `.sz` files in batches, and browse or play the exported MP4s in one workspace. Capture records, tasks, and settings are stored locally. The interface supports Simplified Chinese, English, and light and dark themes.

## Preview

![Data overview](assets/overview.png)

**[Watch the introduction](media/binggan-desktop-overview.mp4)** · 1 min 18 sec · 1920 × 1080

## Features

| Area | Capabilities |
| --- | --- |
| Capture | Start and stop local capture, inspect requests, and save key records. |
| Key management | Name, search, copy, export, and delete saved records. |
| Playback orchestration | Queue selected Sz lessons and track playback, capture, and record saving. |
| Video decryption | Import local `.sz` files and matching key JSON, run batch jobs, and export MP4. |
| Media library | Browse finished resources, view thumbnails, play video, and seek. |
| Settings | Manage certificates, processing settings, notifications, environment checks, and power settings. |
| Interface | Switch between Simplified Chinese and English, and between light and dark themes. |

### Platforms

| Platform | Current scope |
| --- | --- |
| macOS | Development and packaging are supported. Playback orchestration uses the native Accessibility driver. |
| Windows | Runtime not tested. Automated builds target x64 and ARM64; a native playback automation driver is not implemented. |
| Linux | Platform resource preparation and packaging are not configured. |

macOS permission flows, full queues in the real player, keeping the display awake for extended periods, lock-screen behavior, and packaged notifications still need manual validation.

Use capture and decryption with content you own or are authorized to process.

## Getting started

### Requirements

For development from source:

| Tool | Version and purpose |
| --- | --- |
| Go | `1.27.1+`, with the minimum declared in [go.mod](../go.mod), to compile the MyGo backend |
| Node.js | `22.12.0+`, with the minimum declared in [package.json](../package.json); use the bundled npm to install dependencies and run frontend and development scripts |
| macOS compiler tools | Xcode Command Line Tools, providing Clang and the system SDK for native compilation |

MyGo CLI `0.2.5` is installed by `npm ci`. The resource preparation command downloads FFmpeg and ffprobe for the selected platform; global installations are not needed.

**Packaging on macOS:** Swift `6.2+` and a compatible SDK are also required. See [macOS packaging](development/README.en.md#macos).

**Running a packaged application:** The development tools above are not required. The [application configuration](../mygo.config.ts) sets the minimum macOS deployment version to `13.0`.

**Windows:** WebView2 is required at runtime. Runtime behavior has not been tested; build commands are in the [development guide](development/README.en.md#windows-not-tested).

### Install and prepare

Run these commands from the project root. This example is for an Apple Silicon Mac:

```sh
npm ci
npm run setup
npm run prepare:resources -- --platform darwin-arm64
npm run dev
```

`npm run setup` installs frontend dependencies, downloads Go modules, and generates the frontend bindings. Pick the resource target for your machine:

| Machine | Resource target |
| --- | --- |
| Apple Silicon Mac | `darwin-arm64` |
| Intel Mac | `darwin-amd64` |
| Windows x64 (not tested) | `windows-amd64` |
| Windows ARM64 (not tested) | `windows-arm64` |

Platform tools are downloaded from the pinned source manifests and checked with SHA-256.

### Development workspace

Development data defaults to `.local/user-data/`. A new directory starts empty. Separate ports and WebView storage keep development apart from the installed application.

To start fresh, quit the development application, move that directory to a backup location, and run `npm run dev` again. See [data directories](development/README.en.md#data-directories) for custom paths.

## Development

The backend uses MyGo; the frontend uses Vue, shadcn-vue, and Tailwind. Generated bindings handle service calls, Channels carry live capture, and a custom Protocol serves media. SQLite stores settings and task records.

See the [documentation index](README.md) for all guides.

| Guide | Contents |
| --- | --- |
| [Development and packaging](development/README.en.md) | Commands, data directories, platform tools, tests, and application builds |
| [Frontend development](development/frontend.md#english) | UI project, component sources, and design conventions |
| [Contributing](CONTRIBUTING.md#english) | Bug reports, pull requests, and verification |
| [Security and privacy](SECURITY.md#english) | Sharing logs and reporting security issues |

After preparing the resources for your platform, run the full checks:

```sh
npm run check
```

Pushes to `main` and pull requests run checks and build ARM64 and x64 archives for macOS and Windows (runtime not tested). Download archives and SHA-256 checksums from the [workflow run](https://github.com/hackerlengyue/binggan/actions/workflows/ci.yml); artifacts are retained for 14 days. Version tags publish a [release](https://github.com/hackerlengyue/binggan/releases). See [Automated builds and releases](development/README.en.md#automated-builds-and-releases) for the steps.

<details>
<summary>Project layout</summary>

```text
binggan/
├── README.md                    Project guide
├── cmd/capture-service/         Capture helper
├── internal/                    Go services, storage, processing, and tests
├── frontend/                    Vue interface and generated MyGo client
├── native/permission-helper/    macOS permission helper source
├── resources/                   App icon, source manifests, and licenses
├── scripts/                     Development, packaging, and repository checks
├── docs/                        Documentation index, English README, and contribution notes
│   ├── development/             Development, packaging, and UI conventions
│   ├── third-party/             Dependency licenses and asset sources
│   ├── assets/                  Interface images
│   └── media/                   Introduction video
└── .github/                     CI and issue templates
```

</details>

## License

The main project license is undecided. See [Third-party notices](third-party/README.md#english) for dependency licenses, asset sources, and the status of the logo and video. Dependencies include GPL-3.0 components; their license files are retained.

## Acknowledgements

Thank you to the authors and contributors of these open-source projects:

| Project | Used in Binggan for | Links |
| --- | --- | --- |
| **MyGo** | Desktop windows, Go-to-frontend bindings, events, and application packaging. | [GitHub](https://github.com/egoist/mygo) |
| **shadcn-vue** | Vue interface components and layout references. | [Website](https://shadcn-vue.com/) · [GitHub](https://github.com/unovue/shadcn-vue) |
| **Apple TV Like Player** | The built-in video player in the media library. | [GitHub](https://github.com/doraFX/apple-tv-like-player) |
| **LXGW WenKai** | The font used on the startup screen. | [GitHub](https://github.com/lxgw/LxgwWenKai) |
| **PermissionFlow** | macOS permission setup guidance and helper panels. | [GitHub](https://github.com/jaywcjlove/PermissionFlow) |
| **sing-box** | TUN tunneling and routing for capture traffic. | [GitHub](https://github.com/SagerNet/sing-box) |
