#!/usr/bin/env bash
set -euo pipefail

build_android=false
build_ios=false
android_upload=false
ios_upload=false
if test "${PACKAGES:-false}" = true || test "${UPLOAD:-false}" = true; then
  case "${PLATFORM:-both}" in
    both) build_android=true; build_ios=true ;;
    android) build_android=true ;;
    ios) build_ios=true ;;
    *) echo 'PLATFORM must be both, android or ios' >&2; exit 1 ;;
  esac
fi
missing=()
for name in ANDROID_KEYSTORE_BASE64 ANDROID_STORE_PASSWORD ANDROID_KEY_ALIAS ANDROID_KEY_PASSWORD \
  IOS_CERTIFICATE_P12 IOS_CERTIFICATE_PASSWORD IOS_PROVISIONING_PROFILE; do
  test -n "${!name:-}" || missing+=("$name")
done
if test "${UPLOAD:-false}" = true && test ${#missing[@]} -eq 0; then
  if "$build_android" && test -n "${ANDROID_SIGNING_SHA256:-}"; then android_upload=true; fi
  if "$build_ios" && test -n "${APPLE_API_PRIVATE_KEY:-}" && test -n "${APPLE_TEAM_ID:-}" &&
    test -n "${APPLE_API_KEY_ID:-}" && test -n "${APPLE_API_ISSUER_ID:-}"; then ios_upload=true; fi
fi
printf 'build_android=%s\nbuild_ios=%s\nandroid_upload=%s\nios_upload=%s\n' \
  "$build_android" "$build_ios" "$android_upload" "$ios_upload" >> "$GITHUB_OUTPUT"
if test ${#missing[@]} -gt 0; then
  printf 'Mobile distribution uploads disabled; missing signing secrets: %s\n' "${missing[*]}" >> "$GITHUB_STEP_SUMMARY"
fi
