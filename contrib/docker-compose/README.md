Docker-Compose Examples
=======================

Here are few Docker Compose examples:

- `basic.yml`: Basic example
- `sqlite.yml`: SQLite deployment
- `caddy.yml`: Use Caddy as reverse-proxy with automatic HTTPS
- `traefik.yml`: Use Traefik as reverse-proxy with automatic HTTPS

```bash
docker compose -f basic.yml up -d

# SQLite deployment (enter a strong, unique password when prompted)
read -rsp 'Admin password: ' ADMIN_PASSWORD; echo
export ADMIN_PASSWORD
docker compose -f sqlite.yml up -d
unset ADMIN_PASSWORD
```
