-- Hosted dynamic DNS: names this server issues to other Talaria
-- installations under its zone, and the records currently published for
-- them (so unchanged updates never reach the DNS provider).
CREATE TABLE IF NOT EXISTS ddns_names (
  name       TEXT PRIMARY KEY,          -- label only, lowercase (the zone is config)
  token_hash TEXT NOT NULL UNIQUE,      -- sha256 hex of the bearer token
  created_at INTEGER NOT NULL,
  created_ip TEXT NOT NULL,
  last_seen  INTEGER NOT NULL DEFAULT 0 -- last authenticated request
);

CREATE TABLE IF NOT EXISTS ddns_records (
  name       TEXT NOT NULL REFERENCES ddns_names(name) ON DELETE CASCADE,
  host       TEXT NOT NULL,             -- fqdn, e.g. lan.skyle.talaria.gg
  type       TEXT NOT NULL,             -- A or TXT
  content    TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (host, type)
);
CREATE INDEX IF NOT EXISTS idx_ddns_records_name ON ddns_records(name);
