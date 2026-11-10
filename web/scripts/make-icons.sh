#!/bin/sh
# One-off: rasterise the SVG icons with macOS sips. Run from code/web; commit the PNGs.
set -e
cd "$(dirname "$0")/.."
sips -s format png -z 192 192 public/icon.svg --out public/icons/icon-192.png >/dev/null
sips -s format png -z 512 512 public/icon.svg --out public/icons/icon-512.png >/dev/null
sips -s format png -z 512 512 public/icons/maskable.svg --out public/icons/maskable-512.png >/dev/null
sips -s format png -z 180 180 public/icons/full.svg --out public/apple-touch-icon.png >/dev/null
