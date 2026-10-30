#!/usr/bin/env bash
# サーバー上で root として実行する。何度実行しても同じ状態になる
set -euo pipefail

ALP_VERSION=1.0.22
SQLDEF_VERSION=3.11.29
SLOW_LOG=${SLOW_LOG:-/var/log/mysql/mysql-slow.log}

export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=120 update -qq
apt-get -o DPkg::Lock::Timeout=120 install -y -qq percona-toolkit graphviz sysstat tar curl >/dev/null

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

alp_base=https://github.com/tkuchiki/alp/releases/download/v$ALP_VERSION
curl -fsSL -o "$work/alp_linux_amd64.tar.gz" "$alp_base/alp_linux_amd64.tar.gz"
curl -fsSL -o "$work/checksums.txt" "$alp_base/alp_${ALP_VERSION}_checksums.txt"
(cd "$work" && grep ' alp_linux_amd64.tar.gz$' checksums.txt | sha256sum -c --quiet -)
tar -xzf "$work/alp_linux_amd64.tar.gz" -C /usr/local/bin alp

curl -fsSL -o "$work/mysqldef.tar.gz" \
  "https://github.com/sqldef/sqldef/releases/download/v$SQLDEF_VERSION/mysqldef_linux_amd64.tar.gz"
tar -xzf "$work/mysqldef.tar.gz" -C /usr/local/bin mysqldef

sysctl -q -w net.core.somaxconn=10000 net.ipv4.ip_local_port_range='10000 65535' || true
cat > /etc/sysctl.d/99-isucon.conf <<'EOF'
net.core.somaxconn = 10000
net.ipv4.ip_local_port_range = 10000 65535
EOF

/usr/local/bin/alp --version
/usr/local/bin/mysqldef --version
pt-query-digest --version
