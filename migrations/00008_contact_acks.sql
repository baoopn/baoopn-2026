-- +goose Up
-- Visitor acknowledgements sent, for the site-wide and per-address caps.
-- address_key is the address as normalAddress keys it (lower case, no +tag,
-- Gmail without dots). Rows older than 30 days are pruned.
CREATE TABLE contact_acks (
    id          bigserial PRIMARY KEY,
    thread_id   bigint REFERENCES threads(id) ON DELETE SET NULL,
    address_key text NOT NULL,
    sent_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX contact_acks_sent_idx ON contact_acks (sent_at);
CREATE INDEX contact_acks_address_idx ON contact_acks (address_key, sent_at);

-- +goose Down
DROP TABLE contact_acks;
