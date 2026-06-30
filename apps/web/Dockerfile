# Dev image. Build context is apps/web. next dev (Fast Refresh), synced by `docker compose watch`.
FROM node:26-alpine
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
EXPOSE 3000
CMD ["npm", "run", "dev"]
