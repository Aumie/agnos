-- Without this, a fresh database has zero rows in hospitals, and
-- /staff/create would fail with UNKNOWN_HOSPITAL for every request —
-- docker-compose up needs to produce something immediately usable.
INSERT INTO hospitals (code, name) VALUES ('hospital_a', 'Hospital A');
