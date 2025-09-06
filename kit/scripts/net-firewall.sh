#!/usr/bin/env bash
set -euo pipefail

# Minimal DOCKER-USER firewall helper (Linux hosts only)
# Usage: NET_CIDR=172.30.10.0/24 PROXY_IP=172.30.10.2 DNS_IP=172.30.10.3 ./net-firewall.sh install|remove

ACTION=${1:-}
[[ $EUID -eq 0 ]] || { echo "Run as root" >&2; exit 1; }

case "$ACTION" in
  install)
    : "${NET_CIDR:?NET_CIDR required}"
    : "${PROXY_IP:?PROXY_IP required}"
    : "${DNS_IP:?DNS_IP required}"
    iptables -I DOCKER-USER -s "$NET_CIDR" -d "$PROXY_IP" -j ACCEPT
    iptables -I DOCKER-USER -s "$NET_CIDR" -d "$DNS_IP" -j ACCEPT
    iptables -I DOCKER-USER -s "$NET_CIDR" ! -d "$PROXY_IP" -j REJECT
    ;;
  remove)
    # Best-effort flush of rules we added
    iptables -D DOCKER-USER -s "$NET_CIDR" ! -d "$PROXY_IP" -j REJECT || true
    iptables -D DOCKER-USER -s "$NET_CIDR" -d "$DNS_IP" -j ACCEPT || true
    iptables -D DOCKER-USER -s "$NET_CIDR" -d "$PROXY_IP" -j ACCEPT || true
    ;;
  *) echo "Usage: NET_CIDR=... PROXY_IP=... DNS_IP=... $0 install|remove" >&2; exit 1 ;;
esac

