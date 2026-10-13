#!/usr/bin/env bash
# One-time setup of a Linux host as the Talaria production server:
# creates the service user and /opt/talaria, installs the systemd unit and
# the deploy script CI calls, and grants the GitHub Actions runner user
# permission to run that one script as root.
#
#   sudo ./install.sh [runner-user] [data-dir]
#
# data-dir defaults to /opt/talaria/data. Point it at a big disk (for
# example a mirrored HDD pool mounted at /srv/talaria) to keep the database
# and uploads off the OS drive; the unit grants write access to it.
#
# After this, register a self-hosted GitHub Actions runner on the box with
# the label "talaria-server" (Settings → Actions → Runners → New runner).
set -euo pipefail

RUNNER_USER="${1:-$(logname 2>/dev/null || echo "$SUDO_USER")}"
DATA_DIR="${2:-/opt/talaria/data}"
HERE="$(cd "$(dirname "$0")" && pwd)"

if [[ $EUID -ne 0 ]]; then
  echo "run with sudo" >&2
  exit 1
fi

id -u talaria >/dev/null 2>&1 || useradd --system --home /opt/talaria --shell /usr/sbin/nologin talaria
mkdir -p /opt/talaria "$DATA_DIR"
install -m 0755 "$HERE/deploy.sh" /opt/talaria/deploy.sh
sed "s|@DATA_DIR@|$DATA_DIR|g" "$HERE/talaria.service" > /etc/systemd/system/talaria.service
chmod 0644 /etc/systemd/system/talaria.service
chown -R talaria:talaria /opt/talaria "$DATA_DIR"

# Config: start from the sample if there is none yet, with the data dir set.
if [[ ! -f /opt/talaria/talaria.toml ]]; then
  if [[ -f "$HERE/../talaria.example.toml" ]]; then
    sed -E "s|^(\s*data_dir\s*=).*|\1 \"$DATA_DIR\"|" "$HERE/../talaria.example.toml" > /opt/talaria/talaria.toml
  else
    printf '[server]\ndata_dir = "%s"\n' "$DATA_DIR" > /opt/talaria/talaria.toml
  fi
  chown talaria:talaria /opt/talaria/talaria.toml
fi

# CI deploys by calling exactly this script as root — nothing else.
echo "${RUNNER_USER} ALL=(root) NOPASSWD: /opt/talaria/deploy.sh" > /etc/sudoers.d/talaria-deploy
chmod 0440 /etc/sudoers.d/talaria-deploy

systemctl daemon-reload
systemctl enable talaria >/dev/null

cat <<MSG

Talaria host prepared.
  - Config:  /opt/talaria/talaria.toml   (data_dir = "$DATA_DIR")
  - Data:    $DATA_DIR   (database + uploads; copy the old server's data folder here, then
             sudo chown -R talaria:talaria "$DATA_DIR")
  - Register a GitHub Actions runner as user '${RUNNER_USER}' with label
    'talaria-server'; CI then deploys every push to main by running
    /opt/talaria/deploy.sh.
  - First deploy manually (any built linux binary):
        sudo /opt/talaria/deploy.sh ./talaria-linux-amd64
MSG
