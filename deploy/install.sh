#!/usr/bin/env bash
# One-time setup of a Linux host as the Talaria production server:
# creates the service user and /opt/talaria, installs the systemd unit and
# the deploy script CI calls, and grants the GitHub Actions runner user
# permission to run that one script as root.
#
#   sudo ./install.sh [runner-user]
#
# After this, register a self-hosted GitHub Actions runner on the box with
# the label "talaria-server" (Settings → Actions → Runners → New runner).
set -euo pipefail

RUNNER_USER="${1:-$(logname 2>/dev/null || echo "$SUDO_USER")}"
HERE="$(cd "$(dirname "$0")" && pwd)"

if [[ $EUID -ne 0 ]]; then
  echo "run with sudo" >&2
  exit 1
fi

id -u talaria >/dev/null 2>&1 || useradd --system --home /opt/talaria --shell /usr/sbin/nologin talaria
mkdir -p /opt/talaria/data
install -m 0755 "$HERE/deploy.sh" /opt/talaria/deploy.sh
install -m 0644 "$HERE/talaria.service" /etc/systemd/system/talaria.service
chown -R talaria:talaria /opt/talaria

# CI deploys by calling exactly this script as root — nothing else.
echo "${RUNNER_USER} ALL=(root) NOPASSWD: /opt/talaria/deploy.sh" > /etc/sudoers.d/talaria-deploy
chmod 0440 /etc/sudoers.d/talaria-deploy

systemctl daemon-reload
systemctl enable talaria >/dev/null

cat <<EOF

Talaria host prepared.
  - Put your config at /opt/talaria/talaria.toml (copy it from the old server)
    and your data at /opt/talaria/data (database + uploads), then:
        sudo chown -R talaria:talaria /opt/talaria
  - Register a GitHub Actions runner as user '${RUNNER_USER}' with label
    'talaria-server'; CI then deploys every push to main by running
    /opt/talaria/deploy.sh.
  - First deploy manually (any built linux binary):
        sudo /opt/talaria/deploy.sh ./talaria-linux-amd64
EOF
