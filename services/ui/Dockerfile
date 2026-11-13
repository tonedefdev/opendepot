# Stage 1: Build the Next.js app.
FROM node:22.23.5-alpine3.23@sha256:6868d4695b947ee120bffaa463fecd68cde370d7bfec92955e7c8ac576b4f475 AS builder

WORKDIR /app

# Install dependencies first (layer caching).
COPY package.json yarn.lock* .yarnrc.yml* ./
COPY .yarn/patches/ ./.yarn/patches/
RUN corepack enable && yarn install --immutable

# Copy source and build.
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN yarn build

# Stage 2: Runtime — NGINX + Node.js standalone server.
FROM nginx:1.29.8-alpine3.23@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de AS runner

# Install Node.js for the Next.js standalone server (npm is not installed; the
# standalone output runs with `node server.js` only).
ARG APK_PATCH_STAMP=unset
RUN echo "apk patch stamp: ${APK_PATCH_STAMP}" && apk upgrade --no-cache && apk add --no-cache "brotli-libs>=1.2.1-r0" nodejs

# Pre-create nginx writable directories for non-root (UID 101) operation.
RUN mkdir -p /var/cache/nginx/client_temp \
              /var/cache/nginx/proxy_temp \
              /var/cache/nginx/fastcgi_temp \
              /var/cache/nginx/uwsgi_temp \
              /var/cache/nginx/scgi_temp \
              /var/log/nginx \
              /var/run && \
    chown -R 101:101 /var/cache/nginx /var/log/nginx && \
    touch /var/run/nginx.pid && chown 101:101 /var/run/nginx.pid

WORKDIR /app

# nginx.conf.template is provided at runtime by the Helm chart's ui-nginx-config
# ConfigMap (chart/opendepot/templates/ui-configmap.yaml), mounted over this
# path via subPath. This image is only ever run through that Helm chart, so no
# config is baked in here — it would just be a second copy to keep in sync.

# Copy the Next.js standalone build output.
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public

# Entrypoint: substitute env vars into nginx.conf, then launch both processes.
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 80

USER 101

ENTRYPOINT ["/entrypoint.sh"]
