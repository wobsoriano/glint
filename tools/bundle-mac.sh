#!/bin/sh
# Builds Glint into target/<profile>/Glint.app and signs it.
#
# macOS keys Screen Recording, Input Monitoring, Microphone and Camera grants to the code
# signature, so a binary run with `cargo run` asks again after every rebuild. Signing the bundle
# with a stable identity keeps the grants:
#
#   GLINT_SIGN_IDENTITY="Apple Development: Name (TEAMID)" tools/bundle-mac.sh --release
#
# Without GLINT_SIGN_IDENTITY the bundle is signed ad hoc, which still carries the usage strings
# the permission prompts need. Pass --release for a release build.
set -e
cd "$(dirname "$0")/.."
profile=debug
if [ "$1" = "--release" ]; then
    profile=release
    cargo build --release
else
    cargo build
fi
version=$(sed -n 's/^version = "\(.*\)"/\1/p' Cargo.toml | head -1)
app="target/$profile/Glint.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS"
cp "target/$profile/glint" "$app/Contents/MacOS/glint"
cat > "$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>Glint</string>
    <key>CFBundleDisplayName</key>
    <string>Glint</string>
    <key>CFBundleIdentifier</key>
    <string>com.glint.app</string>
    <key>CFBundleExecutable</key>
    <string>glint</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>$version</string>
    <key>CFBundleVersion</key>
    <string>$version</string>
    <key>LSMinimumSystemVersion</key>
    <string>13.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>NSMicrophoneUsageDescription</key>
    <string>Glint records your microphone with screen recordings when you turn it on.</string>
    <key>NSCameraUsageDescription</key>
    <string>Glint records your camera with screen recordings when you turn it on.</string>
    <key>NSScreenCaptureUsageDescription</key>
    <string>Glint captures the screen for screenshots and recordings.</string>
</dict>
</plist>
EOF
codesign --force --sign "${GLINT_SIGN_IDENTITY:--}" --identifier com.glint.app "$app"
codesign --verify "$app"
echo "$app"
