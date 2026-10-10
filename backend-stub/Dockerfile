# Стаб-бэкенд HoneyForge (Node). Слушает :8080 внутри сети Docker.
FROM node:22-alpine
WORKDIR /app
COPY package.json ./
RUN npm install --omit=dev
COPY data.js server.js ./
ENV PORT=8080
# В контейнере слушаем на всех интерфейсах, чтобы nginx мог достучаться.
ENV HOST=0.0.0.0
EXPOSE 8080
CMD ["node", "server.js"]
