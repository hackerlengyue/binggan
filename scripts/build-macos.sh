#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
[[ $(uname -s) == Darwin ]] || { printf 'macOS bundle must be built on macOS\n' >&2; exit 1; }

target=${1:-arm64}
case "$target" in
  arm64|amd64|universal) ;;
  *) printf 'usage: %s [arm64|amd64|universal]\n' "$0" >&2; exit 2 ;;
esac

real_go=$(command -v go)
[[ $real_go = /* ]] || { printf 'go executable must resolve to an absolute path\n' >&2; exit 1; }

# MyGo CLI 0.2.5 unconditionally sets CGO_ENABLED=0 for its child Go builds.
# Only the macOS app needs cgo for our Accessibility driver; retain MyGo's
# generated bindings, frontend overlay, bundle metadata, resources and signing.
wrapper_dir=$(mktemp -d "${TMPDIR:-/tmp}/binggan-macos-go.XXXXXX")
trap 'rm -rf "$wrapper_dir"' EXIT
cat > "$wrapper_dir/go" <<'GO_WRAPPER'
#!/usr/bin/env bash
set -euo pipefail
exec env CGO_ENABLED=1 "$BINGGAN_REAL_GO" "$@"
GO_WRAPPER
chmod 755 "$wrapper_dir/go"
export BINGGAN_REAL_GO=$real_go
export PATH="$wrapper_dir:$PATH"

compiled=$(
  GOOS=darwin GOARCH=arm64 go list -f '{{join .CgoFiles " "}}' ./internal/playerautomation
)
[[ " $compiled " == *' automation_darwin.go '* ]] || {
  printf 'macOS Accessibility driver was not selected by Go build tags: %s\n' "$compiled" >&2
  exit 1
}

# PermissionFlow is a native SwiftUI/AppKit library. Build its small macOS-only
# host against the SDK available on this CLT installation, then let MyGo copy
# and sign it with the rest of the app resources.
permission_sdk=${BINGGAN_PERMISSION_SDK:-}
if [[ -z $permission_sdk ]]; then
  developer_dir=$(xcode-select -p)
  # CLT's macOS 27 SDK can reference SwiftUIMacros without shipping its
  # compiler plugin. Prefer the already validated 26.5 SDK when available.
  for candidate in \
    "$developer_dir/SDKs/MacOSX26.5.sdk" \
    "$developer_dir/Platforms/MacOSX.platform/Developer/SDKs/MacOSX26.5.sdk"; do
    if [[ -d $candidate ]]; then permission_sdk=$candidate; break; fi
  done
  if [[ -z $permission_sdk ]]; then permission_sdk=$(xcrun --sdk macosx --show-sdk-path); fi
fi
[[ -d "$permission_sdk" ]] || {
  printf 'PermissionFlow SDK is missing: %s\n' "$permission_sdk" >&2
  exit 1
}
build_permission_helper() {
  local arch=$1 triple binary_dir destination
  case "$arch" in
    arm64) triple=arm64-apple-macosx13.0 ;;
    amd64) triple=x86_64-apple-macosx13.0 ;;
  esac
  [[ -d "resources/darwin-$arch/tools" ]] || {
    printf 'Run npm run prepare:resources -- --platform darwin-%s first\n' "$arch" >&2
    exit 1
  }
  swift build --package-path native/permission-helper --configuration release \
    --triple "$triple" --sdk "$permission_sdk" --product binggan-permission-helper
  binary_dir=$(swift build --package-path native/permission-helper --configuration release \
    --triple "$triple" --sdk "$permission_sdk" --show-bin-path)
  destination="resources/darwin-$arch/tools/binggan-permission-helper"
  install -m 755 "$binary_dir/binggan-permission-helper" "$destination"
  [[ $(lipo -archs "$destination") == ${triple%%-*} ]] || {
    printf 'PermissionFlow helper has the wrong architecture: %s\n' "$destination" >&2
    exit 1
  }
  rm -rf resources/darwin/PermissionFlow_PermissionFlow.bundle
  mkdir -p resources/darwin
  ditto "$binary_dir/PermissionFlow_PermissionFlow.bundle" resources/darwin/PermissionFlow_PermissionFlow.bundle
}
case "$target" in
  arm64) build_permission_helper arm64 ;;
  amd64) build_permission_helper amd64 ;;
  universal) build_permission_helper arm64; build_permission_helper amd64 ;;
esac

npm run build -- -platform "darwin/$target" -skip-dmg

bundle="$PWD/build/darwin-$target/饼干大小姐.app"
# Keep a stable code requirement for the new Bundle ID across local rebuilds.
# MyGo's default ad-hoc requirement is a changing cdhash.
codesign --force --sign - --requirements '=designated => identifier "time.binggan.haomen.v2"' "$bundle"
case "$target" in
  universal) check_arch=arm64 ;;
  *) check_arch=$target ;;
esac
BINGGAN_MAC_BUNDLE="$bundle" BINGGAN_MAC_ARCH="$check_arch" "$real_go" test . -run '^(TestMacBundleIdentity|TestMacBundleIncludes(PlayerDriver|PermissionFlow))$' -count=1 -v
if [[ $target == universal ]]; then
  BINGGAN_MAC_BUNDLE="$bundle" BINGGAN_MAC_ARCH=amd64 "$real_go" test . -run '^(TestMacBundleIdentity|TestMacBundleIncludes(PlayerDriver|PermissionFlow))$' -count=1 -v
fi
helper="$bundle/Contents/Resources/tools/binggan-permission-helper"
bundle_resources="$bundle/Contents/Resources/PermissionFlow_PermissionFlow.bundle"
[[ -x "$helper" && -d "$bundle_resources" ]] || {
  printf 'PermissionFlow helper or localization resources were not packaged\n' >&2
  exit 1
}
codesign --verify --deep --strict "$bundle"
codesign --verify --strict "$helper"
