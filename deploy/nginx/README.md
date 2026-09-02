# Nginx 反向代理（s3clinet）

- **`worker_processes 1`**：单 worker，与 Go 后端一对一协同，便于 `nginx -s reload` 零停机热加载。
- **Docker Compose**：使用 `nginx.docker.conf` + `conf.d/s3clinet-docker.conf`，对外 `:8080` → `server:8080`。
- **生产 TLS**：`docker compose -f docker-compose.prod.yml -f docker-compose.tls.yml up -d`，挂载 `./certs/{fullchain,privkey}.pem`，配置见 `conf.d/s3clinet-tls.example.conf`。
- **本机开发**：`make dev-nginx` 或 `./scripts/run-dev.sh --nginx`（Go 监听 `:8081`，nginx 对外 `:8080`）。

## 优雅 reload（不中断连接）

```bash
# Docker
docker compose exec nginx nginx -s reload

# 本机
./scripts/graceful-restart.sh nginx
```

## 优雅停止

- nginx：`SIGQUIT`（等待 worker 处理完当前请求）
- Go server：`SIGTERM` → `http.Server.Shutdown`（超时见 `S3C_SHUTDOWN_TIMEOUT`）
