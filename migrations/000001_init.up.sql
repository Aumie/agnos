-- Direct translation of docs/er-diagram.md. gen_random_uuid() is native
-- to Postgres 13+, no pgcrypto extension needed.

CREATE TABLE hospitals (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code       text NOT NULL UNIQUE, -- HIS adapter routing key, e.g. "hospital_a"
    name       text NOT NULL,        -- display name
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE staff (
    id                        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    hospital_id               uuid NOT NULL REFERENCES hospitals (id),
    username                  text NOT NULL,
    password_hash             text NOT NULL,
    -- Nullable, and always set/cleared together: a refresh token hash
    -- with no expiry would be unenforceable (api-spec.md commits to a
    -- 7-day refresh token lifetime).
    refresh_token_hash        text,
    refresh_token_expires_at  timestamptz,
    created_at                timestamptz NOT NULL DEFAULT now(),
    updated_at                timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_staff_hospital_username UNIQUE (hospital_id, username)
);

CREATE TABLE patients (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    hospital_id    uuid NOT NULL REFERENCES hospitals (id),

    -- Hospital-internal patient number; every row originates from a HIS
    -- sync, and Hospital A's contract always includes it.
    patient_hn     text NOT NULL,

    -- Nullable, unlike patient_hn: Hospital A's contract allows lookup by
    -- either national_id or passport_id, so a given record may only have
    -- one of the two populated.
    national_id    text,
    passport_id    text,

    first_name_th  text NOT NULL,
    middle_name_th text,
    last_name_th   text NOT NULL,
    first_name_en  text NOT NULL,
    middle_name_en text,
    last_name_en   text NOT NULL,

    date_of_birth  date,
    phone_number   text,
    email          text,
    gender         text CHECK (gender IN ('M', 'F')),

    synced_at      timestamptz NOT NULL DEFAULT now(), -- last upsert from HIS
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_patients_hospital_patient_hn UNIQUE (hospital_id, patient_hn)
);

-- Partial unique indexes: national_id/passport_id may legitimately be
-- absent, unlike patient_hn above, so these only enforce uniqueness where
-- the value is actually set. Together with patient_hn's constraint, these
-- are the dedupe keys the search usecase upserts against.
CREATE UNIQUE INDEX uq_patients_hospital_national_id
    ON patients (hospital_id, national_id) WHERE national_id IS NOT NULL;
CREATE UNIQUE INDEX uq_patients_hospital_passport_id
    ON patients (hospital_id, passport_id) WHERE passport_id IS NOT NULL;

-- Lookup indexes for the remaining search fields. Name search (TH/EN) uses
-- ILIKE for this scope; see docs/er-diagram.md for why a
-- pg_trgm GIN index is the noted future upgrade, not built now.
CREATE INDEX idx_patients_hospital_phone ON patients (hospital_id, phone_number);
CREATE INDEX idx_patients_hospital_email ON patients (hospital_id, email);
