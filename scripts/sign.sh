#!/usr/bin/env sh
# Sign a binary when the required certificate is configured.
# Skips silently otherwise, so plain builds keep working without secrets.
# The target OS is inferred from the artifact name (e.g. ..._windows_amd64.exe).
# Usage: scripts/sign.sh <artifact>
set -eu

artifact="$1"
base="$(basename "$artifact")"

case "$base" in
  *_windows_*)
    if [ -z "${WINDOWS_SIGNING_PFX:-}" ]; then
      echo "sign.sh: WINDOWS_SIGNING_PFX not set, skipping Windows signing of $artifact"
      exit 0
    fi
    if ! command -v osslsigncode >/dev/null 2>&1; then
      echo "sign.sh: osslsigncode not installed, skipping Windows signing of $artifact" >&2
      exit 0
    fi
    osslsigncode sign \
      -pkcs12 "$WINDOWS_SIGNING_PFX" \
      -pass "${WINDOWS_SIGNING_PASSWORD:-}" \
      -n "pipolinkcheck" \
      -i "https://github.com/jlettori/pipolinkcheck" \
      -t "http://timestamp.digicert.com" \
      -in "$artifact" \
      -out "$artifact.signed"
    mv "$artifact.signed" "$artifact"
    ;;
  *_darwin_*)
    if [ -z "${MACOS_SIGNING_IDENTITY:-}" ]; then
      echo "sign.sh: MACOS_SIGNING_IDENTITY not set, skipping macOS signing of $artifact"
      exit 0
    fi
    codesign --force --options runtime --timestamp --sign "$MACOS_SIGNING_IDENTITY" "$artifact"
    ;;
  *)
    echo "sign.sh: no signing configured for $artifact"
    exit 0
    ;;
esac