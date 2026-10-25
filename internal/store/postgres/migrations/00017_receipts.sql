-- Receipts (ADR 0012): payment confirmations for pass sales, numbered gaplessly
-- per year, immutable once issued (a trigger rejects UPDATE and DELETE), with
-- the issuer, tax data and the rendered PDF snapshotted; the school's receipt
-- settings and logo; mails that attach a receipt's PDF.
-- Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE settings
    ADD COLUMN issuer_name text CHECK (issuer_name <> ''),
    ADD COLUMN issuer_address text CHECK (issuer_address <> ''),
    ADD COLUMN issuer_tax_id text CHECK (issuer_tax_id <> ''),
    ADD COLUMN issuer_email text CHECK (issuer_email <> ''),
    ADD COLUMN issuer_phone text CHECK (issuer_phone <> ''),
    -- NULL until the school chose one; receipts need it.
    ADD COLUMN tax_mode text CHECK (tax_mode IN ('small_business', 'exempt', 'standard')),
    ADD COLUMN tax_note text CHECK (tax_note <> ''),
    ADD COLUMN receipt_footer text CHECK (receipt_footer <> ''),
    -- Read only where receipts are rendered, not with the settings.
    ADD COLUMN logo bytea CHECK (octet_length(logo) BETWEEN 1 AND 524288),
    ADD COLUMN logo_type text CHECK (logo_type IN ('image/png', 'image/jpeg')),
    ADD CONSTRAINT settings_logo_type CHECK ((logo IS NULL) = (logo_type IS NULL));

-- The last sequence number issued per year. The issuing transaction takes the
-- next one with INSERT … ON CONFLICT DO UPDATE … RETURNING, which locks the
-- row until it ends: numbers are gapless, and a rollback gives the number
-- back.
CREATE TABLE receipt_counters (
    year integer PRIMARY KEY CHECK (year BETWEEN 1 AND 9999),
    last integer NOT NULL CHECK (last > 0)
);

CREATE TABLE receipts (
    id uuid PRIMARY KEY,
    number text COLLATE "C" NOT NULL CHECK (btrim(number) <> ''),
    year integer NOT NULL CHECK (year BETWEEN 1 AND 9999),
    sequence integer NOT NULL CHECK (sequence > 0),
    kind text NOT NULL CHECK (kind IN ('sale', 'cancellation')),
    cancels_receipt_id uuid REFERENCES receipts ON DELETE RESTRICT,
    issued_at timestamptz NOT NULL,
    issue_date date NOT NULL,
    locale text NOT NULL CHECK (btrim(locale) <> ''),
    -- The issuer and the tax data at issuance.
    issuer_name text NOT NULL CHECK (btrim(issuer_name) <> ''),
    issuer_address text NOT NULL CHECK (btrim(issuer_address) <> ''),
    issuer_tax_id text NOT NULL CHECK (btrim(issuer_tax_id) <> ''),
    issuer_email text CHECK (issuer_email <> ''),
    issuer_phone text CHECK (issuer_phone <> ''),
    tax_mode text NOT NULL CHECK (tax_mode IN ('small_business', 'exempt', 'standard')),
    tax_note text CHECK (tax_note <> ''),
    footer text CHECK (footer <> ''),
    payer_person_id uuid REFERENCES persons ON DELETE RESTRICT,
    recipient_name text COLLATE "C" NOT NULL CHECK (btrim(recipient_name) <> ''),
    recipient_address text CHECK (recipient_address <> ''),
    recipient_email text CHECK (recipient_email <> ''),
    payment_method text NOT NULL CHECK (payment_method IN ('cash', 'transfer', 'card')),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    net bigint NOT NULL,
    vat bigint NOT NULL,
    gross bigint NOT NULL,
    reason text CHECK (reason <> ''),
    created_by uuid REFERENCES accounts ON DELETE RESTRICT,
    CONSTRAINT receipts_number_key UNIQUE (number),
    CONSTRAINT receipts_year_sequence_key UNIQUE (year, sequence),
    -- A receipt is canceled at most once.
    CONSTRAINT receipts_cancels_key UNIQUE (cancels_receipt_id),
    CONSTRAINT receipts_cancellation_ref CHECK ((kind = 'cancellation') = (cancels_receipt_id IS NOT NULL)),
    CONSTRAINT receipts_reason_cancellation CHECK (reason IS NULL OR kind = 'cancellation'),
    CONSTRAINT receipts_issue_year CHECK (extract(year FROM issue_date) = year),
    CONSTRAINT receipts_amounts CHECK (net + vat = gross)
);

CREATE INDEX receipts_order_idx ON receipts (year DESC, sequence DESC, id);
CREATE INDEX receipts_payer_idx ON receipts (payer_person_id, issued_at, id);

CREATE TABLE receipt_lines (
    receipt_id uuid NOT NULL REFERENCES receipts ON DELETE RESTRICT,
    position integer NOT NULL CHECK (position > 0),
    description text NOT NULL CHECK (btrim(description) <> ''),
    quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 10000),
    unit_price bigint NOT NULL,
    vat_rate_bp integer NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    net bigint NOT NULL,
    vat bigint NOT NULL,
    gross bigint NOT NULL,
    PRIMARY KEY (receipt_id, position),
    CONSTRAINT receipt_lines_amounts CHECK (net + vat = gross AND gross = quantity * unit_price)
);

-- The rendered PDF, the immutable original that mails and downloads read.
-- Apart from the receipt, so that lists do not read it, and so that a receipt
-- issued without one (the seed) gets it rendered once later.
CREATE TABLE receipt_pdfs (
    receipt_id uuid PRIMARY KEY REFERENCES receipts ON DELETE RESTRICT,
    pdf bytea NOT NULL CHECK (octet_length(pdf) > 0)
);

-- Receipts, their lines and PDFs are never changed or deleted: a mistake is
-- corrected by a cancellation receipt.
-- +goose StatementBegin
CREATE FUNCTION receipts_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% of % is not allowed: receipts are immutable', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER receipts_immutable BEFORE UPDATE OR DELETE ON receipts
    FOR EACH ROW EXECUTE FUNCTION receipts_immutable();
CREATE TRIGGER receipt_lines_immutable BEFORE UPDATE OR DELETE ON receipt_lines
    FOR EACH ROW EXECUTE FUNCTION receipts_immutable();
CREATE TRIGGER receipt_pdfs_immutable BEFORE UPDATE OR DELETE ON receipt_pdfs
    FOR EACH ROW EXECUTE FUNCTION receipts_immutable();
-- TRUNCATE bypasses row triggers.
CREATE TRIGGER receipts_no_truncate BEFORE TRUNCATE ON receipts
    FOR EACH STATEMENT EXECUTE FUNCTION receipts_immutable();
CREATE TRIGGER receipt_lines_no_truncate BEFORE TRUNCATE ON receipt_lines
    FOR EACH STATEMENT EXECUTE FUNCTION receipts_immutable();
CREATE TRIGGER receipt_pdfs_no_truncate BEFORE TRUNCATE ON receipt_pdfs
    FOR EACH STATEMENT EXECUTE FUNCTION receipts_immutable();

ALTER TABLE passes
    ADD CONSTRAINT passes_receipt_fkey FOREIGN KEY (receipt_id) REFERENCES receipts ON DELETE RESTRICT;
CREATE UNIQUE INDEX passes_receipt_key ON passes (receipt_id);

ALTER TABLE outbox_mails
    ADD COLUMN receipt_id uuid REFERENCES receipts ON DELETE RESTRICT;
CREATE INDEX outbox_mails_receipt_idx ON outbox_mails (receipt_id);

-- +goose Down

DROP INDEX outbox_mails_receipt_idx;
ALTER TABLE outbox_mails DROP COLUMN receipt_id;

DROP INDEX passes_receipt_key;
ALTER TABLE passes DROP CONSTRAINT passes_receipt_fkey;
-- Passes keep no reference to the receipts that go.
UPDATE passes SET receipt_id = NULL;

DROP TABLE receipt_pdfs;
DROP TABLE receipt_lines;
DROP TABLE receipts;
DROP FUNCTION receipts_immutable();
DROP TABLE receipt_counters;

ALTER TABLE settings
    DROP CONSTRAINT settings_logo_type,
    DROP COLUMN logo_type,
    DROP COLUMN logo,
    DROP COLUMN receipt_footer,
    DROP COLUMN tax_note,
    DROP COLUMN tax_mode,
    DROP COLUMN issuer_phone,
    DROP COLUMN issuer_email,
    DROP COLUMN issuer_tax_id,
    DROP COLUMN issuer_address,
    DROP COLUMN issuer_name;
