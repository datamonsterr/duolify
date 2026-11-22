# Host-local Caddy site settings

`compose.prod.yml` mounts this directory read-only at `/etc/caddy/site.d` in the proxy, and the Caddyfile
imports every `*.caddy` file here into the site block. Files other than this README are git-ignored and live only on the VM.

Example (Cloudflare origin certificate, see `docs/infra/staging.md`), `tls.caddy`:

```
tls /etc/caddy/site.d/origin.pem /etc/caddy/site.d/origin.key
```

`origin-ca.pem` (the issuing CA's root, for example Cloudflare's Origin CA ECC root), when present, is trusted by `selfhost.sh smoke`.
