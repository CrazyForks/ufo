# Dev image. next dev (Fast Refresh); sources synced by `docker compose watch`.
FROM node:26-alpine
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
EXPOSE 3000
CMD ["npm", "run", "dev"]
