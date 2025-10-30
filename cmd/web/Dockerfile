FROM node:20-alpine

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies (no lockfile required)
RUN npm install --omit=dev

# Copy source code
COPY . .

# Build the application
RUN npm run build

# Production stage
FROM nginx:1.25.4-alpine

# Copy built files to nginx
COPY --from=0 /app/build /usr/share/nginx/html

# Copy nginx config
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
