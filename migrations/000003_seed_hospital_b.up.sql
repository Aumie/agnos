-- hospital_b has no HIS integration yet — internal/his/adapters.go's
-- "hospital_b" entry is still commented out (see that file's comment for
-- the pattern a real adapter would follow). Unlike hospital_a, there is no
-- live-sync path that could ever populate a patient record for it, so
-- /patient/search for a hospital_b-scoped staff member would otherwise
-- always return empty, even though every non-id filter (name/DOB/phone/
-- email) never touches the HIS adapter at all — see api-spec.md. Seeding
-- one patient directly is the only way to exercise that search path
-- locally for hospital_b. An id-based search still works exactly as
-- documented: it gracefully finds nothing new to sync (no adapter
-- registered, logged server-side, never a request error — see
-- docs/DECISION_LOG.md's graceful-degrade entry) and falls through to this
-- already-seeded row.
--
-- First name deliberately reuses "Somchai" — /mockserver's hospital_a
-- seed patient is also a Somchai (see mockserver/main.go's seedPatients).
-- Searching first_name="som" scoped to one hospital demonstrates partial
-- match and hospital scoping together: it finds exactly this one row, not
-- hospital_a's same-first-name patient too.
INSERT INTO hospitals (code, name) VALUES ('hospital_b', 'Hospital B');

INSERT INTO patients (
    hospital_id, patient_hn, national_id, passport_id,
    first_name_th, last_name_th, first_name_en, last_name_en,
    date_of_birth, phone_number, email, gender
)
SELECT
    id, 'HN90001', '2101234567890', NULL,
    'สมชาย', 'สุขใจ', 'Somchai', 'Sukjai',
    '1992-07-20', '0823456789', 'somchai.sukjai@example.com', 'M'
FROM hospitals WHERE code = 'hospital_b';
