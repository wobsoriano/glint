#!/bin/sh
# Builds a release Glint.app with tools/bundle-mac.sh and packs it into
# target/release/Glint-<version>-macos-<arch>.dmg next to an Applications link, so users can drag
# the app across. Prints the dmg path. GLINT_SIGN_IDENTITY is passed through to bundle-mac.sh.
set -e
cd "$(dirname "$0")/.."
tools/bundle-mac.sh --release >/dev/null
version=$(sed -n 's/^version = "\(.*\)"/\1/p' Cargo.toml | head -1)
dmg="target/release/Glint-$version-macos-$(uname -m).dmg"
staging=target/release/dmg
rm -rf "$staging" "$dmg"
mkdir "$staging"
# ditto keeps the bundle's signature and metadata intact where cp -R may not.
ditto target/release/Glint.app "$staging/Glint.app"
ln -s /Applications "$staging/Applications"
hdiutil create -volname Glint -srcfolder "$staging" -format UDZO "$dmg" >/dev/null
rm -rf "$staging"
echo "$dmg"
