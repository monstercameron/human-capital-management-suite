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
    # Stage the complete candidate source asset catalog in the embed directory.
    # WASM, its runtime shim, compressed forms, and their manifest are built
    # into this directory below and enter go:embed through a build overlay,
    # while every other served asset is embedded from the candidate source assets
    # directory. The manifest the bundle builder generates describes only the
    # files present here, but the runtime validator compares it against the
    # whole embedded catalog (overlay bundle plus source assets), so
    # staging a subset yields a manifest the server rejects as stale on
    # startup. scripts/stage-workspace-assets.sh owns that clear-and-stage
    # behaviour (and refuses unsafe destinations before clearing anything);
    # the bundle build below then overwrites the generated bundle files and
    # manifest.json in place.
    bash "$root/scripts/stage-workspace-assets.sh" \
      "$root/internal/humanwork/workspace/assets" "$embed"
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
