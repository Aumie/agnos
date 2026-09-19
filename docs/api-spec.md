# API Spec — Hospital Middleware

Machine-readable version: [`openapi.yaml`](./openapi.yaml).

## Auth scheme

`Authorization: Bearer <access_token>` on protected routes.

- Access token: short-lived, 15 minutes. Claims: `sub` (staff id), `hospital_id`, `exp`. Both `staff_id` and `hospital_id` are embedded so `/patient/search` can scope by hospital straight off the token, with no DB lookup needed per request. This trades theoretical staleness (if a staff member's hospital changed mid-token-lifetime) for zero extra DB round-trips — acceptable here because nothing in this system's scope ever reassigns a staff member to a different hospital, so that staleness case can't actually occur.
- Refresh token: 7 days, single active token per staff member (hashed at rest).

## Known limitations (stated explicitly, not silent gaps)

- **No rate limiting or account lockout on repeated failed `/staff/login` attempts.** The constant-time comparison (see Auth scheme below) prevents an attacker from *enumerating* valid usernames/hospitals via response timing, but it does not prevent brute-forcing a password once a valid username+hospital pair is known. Out of scope for this project; a real deployment would add this at the edge (e.g. Nginx `limit_req`, or a per-IP/per-username counter in the service layer).
- **No CORS headers configured.** Irrelevant for server-to-server calls (Postman, another backend service), but a browser-based frontend calling these endpoints directly from a different origin would be blocked by the browser's own same-origin policy until `Access-Control-Allow-Origin` (and related headers) are added.
- **`username` and `hospital` lookups are case-sensitive** (`WHERE username = $1`, plain equality — no `LOWER()`/`citext`). `nurse_j` and `Nurse_J` are different accounts. This is the current, intentional behavior — not yet requested to be case-insensitive — documented here so it isn't mistaken for an oversight if it comes up.
- **`Content-Type` is not checked on any endpoint.** Every handler binds the request body as JSON unconditionally (Gin's `ShouldBindJSON` doesn't inspect the header at all) — a request with a missing or incorrect `Content-Type` still succeeds as long as the body itself is valid JSON.

## Common error envelope

All error responses share this shape:

```json
{
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "username or password is incorrect"
  }
}
```

## Assumptions (explicitly called out, not inferred)

1. **`/staff/create` is unauthenticated** for this scope — the spec defines no role/admin concept. In production this would sit behind an admin-only role; noted here rather than left as a silent gap.
2. **Username uniqueness is scoped per hospital**, not global — two different hospitals may each have their own `nurse_j`. This is why `hospital` is a required input on both `/staff/create` and `/staff/login`.
3. **`/staff/refresh` is an addition** beyond the literal task list, required to back the short-lived-JWT + refresh-token design (see project structure doc). Token rotation on refresh; no revocation-list/blacklist infrastructure — noted as an intentional simplification, not an oversight.
4. **`page`/`page_size` on `/patient/search` are additions** — an all-fields-empty request means "every patient in my hospital," so pagination is a practical safeguard, not scope creep. Defaults: `page=1`, `page_size=20`, max `page_size=100`. This also means the **response is a `{data, page, page_size, total}` object, not a bare array** — the scope's "Output: Patients matching the criteria..." doesn't specify an envelope shape; wrapping it is a direct consequence of adding pagination, not a separate unstated choice.
5. **HTTP method is `POST` for all four of our own endpoints** (`/staff/create`, `/staff/login`, `/staff/refresh`, `/patient/search`) — the scope states a verb only for Hospital A's *external* API (`GET .../patient/search/{id}`); it never specifies a verb for the four endpoints we implement. `POST` is used because every one of them takes a JSON request body (credentials or search filters), and a `GET` carrying a body — especially one carrying a password or carrying filters that will grow — is both non-standard and inadvisable.
6. **A `hospitals` table/model exists, beyond the two schemas the scope names** ("Patient" and "Staff" in Tasks 2–3). This third model isn't asked for directly — it's added because both `staff.hospital_id` and `patients.hospital_id` need something to reference, and the `his` adapter registry needs a stable routing key. Full justification is in `er-diagram.md` ("Why a `hospitals` table exists at all"); listed here too so every schema-level addition is in one place.
7. **There is no hospital-management API — a hospital row can only ever come from a migration or a manual `INSERT`, never from a request.** `HospitalRepo` (`internal/postgres/hospital_repo.go`) only has `FindByCode`/`FindByID`, no `Create`; none of the four endpoints in scope create one either. Onboarding a real new hospital means writing a new migration (`000003_add_hospital_x.up.sql`, following the `000002_seed_hospitals` pattern) or running SQL directly — an ops/DBA action outside the running application, not an in-app workflow. This is the concrete mechanism behind "a hospital onboarded administratively before its HIS integration is coded," referenced below in the graceful-degrade behavior for `/patient/search`.

---

## `POST /staff/create`

**Auth:** none (see Assumption 1).

**Request**
```json
{
  "username": "nurse_j",
  "password": "S3cur3P@ss!",
  "hospital": "hospital_a"
}
```

| field | type | required | notes |
|---|---|---|---|
| username | string | yes | unique per `hospital`; leading/trailing whitespace trimmed; max 50 characters after trimming |
| password | string | yes | 10-20 characters, hashed with bcrypt before storage (max kept well under bcrypt's 72-byte input limit, which would otherwise silently truncate longer passwords) |
| hospital | string | yes | hospital **code** (e.g. `hospital_a`) — must match a known `hospitals.code`; trimmed and length-capped the same way as `username` |

**Response `201`**
```json
{
  "id": "5f2b1c1a-...-uuid",
  "username": "nurse_j",
  "hospital": "hospital_a",
  "created_at": "2026-09-18T12:00:00Z"
}
```
`hospital` echoes back the code as submitted, trimmed the same way it was validated (see the field table above) — the scope doesn't specify an output shape for this endpoint, so nothing beyond the normalized input (plus the generated `id`/`created_at`) is added. No display-name lookup, no extra fields.

**Errors**
| status | code | when |
|---|---|---|
| 400 | VALIDATION_ERROR | missing/malformed field |
| 409 | USERNAME_TAKEN | username already exists for that hospital |
| 422 | UNKNOWN_HOSPITAL | hospital code not recognized |

**Required test cases (do not skip):**
- valid input creates a staff row, returns 201 with `id`/`username`/`hospital`/`created_at`.
- duplicate username **within the same hospital** → 409 USERNAME_TAKEN.
- same username in a **different** hospital → succeeds (uniqueness is per-hospital, not global — Assumption 2).
- unknown hospital code → 422 UNKNOWN_HOSPITAL.
- missing/empty `username`, `password`, or `hospital` → 400 VALIDATION_ERROR.
- password shorter than 10 or longer than 20 characters → 400 VALIDATION_ERROR.
- `username` or `hospital` longer than 50 characters → 400 VALIDATION_ERROR.
- whitespace-only `username` (e.g. `"   "`) → 400 VALIDATION_ERROR (trims to empty).
- `username`/`hospital` with leading/trailing whitespace (e.g. `"  nurse_j  "`) → succeeds, stored/returned trimmed.

---

## `POST /staff/login`

**Auth:** none. `username` and `hospital` are trimmed and length-capped the same way as `/staff/create` (see that section's field table) — an over-length value can never match a real row (bounded at write time by `/staff/create`), so it's rejected as `VALIDATION_ERROR` before any credential check runs, not folded into the constant-time `INVALID_CREDENTIALS` path below.

**Request**
```json
{
  "username": "nurse_j",
  "password": "S3cur3P@ss!",
  "hospital": "hospital_a"
}
```

**Response `200`**
```json
{
  "access_token": "eyJhbGciOi...",
  "refresh_token": "eyJhbGciOi...",
  "token_type": "Bearer",
  "expires_in": 900
}
```

**Errors**
| status | code | when |
|---|---|---|
| 400 | VALIDATION_ERROR | missing/malformed field |
| 401 | INVALID_CREDENTIALS | wrong username/password/hospital combination |

**Required test cases (do not skip):**
- correct `username`/`password`/`hospital` → 200 with `access_token`/`refresh_token`.
- correct username/password but **wrong hospital** → 401 INVALID_CREDENTIALS (looks identical to "wrong password" — never reveal which part was wrong).
- wrong password → 401 INVALID_CREDENTIALS.
- unknown username → 401 INVALID_CREDENTIALS (same generic error as wrong password — don't leak whether the username exists).
- missing/empty field → 400 VALIDATION_ERROR.

---

## `POST /staff/refresh`

**Auth:** none (refresh token carries the authority).

**Request**
```json
{ "refresh_token": "eyJhbGciOi..." }
```

**Response `200`** — same shape as `/staff/login`, tokens rotated.

**Errors**
| status | code | when |
|---|---|---|
| 401 | INVALID_REFRESH_TOKEN | expired, revoked, or unknown token |

**Required test cases (do not skip):**
- valid, unexpired refresh token → 200 with a new, rotated token pair.
- expired refresh token → 401 INVALID_REFRESH_TOKEN.
- **reuse of an already-rotated token** (replay after a prior successful refresh) → 401 INVALID_REFRESH_TOKEN.
- malformed/garbage token string → 401 INVALID_REFRESH_TOKEN.

---

## `POST /patient/search`

**Auth:** required (Bearer access token).

**Request** — all fields optional:
```json
{
  "national_id": "1234567890123",
  "passport_id": null,
  "first_name": "Somchai",
  "middle_name": null,
  "last_name": null,
  "date_of_birth": null,
  "phone_number": null,
  "email": null,
  "page": 1,
  "page_size": 20
}
```

**Response `200`**
```json
{
  "data": [
    {
      "patient_hn": "HN00123",
      "national_id": "1234567890123",
      "passport_id": null,
      "first_name_th": "สมชาย",
      "middle_name_th": null,
      "last_name_th": "ใจดี",
      "first_name_en": "Somchai",
      "middle_name_en": null,
      "last_name_en": "Jaidee",
      "date_of_birth": "1985-04-12",
      "phone_number": "0812345678",
      "email": "somchai@example.com",
      "gender": "M"
    }
  ],
  "page": 1,
  "page_size": 20,
  "total": 1
}
```

**Behavior — important beyond the schema:**
- Every optional string filter (`national_id`, `passport_id`, `first_name`, `middle_name`, `last_name`, `phone_number`, `email`) is trimmed of whitespace and treated as "not provided" if that leaves it empty — `{"national_id": ""}` behaves identically to omitting the field entirely, rather than triggering a pointless id-based sync attempt with an empty id. Any of these fields longer than 200 characters after trimming is rejected as `VALIDATION_ERROR`.
- If `national_id` or `passport_id` is given and not already present in Postgres, the search usecase calls the staff's hospital HIS adapter live, normalizes the response into the internal `Patient` shape, upserts it, and includes it in the result. This upsert is what fires the `PatientSynced` event (see project structure doc).
- Searches on name / date_of_birth / phone / email query Postgres only — the upstream Hospital A API has no capability to filter on those fields (it only supports single-ID lookup).
- **Name matching is language-agnostic.** The request takes plain `first_name`/`middle_name`/`last_name` (no `_th`/`_en` distinction — that split only exists in stored data, not in this endpoint's input contract). Each is matched against **both** the `_th` and `_en` column for that name part, OR'd together: `WHERE first_name_th ILIKE $1 OR first_name_en ILIKE $1` (same pattern for middle/last). A staff member can search a patient by either their Thai or English name without needing to know which one is on file.
- **Name filters treat `%` and `_` as literal characters, not `ILIKE` wildcards.** A search for `first_name: "100%"` matches a name containing the literal substring "100%", not "any characters followed by anything" — the value is escaped before being wrapped for the partial match, so a name that happens to contain one of these characters can still be searched for as typed.
- Results are always scoped to `staff.hospital`, enforced server-side — never trusted from client input. The hospital itself is **not** part of the response payload (the scope specifies no such field, and a staff member is only ever shown their own hospital's data, so it would be redundant); scoping is an internal query constraint, not something the client needs echoed back.
- **If `hospitals.code` exists in the DB but no adapter is registered for it in `his.Registry`** (hospital onboarded administratively before its HIS integration is coded), the live-sync step is skipped silently — the request still succeeds, returning whatever's already cached in Postgres (nothing, for a brand-new hospital). This is a distinct case from "id not found in HIS" and should be logged server-side, but must never surface as a request-level error — only the id-lookup-sync path is affected; name/DOB/phone/email search never touches the adapter at all.

**Errors**
| status | code | when |
|---|---|---|
| 400 | VALIDATION_ERROR | malformed filter (e.g. bad date format) |
| 401 | UNAUTHENTICATED | missing/invalid/expired access token |

**Required test cases (do not skip):**
- id-based search where the adapter *is* registered and the HIS has a match → upserts and returns it.
- id-based search where the adapter *is* registered and the HIS has no match → empty result, no error.
- id-based search where **no adapter is registered** for the staff's hospital → empty/cached-only result, no error, distinct log line (not the same code path as "HIS has no match").
- name/DOB/phone/email-only search → never invokes the adapter, even when one is registered.
- search results never include another hospital's patients, even when filters would otherwise match.
- a `first_name` filter matches a patient whose **Thai** name matches, even if their English name doesn't (and vice versa).
- `{"national_id": ""}` (empty string, not omitted) → treated as no filter at all, no HIS sync attempted.
- a filter field longer than 200 characters → 400 VALIDATION_ERROR.
- a name filter containing a literal `%` or `_` matches only names containing that literal character, not everything (or every single-character substitution).
- a HIS response missing a required field, or with an unrecognized `gender` value, is treated as a genuine sync failure (`SyncFailedEvent` fires, error propagates) — never an unhandled 500 from a downstream NOT NULL/CHECK constraint violation.
