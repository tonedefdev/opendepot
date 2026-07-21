# Stage 1: Build the Next.js app.
FROM node:22-alpine AS builder

WORKDIR /app

# Install dependencies first (layer caching).
COPY package.json yarn.lock* .yarnrc.yml* ./
RUN corepack enable && yarn install --immutable

# Copy source and build.
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN yarn build

# Stage 2: Runtime — NGINX + Node.js standalone server.
FROM nginx:1.27-alpine AS runner

# Install Node.js for the Next.js standalone server.
RUN apk upgrade --no-cache && apk add --no-cache nodejs

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
