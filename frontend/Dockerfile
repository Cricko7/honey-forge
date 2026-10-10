# HoneyForge frontend: multi-stage сборка SPA → раздача через Caddy.
# ------- stage 1: сборка Vite -------
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install
COPY . .
# Единый origin: API зовётся по тому же адресу, что и страница (Caddy проксирует).
# Непустое значение → клиент работает в реальном режиме (не mock).
ARG VITE_API_BASE=http://localhost:8088
RUN echo "VITE_API_BASE=$VITE_API_BASE" > .env.production
RUN npm run build

# ------- stage 2: Caddy раздаёт dist и проксирует API -------
FROM caddy:2-alpine
COPY --from=build /app/dist /srv
COPY Caddyfile /etc/caddy/Caddyfile
EXPOSE 80
