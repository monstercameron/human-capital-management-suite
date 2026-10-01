package timeclockapp

// KioskCSS is the self-contained stylesheet for the reference tablet page.
// It has no external assets or JavaScript dependency and keeps controls at a
// 44px minimum touch target for managed tablets and keyboard users.
const KioskCSS = `
.timeclock-kiosk{box-sizing:border-box;min-height:100vh;padding:clamp(1rem,4vw,3rem);background:#f5f7fa;color:#15202b;font:500 1rem/1.5 system-ui,sans-serif;display:flex;flex-direction:column;gap:1.5rem}
.timeclock-header{display:flex;align-items:center;justify-content:space-between;gap:1rem;max-width:58rem;width:100%;margin:auto}
.timeclock-site{font-weight:700}.timeclock-status{display:flex;align-items:center;gap:.4rem;color:#315b46}.timeclock-dot{color:#2f9e61}.timeclock-queue{margin-inline-start:.75rem;color:#536170;font-size:.9rem}
.timeclock-face{font-variant-numeric:tabular-nums;font-size:clamp(1.5rem,5vw,2.5rem);font-weight:800}.timeclock-seconds,.timeclock-meridiem{font-size:.45em;margin-inline-start:.15em}
.timeclock-locale{display:flex;align-items:center;gap:.5rem;align-self:flex-end}.timeclock-locale select{min-height:44px;padding:.5rem;border:1px solid #9aa8b6;border-radius:.5rem;background:#fff;color:inherit}
.timeclock-panel{box-sizing:border-box;width:min(100%,34rem);margin:auto;padding:clamp(1.25rem,5vw,3rem);border:1px solid #d5dce3;border-radius:1rem;background:#fff;box-shadow:0 8px 24px #15202b12}.timeclock-panel h1{margin-top:0;font-size:clamp(1.5rem,5vw,2.25rem)}
.timeclock-panel input{box-sizing:border-box;width:100%;min-height:48px;margin:.5rem 0 1rem;padding:.65rem;border:1px solid #8190a0;border-radius:.5rem;font:inherit}.button{min-height:48px;padding:.65rem 1.1rem;border:1px solid #315b46;border-radius:.5rem;font:700 1rem inherit;cursor:pointer}.button.primary{background:#315b46;color:#fff}.button.secondary{background:#fff;color:#315b46}.button.text{border-color:transparent;background:transparent;color:#315b46}.button:focus-visible,.timeclock-key:focus-visible,select:focus-visible,input:focus-visible{outline:3px solid #1669c9;outline-offset:3px}
.timeclock-keypad{display:grid;grid-template-columns:repeat(3,1fr);gap:.65rem;margin:1rem 0}.timeclock-key{min-height:56px;border:1px solid #b2bfcb;border-radius:.6rem;background:#f8fafc;font-size:1.35rem;cursor:pointer}.timeclock-actions{display:grid;grid-template-columns:1fr 1fr;gap:.75rem;margin:1.5rem 0}.field-error{color:#a51d2d}.muted{color:#536170}
@media (max-width:600px){.timeclock-header{align-items:flex-start;flex-wrap:wrap}.timeclock-face{order:-1;width:100%}.timeclock-locale{align-self:stretch;justify-content:flex-end}.timeclock-panel{padding:1.25rem}.timeclock-actions{grid-template-columns:1fr}.timeclock-kiosk{padding:1rem}}
@media (prefers-reduced-motion:reduce){.timeclock-kiosk *{scroll-behavior:auto!important;transition:none!important}}
[dir=rtl] .timeclock-status,[dir=rtl] .timeclock-header{direction:rtl}
`
