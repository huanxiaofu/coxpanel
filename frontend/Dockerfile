FROM node:22-alpine AS build

WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund

COPY frontend ./
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.27-alpine

COPY --from=build /src/frontend/dist /usr/share/nginx/html
COPY deployments/nginx/default.conf /etc/nginx/conf.d/default.conf

USER nginx
EXPOSE 8080
STOPSIGNAL SIGTERM
