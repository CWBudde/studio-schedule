-- +goose Up

ALTER TABLE settings
    ADD COLUMN issuer_name LONGTEXT,
    ADD COLUMN issuer_address LONGTEXT,
    ADD COLUMN issuer_tax_id LONGTEXT,
    ADD COLUMN issuer_email LONGTEXT,
    ADD COLUMN issuer_phone LONGTEXT,
    ADD COLUMN tax_mode VARBINARY(32),
    ADD COLUMN tax_note LONGTEXT,
    ADD COLUMN receipt_footer LONGTEXT,
    ADD COLUMN logo LONGBLOB,
    ADD COLUMN logo_type VARBINARY(32),
    ADD CONSTRAINT settings_issuer_name_check CHECK (OCTET_LENGTH(issuer_name) > 0),
    ADD CONSTRAINT settings_issuer_address_check CHECK (OCTET_LENGTH(issuer_address) > 0),
    ADD CONSTRAINT settings_issuer_tax_id_check CHECK (OCTET_LENGTH(issuer_tax_id) > 0),
    ADD CONSTRAINT settings_issuer_email_check CHECK (OCTET_LENGTH(issuer_email) > 0),
    ADD CONSTRAINT settings_issuer_phone_check CHECK (OCTET_LENGTH(issuer_phone) > 0),
    ADD CONSTRAINT settings_tax_mode_check CHECK (tax_mode IN ('small_business', 'exempt', 'standard')),
    ADD CONSTRAINT settings_tax_note_check CHECK (OCTET_LENGTH(tax_note) > 0),
    ADD CONSTRAINT settings_receipt_footer_check CHECK (OCTET_LENGTH(receipt_footer) > 0),
    ADD CONSTRAINT settings_logo_check CHECK (OCTET_LENGTH(logo) BETWEEN 1 AND 524288),
    ADD CONSTRAINT settings_logo_type_check CHECK (logo_type IN ('image/png', 'image/jpeg')),
    ADD CONSTRAINT settings_logo_type CHECK ((logo IS NULL) = (logo_type IS NULL));

-- The last sequence number issued per year. The issuing transaction holds
-- the row lock until it ends, and a rollback gives the number back.
CREATE TABLE receipt_counters (
    year INT PRIMARY KEY,
    last INT NOT NULL,
    CONSTRAINT receipt_counters_year_check CHECK (year BETWEEN 1 AND 9999),
    CONSTRAINT receipt_counters_last_check CHECK (last > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE receipts (
    id BINARY(16) PRIMARY KEY,
    number VARBINARY(3072) NOT NULL,
    year INT NOT NULL,
    sequence INT NOT NULL,
    kind VARBINARY(32) NOT NULL,
    cancels_receipt_id BINARY(16),
    issued_at DATETIME(6) NOT NULL,
    issue_date DATE NOT NULL,
    locale LONGTEXT NOT NULL,
    issuer_name LONGTEXT NOT NULL,
    issuer_address LONGTEXT NOT NULL,
    issuer_tax_id LONGTEXT NOT NULL,
    issuer_email LONGTEXT,
    issuer_phone LONGTEXT,
    tax_mode VARBINARY(32) NOT NULL,
    tax_note LONGTEXT,
    footer LONGTEXT,
    payer_person_id BINARY(16),
    recipient_name LONGTEXT NOT NULL,
    recipient_address LONGTEXT,
    recipient_email LONGTEXT,
    payment_method VARBINARY(32) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    net BIGINT NOT NULL,
    vat BIGINT NOT NULL,
    gross BIGINT NOT NULL,
    reason LONGTEXT,
    created_by BINARY(16),
    CONSTRAINT receipts_number_check CHECK (OCTET_LENGTH(TRIM(number)) > 0),
    CONSTRAINT receipts_year_check CHECK (year BETWEEN 1 AND 9999),
    CONSTRAINT receipts_sequence_check CHECK (sequence > 0),
    CONSTRAINT receipts_kind_check CHECK (kind IN ('sale', 'cancellation')),
    CONSTRAINT receipts_locale_check CHECK (OCTET_LENGTH(TRIM(locale)) > 0),
    CONSTRAINT receipts_issuer_name_check CHECK (OCTET_LENGTH(TRIM(issuer_name)) > 0),
    CONSTRAINT receipts_issuer_address_check CHECK (OCTET_LENGTH(TRIM(issuer_address)) > 0),
    CONSTRAINT receipts_issuer_tax_id_check CHECK (OCTET_LENGTH(TRIM(issuer_tax_id)) > 0),
    CONSTRAINT receipts_issuer_email_check CHECK (OCTET_LENGTH(issuer_email) > 0),
    CONSTRAINT receipts_issuer_phone_check CHECK (OCTET_LENGTH(issuer_phone) > 0),
    CONSTRAINT receipts_tax_mode_check CHECK (tax_mode IN ('small_business', 'exempt', 'standard')),
    CONSTRAINT receipts_tax_note_check CHECK (OCTET_LENGTH(tax_note) > 0),
    CONSTRAINT receipts_footer_check CHECK (OCTET_LENGTH(footer) > 0),
    CONSTRAINT receipts_recipient_name_check CHECK (OCTET_LENGTH(TRIM(recipient_name)) > 0),
    CONSTRAINT receipts_recipient_address_check CHECK (OCTET_LENGTH(recipient_address) > 0),
    CONSTRAINT receipts_recipient_email_check CHECK (OCTET_LENGTH(recipient_email) > 0),
    CONSTRAINT receipts_payment_method_check CHECK (payment_method IN ('cash', 'transfer', 'card')),
    CONSTRAINT receipts_currency_check CHECK (CHAR_LENGTH(currency) = 3 AND currency REGEXP '^[A-Z]{3}$'),
    CONSTRAINT receipts_reason_check CHECK (OCTET_LENGTH(reason) > 0),
    CONSTRAINT receipts_cancellation_ref CHECK ((kind = 'cancellation') = (cancels_receipt_id IS NOT NULL)),
    CONSTRAINT receipts_reason_cancellation CHECK (reason IS NULL OR kind = 'cancellation'),
    CONSTRAINT receipts_issue_year CHECK (YEAR(issue_date) = year),
    CONSTRAINT receipts_amounts CHECK (net + vat = gross),
    UNIQUE KEY receipts_number_key (number),
    UNIQUE KEY receipts_year_sequence_key (year, sequence),
    UNIQUE KEY receipts_cancels_key (cancels_receipt_id),
    KEY receipts_order_idx (year DESC, sequence DESC, id),
    KEY receipts_payer_idx (payer_person_id, issued_at, id),
    KEY receipts_created_by_fkey (created_by),
    CONSTRAINT receipts_cancels_receipt_id_fkey
        FOREIGN KEY (cancels_receipt_id) REFERENCES receipts(id) ON DELETE RESTRICT,
    CONSTRAINT receipts_payer_person_id_fkey FOREIGN KEY (payer_person_id) REFERENCES persons(id) ON DELETE RESTRICT,
    CONSTRAINT receipts_created_by_fkey FOREIGN KEY (created_by) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE receipt_lines (
    receipt_id BINARY(16) NOT NULL,
    position INT NOT NULL,
    description LONGTEXT NOT NULL,
    quantity INT NOT NULL,
    unit_price BIGINT NOT NULL,
    vat_rate_bp INT NOT NULL,
    net BIGINT NOT NULL,
    vat BIGINT NOT NULL,
    gross BIGINT NOT NULL,
    PRIMARY KEY (receipt_id, position),
    CONSTRAINT receipt_lines_position_check CHECK (position > 0),
    CONSTRAINT receipt_lines_description_check CHECK (OCTET_LENGTH(TRIM(description)) > 0),
    CONSTRAINT receipt_lines_quantity_check CHECK (quantity BETWEEN 1 AND 10000),
    CONSTRAINT receipt_lines_vat_rate_bp_check CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    CONSTRAINT receipt_lines_amounts CHECK (net + vat = gross AND gross = quantity * unit_price),
    CONSTRAINT receipt_lines_receipt_id_fkey FOREIGN KEY (receipt_id) REFERENCES receipts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE receipt_pdfs (
    receipt_id BINARY(16) PRIMARY KEY,
    pdf LONGBLOB NOT NULL,
    CONSTRAINT receipt_pdfs_pdf_check CHECK (OCTET_LENGTH(pdf) > 0),
    CONSTRAINT receipt_pdfs_receipt_id_fkey FOREIGN KEY (receipt_id) REFERENCES receipts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- Row triggers protect issued receipts on UPDATE and DELETE. TRUNCATE has
-- no trigger event on MySQL or MariaDB and requires the DROP privilege.
CREATE TRIGGER receipts_immutable_update BEFORE UPDATE ON receipts
    FOR EACH ROW SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'Receipts are immutable';
CREATE TRIGGER receipts_immutable_delete BEFORE DELETE ON receipts
    FOR EACH ROW SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'Receipts are immutable';
CREATE TRIGGER receipt_lines_immutable_update BEFORE UPDATE ON receipt_lines
    FOR EACH ROW SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'Receipt lines are immutable';
CREATE TRIGGER receipt_lines_immutable_delete BEFORE DELETE ON receipt_lines
    FOR EACH ROW SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'Receipt lines are immutable';
CREATE TRIGGER receipt_pdfs_immutable_update BEFORE UPDATE ON receipt_pdfs
    FOR EACH ROW SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'Receipt PDFs are immutable';
CREATE TRIGGER receipt_pdfs_immutable_delete BEFORE DELETE ON receipt_pdfs
    FOR EACH ROW SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'Receipt PDFs are immutable';

ALTER TABLE passes
    ADD UNIQUE KEY passes_receipt_key (receipt_id),
    ADD CONSTRAINT passes_receipt_fkey FOREIGN KEY (receipt_id) REFERENCES receipts(id) ON DELETE RESTRICT;

ALTER TABLE outbox_mails
    ADD COLUMN receipt_id BINARY(16),
    ADD KEY outbox_mails_receipt_idx (receipt_id),
    ADD CONSTRAINT outbox_mails_receipt_id_fkey FOREIGN KEY (receipt_id) REFERENCES receipts(id) ON DELETE RESTRICT;

-- +goose Down

ALTER TABLE outbox_mails DROP FOREIGN KEY outbox_mails_receipt_id_fkey;
ALTER TABLE outbox_mails DROP INDEX outbox_mails_receipt_idx, DROP COLUMN receipt_id;

ALTER TABLE passes DROP FOREIGN KEY passes_receipt_fkey;
ALTER TABLE passes DROP INDEX passes_receipt_key;
UPDATE passes SET receipt_id = NULL;

DROP TABLE receipt_pdfs;
DROP TABLE receipt_lines;
DROP TABLE receipts;
DROP TABLE receipt_counters;

ALTER TABLE settings
    DROP CONSTRAINT settings_logo_type,
    DROP CONSTRAINT settings_logo_type_check,
    DROP CONSTRAINT settings_logo_check,
    DROP CONSTRAINT settings_receipt_footer_check,
    DROP CONSTRAINT settings_tax_note_check,
    DROP CONSTRAINT settings_tax_mode_check,
    DROP CONSTRAINT settings_issuer_phone_check,
    DROP CONSTRAINT settings_issuer_email_check,
    DROP CONSTRAINT settings_issuer_tax_id_check,
    DROP CONSTRAINT settings_issuer_address_check,
    DROP CONSTRAINT settings_issuer_name_check,
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
