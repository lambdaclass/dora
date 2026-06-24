-- +goose Up
-- +goose StatementBegin

-- Generic key/value store (sync markers, pruning state).
CREATE TABLE IF NOT EXISTS "explorer_state" (
    "key"   TEXT NOT NULL PRIMARY KEY,
    "value" TEXT NOT NULL
);

-- Validator name lookup.
CREATE TABLE IF NOT EXISTS "validator_names" (
    "index" INTEGER NOT NULL PRIMARY KEY,
    "name"  TEXT NOT NULL
);

-- Per-slot rows of the lean chain. No epoch aggregation; finality is tracked
-- via the justified/finalized flags plus the checkpoints table.
CREATE TABLE IF NOT EXISTS "slots" (
    "slot"              INTEGER NOT NULL,
    "root"              BLOB NOT NULL,
    "parent_root"       BLOB,
    "state_root"        BLOB,
    "proposer"          INTEGER NOT NULL,
    "status"            INTEGER NOT NULL DEFAULT 0,
    "attestation_count" INTEGER NOT NULL DEFAULT 0,
    "justified"         BOOLEAN NOT NULL DEFAULT FALSE,
    "finalized"         BOOLEAN NOT NULL DEFAULT FALSE,
    "block_size"        INTEGER NOT NULL DEFAULT 0,
    "recv_delay"        INTEGER NOT NULL DEFAULT 0,
    CONSTRAINT "slots_pkey" PRIMARY KEY ("slot", "root")
);
CREATE INDEX IF NOT EXISTS "slots_slot_idx" ON "slots" ("slot");
CREATE INDEX IF NOT EXISTS "slots_proposer_idx" ON "slots" ("proposer");
CREATE INDEX IF NOT EXISTS "slots_status_idx" ON "slots" ("status");

-- Finality history: justified and finalized checkpoints over time.
CREATE TABLE IF NOT EXISTS "checkpoints" (
    "slot" INTEGER NOT NULL,
    "root" BLOB NOT NULL,
    "type" INTEGER NOT NULL,
    CONSTRAINT "checkpoints_pkey" PRIMARY KEY ("slot", "type")
);

-- Validator registry. Two XMSS pubkeys per validator (opaque bytes).
CREATE TABLE IF NOT EXISTS "validators" (
    "index"             INTEGER NOT NULL PRIMARY KEY,
    "attestation_pubkey" BLOB,
    "proposal_pubkey"    BLOB
);

-- Proposer duty assignments per slot and whether they were fulfilled.
CREATE TABLE IF NOT EXISTS "validator_duties" (
    "slot"      INTEGER NOT NULL PRIMARY KEY,
    "proposer"  INTEGER NOT NULL,
    "fulfilled" BOOLEAN NOT NULL DEFAULT FALSE
);

-- Per-validator attestations used to compute participation.
CREATE TABLE IF NOT EXISTS "votes" (
    "slot"            INTEGER NOT NULL,
    "validator_index" INTEGER NOT NULL,
    "source_slot"     INTEGER NOT NULL DEFAULT 0,
    "source_root"     BLOB,
    "target_slot"     INTEGER NOT NULL DEFAULT 0,
    "target_root"     BLOB,
    "head_root"       BLOB,
    CONSTRAINT "votes_pkey" PRIMARY KEY ("slot", "validator_index")
);
CREATE INDEX IF NOT EXISTS "votes_slot_idx" ON "votes" ("slot");

-- Near-head block cache for reorg replay.
CREATE TABLE IF NOT EXISTS "unfinalized_blocks" (
    "root"       BLOB NOT NULL PRIMARY KEY,
    "slot"       INTEGER NOT NULL,
    "status"     INTEGER NOT NULL DEFAULT 0,
    "block_data" BLOB
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
