#!/usr/bin/env bash
# Build the standalone time clock kiosk into .artifacts only.
#
# The Go runtime shim must come from the same toolchain used for the WASM
# binary; its location in supported current Go toolchains is lib/wasm.
set -eu

script_dir=${BASH_SOURCE[0]%/*}
root="$(cd "$script_dir/.." && pwd)"
out="$root/.artifacts/bin/timeclock-kiosk"
tmp="$root/.artifacts/tmp"
cache="$root/.artifacts/gocache"
cd "$root"
goroot="$(go env GOROOT)"
shim_source="$goroot/lib/wasm/wasm_exec.js"
wasm_tmp="$out/.timeclock.wasm.tmp.$$"
shim_tmp="$out/.wasm_exec.js.tmp.$$"

if [ ! -f "$shim_source" ]; then
  echo "build-timeclock-kiosk: matching Go shim not found: $shim_source" >&2
  exit 1
fi

mkdir -p "$out" "$tmp" "$cache"
GOTMPDIR="$tmp"; TMP="$tmp"; TEMP="$tmp"; GOCACHE="$cache"
export GOTMPDIR TMP TEMP GOCACHE
trap 'rm -f "$wasm_tmp" "$shim_tmp"' EXIT HUP INT TERM

GOOS=js GOARCH=wasm go build -trimpath -o "$wasm_tmp" ./tools/uxqual/cmd/timeclockwasm
if [ ! -s "$wasm_tmp" ]; then
  echo "build-timeclock-kiosk: Go produced an empty WASM bundle" >&2
  exit 1
fi
cp "$shim_source" "$shim_tmp"
if [ ! -s "$shim_tmp" ]; then
  echo "build-timeclock-kiosk: Go shim is empty: $shim_source" >&2
  exit 1
fi

mv -f "$wasm_tmp" "$out/timeclock.wasm"
mv -f "$shim_tmp" "$out/wasm_exec.js"
echo "built $out/timeclock.wasm"
echo "copied matching Go shim to $out/wasm_exec.js"
