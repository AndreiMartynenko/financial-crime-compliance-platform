FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.29-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --chown=101:101 --from=build /app/dist /usr/share/nginx/html
COPY --chown=101:101 config.js.template /opt/fccp/config.js.template
COPY --chown=101:101 entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
