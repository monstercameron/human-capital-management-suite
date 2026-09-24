#!/usr/bin/env bash
# Build the process binaries into .artifacts/bin, never into the checkout root.
#
#   scripts/build.sh            # hcmnext, scheduler, worker, migrate, hcmctl, projector
#   scripts/build.sh hcmnext    # one command
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/.artifacts/bin"
embed="$root/.artifacts/embed"
tmp="$root/.artifacts/tmp"
cache="$root/.artifacts/gocache"
asset_tool="$tmp/journeywasm.exe"
mkdir -p "$out" "$embed" "$tmp" "$cache"
GOTMPDIR="$tmp"; TMP="$tmp"; TEMP="$tmp"; GOCACHE="$cache"
export GOTMPDIR TMP TEMP GOCACHE
cmds=("$@")
[ ${#cmds[@]} -eq 0 ] && cmds=(hcmnext scheduler worker migrate hcmctl projector)
cd "$root"
for c in "${cmds[@]}"; do
  if [ "$c" = "hcmnext" ]; then
    # The only committed source asset needed by the served catalog is the
    # brand mark. WASM, its runtime shim, compressed forms, and their manifest
    # live under .artifacts and enter go:embed through a build overlay.
    cp "$root/internal/humanwork/workspace/assets/harborcare-logo.svg" "$embed/harborcare-logo.svg"
    # The development seed writes only bounded display proxies into assets/.
    # Package those generated files when present; source originals never enter
    # this directory and the Go route allowlist rejects arbitrary names.
    rm -f "$embed"/person-hc-*-small.jpg
    shopt -s nullglob
    for photo in "$root"/internal/humanwork/workspace/assets/person-hc-*-small.jpg; do
      name="${photo##*/}"
      case "$name" in
        person-hc-[0-9][0-9][0-9]-small.jpg)
          index="${name#person-hc-}"
          index="${index%-small.jpg}"
          number=$((10#$index))
          if [ "$number" -ge 1 ] && [ "$number" -le 59 ] && [ $((number % 4)) -ne 0 ]; then
            cp "$photo" "$embed/$name"
          fi
          ;;
      esac
    done
    # Building the helper to a named .artifacts path avoids `go run`'s
    # temporary executable cleanup race on Windows.
    go build -o "$asset_tool" ./tools/uxqual/cmd/journeywasm
    "$asset_tool" -root "$root" -out "$embed" -overlay "$embed/assets.overlay.json"
    go build -overlay="$embed/assets.overlay.json" -o "$out/$c.exe" "./cmd/$c"
    # Generate the release SBOM from the built binary's embedded module graph
    # and enforce the P1B runtime boundary before considering the build ready.
    go run ./tools/quality/releaseboundary/cmd/releaseboundary \
      -root "$root" -binary "$out/$c.exe" -out "$out/$c.sbom.json"
  else
    go build -o "$out/$c.exe" "./cmd/$c"
  fi
  echo "built $out/$c.exe"
done
