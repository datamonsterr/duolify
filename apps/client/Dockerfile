# syntax=docker/dockerfile:1
# Web client image for the self-hosted stack (infra/deploy). Build context: repo root.
# VITE_API_URL defaults to empty: the web client calls the API on its own origin
# (the reverse proxy routes /v1 to the API), so one image runs on any domain.
FROM node:24-bookworm-slim AS build
WORKDIR /src
RUN npm install --global --no-fund --no-audit pnpm@12.18.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/client/package.json apps/client/
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile --filter @duolify/client --ignore-scripts
COPY docs/design/tokens.json docs/design/theme.css docs/design/
COPY services/api/internal/characters/schema/ services/api/internal/characters/schema/
COPY services/api/internal/characters/rig/ services/api/internal/characters/rig/
COPY apps/client/ apps/client/
ARG VITE_API_URL=""
ARG VITE_POSTHOG_KEY=""
ARG VITE_POSTHOG_HOST="https://us.i.posthog.com"
# Empty = the landing shows only the poster. Otherwise <asset origin>/landing/v1 (video served from the asset bucket).
ARG VITE_LANDING_MEDIA_BASE=""
ENV VITE_API_URL=$VITE_API_URL VITE_POSTHOG_KEY=$VITE_POSTHOG_KEY VITE_POSTHOG_HOST=$VITE_POSTHOG_HOST VITE_LANDING_MEDIA_BASE=$VITE_LANDING_MEDIA_BASE
RUN pnpm --filter @duolify/client build

# Caddy is both the static file server and the reverse proxy (infra/deploy/Caddyfile).
FROM caddy:2.11-alpine AS web
COPY --from=build /src/apps/client/dist /srv
COPY infra/deploy/Caddyfile infra/deploy/security-headers.caddy /etc/caddy/
COPY infra/deploy/proxy-entrypoint.sh /usr/local/bin/proxy-entrypoint.sh
ENTRYPOINT ["/usr/local/bin/proxy-entrypoint.sh"]
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
