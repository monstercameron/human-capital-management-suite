# Scripts

Root-level developer automation belongs here when it is not owned by a specific workspace package.

Package-specific scripts should stay inside their owning package.

## Standalone time clock kiosk

Build the browser-only kiosk bundle and the `wasm_exec.js` shim from the same
installed Go toolchain:

```sh
bash scripts/build-timeclock-kiosk.sh
```

The helper writes `.artifacts/bin/timeclock-kiosk/timeclock.wasm` and
`.artifacts/bin/timeclock-kiosk/wasm_exec.js`. Build the server separately,
then pass both paths when starting it from the repository root:

```sh
bash scripts/build.sh hcmnext
./.artifacts/bin/hcmnext.exe serve -profile=local-dev \
  -clock-kiosk-wasm-path=.artifacts/bin/timeclock-kiosk/timeclock.wasm \
  -clock-kiosk-exec-path=.artifacts/bin/timeclock-kiosk/wasm_exec.js
```

The standalone `/timeclock/` mount remains disabled unless both kiosk asset
paths are configured. The equivalent environment variables are
`HCMNEXT_CLOCK_KIOSK_WASM_PATH` and `HCMNEXT_CLOCK_KIOSK_EXEC_PATH`.
