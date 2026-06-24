-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS "explorer_state" (
    "key"   VARCHAR(255) NOT NULL PRIMARY KEY,
    "value" TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "validator_names" (
    "index" BIGINT NOT NULL PRIMARY KEY,
    "name"  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "slots" (
    "slot"              BIGINT NOT NULL,
    "root"              BYTEA NOT NULL,
    "parent_root"       BYTEA,
    "state_root"        BYTEA,
    "proposer"          BIGINT NOT NULL,
    "status"            SMALLINT NOT NULL DEFAULT 0,
    "attestation_count" BIGINT NOT NULL DEFAULT 0,
    "justified"         BOOLEAN NOT NULL DEFAULT FALSE,
    "finalized"         BOOLEAN NOT NULL DEFAULT FALSE,
    "block_size"        BIGINT NOT NULL DEFAULT 0,
    "recv_delay"        INTEGER NOT NULL DEFAULT 0,
    CONSTRAINT "slots_pkey" PRIMARY KEY ("slot", "root")
);
CREATE INDEX IF NOT EXISTS "slots_slot_idx" ON "slots" ("slot");
CREATE INDEX IF NOT EXISTS "slots_proposer_idx" ON "slots" ("proposer");
CREATE INDEX IF NOT EXISTS "slots_status_idx" ON "slots" ("status");

CREATE TABLE IF NOT EXISTS "checkpoints" (
    "slot" BIGINT NOT NULL,
    "root" BYTEA NOT NULL,
    "type" SMALLINT NOT NULL,
    CONSTRAINT "checkpoints_pkey" PRIMARY KEY ("slot", "type")
);

CREATE TABLE IF NOT EXISTS "validators" (
    "index"             BIGINT NOT NULL PRIMARY KEY,
    "attestation_pubkey" BYTEA,
    "proposal_pubkey"    BYTEA
);

CREATE TABLE IF NOT EXISTS "validator_duties" (
    "slot"      BIGINT NOT NULL PRIMARY KEY,
    "proposer"  BIGINT NOT NULL,
    "fulfilled" BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS "votes" (
    "slot"            BIGINT NOT NULL,
    "validator_index" BIGINT NOT NULL,
    "source_slot"     BIGINT NOT NULL DEFAULT 0,
    "source_root"     BYTEA,
    "target_slot"     BIGINT NOT NULL DEFAULT 0,
    "target_root"     BYTEA,
    "head_root"       BYTEA,
    CONSTRAINT "votes_pkey" PRIMARY KEY ("slot", "validator_index")
);
CREATE INDEX IF NOT EXISTS "votes_slot_idx" ON "votes" ("slot");

CREATE TABLE IF NOT EXISTS "unfinalized_blocks" (
    "root"       BYTEA NOT NULL PRIMARY KEY,
    "slot"       BIGINT NOT NULL,
    "status"     INTEGER NOT NULL DEFAULT 0,
    "block_data" BYTEA
);
CREATE INDEX IF NOT EXISTS "unfinalized_blocks_slot_idx" ON "unfinalized_blocks" ("slot");

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS "unfinalized_blocks";
DROP TABLE IF EXISTS "votes";
DROP TABLE IF EXISTS "validator_duties";
DROP TABLE IF EXISTS "validators";
DROP TABLE IF EXISTS "checkpoints";
DROP TABLE IF EXISTS "slots";
DROP TABLE IF EXISTS "validator_names";
DROP TABLE IF EXISTS "explorer_state";
-- +goose StatementEnd
